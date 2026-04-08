package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/agent"
	"github.com/sazid/bitcode/internal/conversation"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/notify"
	"github.com/sazid/bitcode/internal/telemetry"
	"github.com/sazid/bitcode/internal/tools"
	"github.com/sazid/bitcode/internal/version"
)

func main() {
	_ = godotenv.Load()

	var prompt string
	var reasoningEffort string
	var showVersion bool
	var quiet bool
	var maxTurns int
	flag.StringVar(&prompt, "p", "", "Prompt to send to LLM (omit for interactive mode)")
	flag.StringVar(&reasoningEffort, "reasoning", "", "Reasoning effort: low, medium, high (omit to let the model decide)")
	flag.BoolVar(&showVersion, "version", false, "Show version information")
	flag.BoolVar(&quiet, "q", false, "Quiet mode: suppress tool usage and spinner output (single-shot only)")
	flag.IntVar(&maxTurns, "max-turns", defaultMaxAgentTurns, "Maximum number of agent turns per conversation")
	var continueID string
	flag.StringVar(&continueID, "c", "", "Resume a conversation by ID")
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

	// Initialize conversation manager
	var convManager *conversation.Manager
	if os.Getenv("BITCODE_CONVERSATIONS") != "false" {
		cwd, _ := os.Getwd()
		var err error
		convManager, err = conversation.NewManager(conversation.DefaultDir(), cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: conversation persistence disabled (%v)\n", err)
		}
	}

	if prompt != "" {
		guardMgr.SetPermissionHandler(guard.AutoDenyHandler())
	}

	// Set up telemetry observer
	var observer telemetry.Observer = telemetry.NoopObserver{}
	turnCounter := telemetry.NewTurnCounter()
	sessionID := randomHex(4)

	if os.Getenv("BITCODE_TELEMETRY") != "false" {
		store, err := telemetry.NewStore(telemetry.DefaultDir())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: telemetry disabled (%v)\n", err)
		} else {
			observer = telemetry.NewCollector(sessionID, store)
		}
	}

	// Wrap provider, tools, and guard with telemetry
	wrappedProvider := telemetry.WrapProvider(provider, observer, turnCounter, providerCfg.ProviderInfo())
	wrappedTools := telemetry.WrapToolRegistry(toolManager, observer, turnCounter)
	wrappedGuard := telemetry.WrapGuardEvaluator(guardMgr, observer, turnCounter)

	// Build agent registry and wire the Agent tool
	agentRegistry := buildAgentRegistry()

	agentConfig := &AgentConfig{
		Provider:         wrappedProvider,
		ProviderConfig:   providerCfg,
		Model:            model,
		Reasoning:        reasoningEffort,
		MaxTurns:         maxTurns,
		Tools:            wrappedTools,
		SkillManager:     skillManager,
		AgentRegistry:    agentRegistry,
		Reminders:        reminderMgr,
		Guard:            wrappedGuard,
		TodoStore:        todoStore,
		CompactState:     compactState,
		InstructionFiles: instructionFiles,
		Observer:         observer,
		TurnCounter:      turnCounter,
		ConvManager:      convManager,
	}

	// Register AgentTool after config is built (wrappedTools delegates to toolManager,
	// so this registration is visible through the wrapper).
	agentTool := &agent.AgentTool{
		Registry:     agentRegistry,
		ParentConfig: agentConfig,
	}
	agentConfig.AgentTool = agentTool
	toolManager.Register(agentTool)

	if continueID != "" && convManager != nil && prompt != "" {
		conv, err := convManager.Load(continueID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading conversation %s: %v\n", continueID, err)
			os.Exit(1)
		}
		agentConfig.ConvID = conv.ID

		observer.RecordSessionStart("single-shot-resume")
		messages, toolDefs := newConversation(agentConfig)
		messages = append(messages, conv.Messages...)
		messages = append(messages, llm.TextMessage(llm.RoleUser, prompt))
		if err := convManager.AppendMessage(conv.ID, llm.TextMessage(llm.RoleUser, prompt)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to persist user message: %v\n", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() { <-sigCh; cancel() }()

		agentConfig.TaskTitle = prompt
		result := runAgentLoop(ctx, agentConfig, &messages, toolDefs, singleShotCallbacks(themes, agentConfig.TodoStore, quiet))

		if result != nil && result.Output != "" {
			if quiet {
				fmt.Fprint(os.Stdout, result.Output)
			} else {
				renderMarkdown(os.Stdout, themes.Active(), result.Output)
			}
		}

		title := "BitCode: " + notify.Truncate(agentConfig.TaskTitle, 40)
		notify.Send(title, "Finished working")
		observer.Close()
	} else if prompt != "" {
		observer.RecordSessionStart("single-shot")
		runSingleShot(agentConfig, themes, prompt, quiet)
		observer.Close()
	} else {
		observer.RecordSessionStart("interactive")
		runInteractive(agentConfig, themes, providerCfg.ProviderInfo(), guardMgr)
		observer.Close()
	}
}

