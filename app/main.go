package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/notify"
	"github.com/sazid/bitcode/internal/tools"
	"github.com/sazid/bitcode/internal/version"
)

func main() {
	_ = godotenv.Load()

	var prompt string
	var reasoningEffort string
	var showVersion bool
	var maxTurns int
	flag.StringVar(&prompt, "p", "", "Prompt to send to LLM (omit for interactive mode)")
	flag.StringVar(&reasoningEffort, "reasoning", "", "Reasoning effort: low, medium, high (omit to let the model decide)")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.IntVar(&maxTurns, "max-turns", defaultMaxAgentTurns, "Maximum number of agent turns per conversation")
	flag.Parse()

	if showVersion {
		fmt.Fprintf(os.Stderr, "BitCode %s\n", version.String())
		os.Exit(0)
	}

	providerCfg := resolveProviderConfig()

	isLocalhost := strings.HasPrefix(providerCfg.BaseURL, "http://localhost") || strings.HasPrefix(providerCfg.BaseURL, "http://127.0.0.1")
	if providerCfg.APIKey == "" && !isLocalhost {
		fmt.Fprintln(os.Stderr, "Error: BITCODE_API_KEY not set (not required when base URL points to localhost)")
		os.Exit(1)
	}

	provider, err := llm.NewProvider(providerCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating LLM provider: %v\n", err)
		os.Exit(1)
	}

	model := providerCfg.Model
	themes := DefaultThemeRegistry()
	toolManager, compactState, todoStore, skillManager := buildToolManager()
	instructionFiles := discoverInstructionFiles()
	reminderMgr := buildReminderManager(skillManager, instructionFiles)
	guardMgr := buildGuardManager(providerCfg)

	if prompt != "" {
		guardMgr.SetPermissionHandler(guard.AutoDenyHandler())
	}

	agentConfig := &AgentConfig{
		Provider:         provider,
		Model:            model,
		Reasoning:        reasoningEffort,
		MaxTurns:         maxTurns,
		ToolManager:      toolManager,
		SkillManager:     skillManager,
		ReminderMgr:      reminderMgr,
		GuardMgr:         guardMgr,
		TodoStore:        todoStore,
		CompactState:     compactState,
		InstructionFiles: instructionFiles,
	}

	if prompt != "" {
		runSingleShot(agentConfig, themes, prompt)
	} else {
		runInteractive(agentConfig, themes)
	}
}

func newConversation(config *AgentConfig) ([]llm.Message, []llm.ToolDef) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, buildSystemPrompt(config.SkillManager, config.InstructionFiles)),
	}
	return messages, toolDefsFromManager(config.ToolManager)
}

func toolDefsFromManager(m tools.ToolRegistry) []llm.ToolDef {
	var defs []llm.ToolDef
	for _, d := range m.ToolDefinitions() {
		defs = append(defs, llm.ToolDef{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return defs
}

func singleShotCallbacks(themes *ThemeRegistry, todoStore tools.TodoStore) AgentCallbacks {
	var spin *Spinner
	return AgentCallbacks{
		OnContent: func(content string) {
			renderMarkdown(os.Stderr, themes.Active(), content)
		},
		OnThinking: func(active bool) {
			if active {
				var todos []tools.TodoItem
				if todoStore != nil {
					todos = todoStore.Get()
				}
				spin = StartSpinner(os.Stderr, themes.Active(), todos)
			} else if spin != nil {
				spin.Stop()
				spin = nil
			}
		},
		OnEvent: func(e internal.Event) {
			renderEvent(os.Stderr, themes.Active(), e)
		},
		OnError: func(err error) {
			t := themes.Active()
			fmt.Fprintf(os.Stderr, "%sError: %v%s\n", t.ANSI(t.Error), err, t.ANSIReset())
		},
	}
}

// runSingleShot runs a single prompt through the agent loop and exits.
func runSingleShot(config *AgentConfig, themes *ThemeRegistry, prompt string) {
	config.TaskTitle = prompt

	messages, toolDefs := newConversation(config)
	messages = append(messages, llm.TextMessage(llm.RoleUser, prompt))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	runAgentLoop(ctx, config, &messages, toolDefs, singleShotCallbacks(themes, config.TodoStore))

	title := "BitCode: " + notify.Truncate(config.TaskTitle, 40)
	notify.Send(title, "Finished working")
}

// runInteractive runs the interactive REPL mode with a persistent TUI.
func runInteractive(config *AgentConfig, themes *ThemeRegistry) {
	printWelcomeBanner(themes.Active(), config.Model, config.Reasoning)

	slashCommands := buildSlashCommands(config)
	submitCh := make(chan InputResult, 1)

	model := newSessionModel(config, themes, slashCommands, submitCh)
	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))

	// Set up interactive permission handler (needs the tea.Program reference)
	if gm, ok := config.GuardMgr.(*guard.Manager); ok {
		gm.SetPermissionHandler(func(toolName string, decision guard.Decision) guard.PermissionResult {
			title := "BitCode"
			if t := config.TaskTitle; t != "" {
				title = "BitCode: " + notify.Truncate(t, 40)
			}
			notify.Send(title, "Approval needed for "+toolName)

			responseCh := make(chan guard.PermissionResult, 1)
			p.Send(permRequestMsg{toolName: toolName, decision: decision, responseCh: responseCh})
			return <-responseCh
		})
	}

	go runOrchestrator(p, config, themes, submitCh)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

// resolveProviderConfig determines the LLM provider configuration from environment variables.
//
// Environment variables:
//
//	BITCODE_API_KEY    — API key for the LLM provider
//	BITCODE_MODEL      — model name (default: auto-detected from provider)
//	BITCODE_BASE_URL   — API endpoint (default: auto-detected from provider)
//	BITCODE_PROVIDER   — backend: "openai-chat", "openai-responses", "anthropic" (default: auto-detect from model)
//	BITCODE_WEBSOCKET  — "true" to use WebSocket transport for openai-responses
func resolveProviderConfig() llm.ProviderConfig {
	cfg := llm.ProviderConfig{
		Backend:      os.Getenv("BITCODE_PROVIDER"),
		APIKey:       os.Getenv("BITCODE_API_KEY"),
		BaseURL:      os.Getenv("BITCODE_BASE_URL"),
		Model:        os.Getenv("BITCODE_MODEL"),
		UseWebSocket: os.Getenv("BITCODE_WEBSOCKET") == "true",
	}

	// Apply defaults based on detected backend
	backend := cfg.Backend
	if backend == "" {
		backend = llm.DetectBackend(cfg.Model, cfg.BaseURL)
	}
	switch backend {
	case llm.BackendAnthropic:
		if cfg.Model == "" {
			cfg.Model = "claude-sonnet-4-6"
		}
	default:
		if cfg.BaseURL == "" {
			cfg.BaseURL = "https://openrouter.ai/api/v1"
		}
		if cfg.Model == "" {
			cfg.Model = "openrouter/free"
		}
	}

	return cfg
}

func buildSlashCommands(config *AgentConfig) []SlashCommand {
	commands := []SlashCommand{
		{Name: "new", Description: "Start a new conversation", Source: "builtin"},
		{Name: "reasoning", Description: "Set reasoning effort (none/low/medium/high/xhigh)", Source: "builtin"},
		{Name: "turns", Description: "Get or set max agent turns", Source: "builtin"},
		{Name: "theme", Description: "Switch theme (dark/light/mono)", Source: "builtin"},
		{Name: "help", Description: "Show available commands", Source: "builtin"},
		{Name: "exit", Description: "Exit BitCode", Source: "builtin"},
		{Name: "quit", Description: "Exit BitCode", Source: "builtin"},
	}
	for _, s := range config.SkillManager.List() {
		commands = append(commands, SlashCommand{
			Name:        s.Name,
			Description: s.Description,
			Source:      s.Source,
		})
	}
	return commands
}