func newConversation(config *AgentConfig) ([]llm.Message, []llm.ToolDef) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, buildSystemPrompt(config.AgentRegistry)),
	}
	return messages, toolDefsFromManager(config.Tools)
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

func singleShotCallbacks(themes *ThemeRegistry, todoStore tools.TodoStore, quiet bool) AgentCallbacks {
	var spin *Spinner
	return AgentCallbacks{
		OnContent: func(content string) {
			if !quiet {
				renderMarkdown(os.Stderr, themes.Active(), content)
			}
		},
		OnThinking: func(active bool) {
			if quiet {
				return
			}
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
			if quiet {
				return
			}
			renderEvent(os.Stderr, themes.Active(), e)
		},
		OnError: func(err error) {
			t := themes.Active()
			fmt.Fprintf(os.Stderr, "%sError: %v%s\n", t.ANSI(t.Error), err, t.ANSIReset())
		},
	}
}

// runSingleShot runs a single prompt through the agent loop and exits.
func runSingleShot(config *AgentConfig, themes *ThemeRegistry, prompt string, quiet bool) {
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

	result := runAgentLoop(ctx, config, &messages, toolDefs, singleShotCallbacks(themes, config.TodoStore, quiet))

	// Write the final assistant output to stdout after all stderr activity is done.
	// This keeps it cleanly separated from tool events/spinners on stderr.
	if result != nil && result.Output != "" {
		if quiet {
			fmt.Fprint(os.Stdout, result.Output)
		} else {
			renderMarkdown(os.Stdout, themes.Active(), result.Output)
		}
	}

	title := "BitCode: " + notify.Truncate(config.TaskTitle, 40)
	notify.Send(title, "Finished working")
}

// runInteractive runs the interactive REPL mode with a persistent TUI.
func runInteractive(config *AgentConfig, themes *ThemeRegistry, providerInfo string, guardMgr *guard.Manager) {
	printWelcomeBanner(themes.Active(), config.Model, config.Reasoning, providerInfo)

	slashCommands := buildSlashCommands(config)
	submitCh := make(chan InputResult, 1)

	model := newSessionModel(config, themes, slashCommands, submitCh)
	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))

	// Set up interactive permission handler (needs the tea.Program reference)
	if guardMgr != nil {
		guardMgr.SetPermissionHandler(func(toolName string, decision guard.Decision) guard.PermissionResult {
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
		{Name: "history", Description: "List recent conversations", Source: "builtin"},
		{Name: "search", Description: "Search conversations (usage: /search <query>)", Source: "builtin"},
		{Name: "resume", Description: "Resume a conversation (usage: /resume <id>)", Source: "builtin"},
		{Name: "fork", Description: "Fork a conversation (usage: /fork <id> [msg-index])", Source: "builtin"},
		{Name: "rename", Description: "Rename current conversation", Source: "builtin"},
		{Name: "reasoning", Description: "Set reasoning effort (none/low/medium/high/xhigh)", Source: "builtin"},
		{Name: "turns", Description: "Get or set max agent turns", Source: "builtin"},
		{Name: "theme", Description: "Switch theme (dark/light/mono)", Source: "builtin"},
		{Name: "stats", Description: "Show session telemetry stats", Source: "builtin"},
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

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
