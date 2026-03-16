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
	"github.com/sazid/bitcode/internal/config"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/notify"
	"github.com/sazid/bitcode/internal/reminder"
	"github.com/sazid/bitcode/internal/skills"
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

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	baseUrl := os.Getenv("OPENROUTER_BASE_URL")
	model := os.Getenv("OPENROUTER_MODEL")
	if baseUrl == "" {
		baseUrl = "https://openrouter.ai/api/v1"
	}
	if model == "" {
		model = "openrouter/free"
	}

	isLocalhost := strings.HasPrefix(baseUrl, "http://localhost") || strings.HasPrefix(baseUrl, "http://127.0.0.1")
	if apiKey == "" && !isLocalhost {
		fmt.Fprintln(os.Stderr, "Error: Env variable OPENROUTER_API_KEY not found (not required when base URL points to localhost)")
		os.Exit(1)
	}

	toolManager := tools.NewManager()
	toolManager.Register(&tools.ReadTool{})
	toolManager.Register(&tools.WriteTool{})
	toolManager.Register(&tools.EditTool{})
	toolManager.Register(&tools.GlobTool{})
	toolManager.Register(&tools.BashTool{})
	toolManager.Register(&tools.WebSearchTool{})

	todoStore := tools.NewTodoStore()
	toolManager.Register(&tools.TodoReadTool{Store: todoStore})
	toolManager.Register(&tools.TodoWriteTool{Store: todoStore})

	skillManager := skills.DefaultManager()
	toolManager.Register(&tools.SkillTool{SkillManager: skillManager})

	// Discover CLAUDE.md and AGENTS.md instruction files
	wd, _ := os.Getwd()
	discovered := config.DiscoverInstructionFiles(wd)
	// Merge project and user files into a single list for the system prompt
	var instructionFiles []string
	instructionFiles = append(instructionFiles, discovered.ProjectFiles...)
	for _, f := range discovered.UserFiles {
		instructionFiles = append(instructionFiles, "~/"+f)
	}

	reminderMgr := reminder.NewManager()

	// Register built-in reminders
	reminderMgr.Register(reminder.Reminder{
		ID:       "skill-availability",
		Content:  buildSkillReminderContent(skillManager),
		Schedule: reminder.Schedule{Kind: reminder.ScheduleOneShot},
		Source:   "builtin",
		Priority: 0,
		Active:   true,
	})
	reminderMgr.Register(reminder.Reminder{
		ID:      "conversation-length",
		Content: "The conversation is getting long. Consider summarizing the work done so far and starting a new conversation with /new if the current task is complete.",
		Schedule: reminder.Schedule{
			Kind:     reminder.ScheduleCondition,
			MaxFires: 2,
			Condition: func(state *reminder.ConversationState) bool {
				return len(state.Messages) > 80
			},
		},
		Source:   "builtin",
		Priority: 1,
		Active:   true,
	})

	// Periodic reminder about instruction files
	if len(instructionFiles) > 0 {
		reminderMgr.Register(reminder.Reminder{
			ID:      "instruction-files",
			Content: buildInstructionFilesReminderContent(instructionFiles),
			Schedule: reminder.Schedule{
				Kind:         reminder.ScheduleTurn,
				TurnInterval: 8,
			},
			Source:   "builtin",
			Priority: 0,
			Active:   true,
		})
	}

	// End-of-work reminder to update instruction files
	if len(instructionFiles) > 0 {
		reminderMgr.Register(reminder.Reminder{
			ID:      "update-instruction-files",
			Content: "You've done significant work in this session. Before finishing, consider whether the project's instruction files (CLAUDE.md, AGENTS.md) should be updated to reflect important changes — such as new conventions, architecture decisions, build commands, or key patterns. Only update if genuinely relevant and important; skip for minor or routine changes.",
			Schedule: reminder.Schedule{
				Kind:     reminder.ScheduleCondition,
				MaxFires: 1,
				Condition: func(state *reminder.ConversationState) bool {
					return state.Turn >= 12
				},
			},
			Source:   "builtin",
			Priority: 2,
			Active:   true,
		})
	}

	// Load reminder plugins from disk
	for _, r := range reminder.LoadPlugins() {
		reminderMgr.Register(r)
	}

	// Guard system
	guardMgr := guard.NewManager()
	guardMgr.AddRule(&guard.DangerousCommandRule{})
	guardMgr.AddRule(&guard.WorkingDirRule{})
	guardMgr.AddRule(&guard.SensitiveFileRule{})
	for _, r := range guard.LoadPlugins() {
		guardMgr.AddRule(r)
	}
	guardMgr.AddRule(&guard.DefaultPolicyRule{})

	// Optional LLM guard agent (enabled by default unless explicitly disabled)
	// To disable: set BITCODE_GUARD_LLM=false
	if os.Getenv("BITCODE_GUARD_LLM") != "false" {
		guardModel := os.Getenv("BITCODE_GUARD_LLM_MODEL")
		if guardModel == "" {
			guardModel = model
		}
		guardBaseURL := os.Getenv("BITCODE_GUARD_LLM_BASE_URL")
		if guardBaseURL == "" {
			guardBaseURL = baseUrl
		}
		guardAPIKey := os.Getenv("BITCODE_GUARD_LLM_API_KEY")
		if guardAPIKey == "" {
			guardAPIKey = apiKey
		}
		guardProvider := llm.NewOpenAIProvider(guardAPIKey, guardBaseURL)
		guardSkillMgr := guard.NewGuardSkillManager()

		maxTurns := 0 // uses default (5)
		if v := os.Getenv("BITCODE_GUARD_MAX_TURNS"); v != "" {
			fmt.Sscan(v, &maxTurns)
		}

		guardMgr.SetLLMValidator(guard.NewGuardAgent(guardProvider, guardModel, guardSkillMgr, maxTurns))
	}

	isNonInteractive := prompt != ""
	if isNonInteractive {
		guardMgr.SetPermissionHandler(guard.AutoDenyHandler())
	}
	// Interactive permission handler is set in runInteractive

	config := &AgentConfig{
		Provider:         llm.NewOpenAIProvider(apiKey, baseUrl),
		Model:            model,
		Reasoning:        reasoningEffort,
		MaxTurns:         maxTurns,
		ToolManager:      toolManager,
		SkillManager:     skillManager,
		ReminderMgr:      reminderMgr,
		GuardMgr:         guardMgr,
		TodoStore:        todoStore,
		InstructionFiles: instructionFiles,
	}

	if prompt != "" {
		runSingleShot(config, prompt)
	} else {
		runInteractive(config)
	}
}

func newConversation(config *AgentConfig) ([]llm.Message, []llm.ToolDef) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, buildSystemPrompt(config.SkillManager, config.InstructionFiles)),
	}
	return messages, toolDefsFromManager(config.ToolManager)
}

func toolDefsFromManager(m *tools.Manager) []llm.ToolDef {
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

func singleShotCallbacks(todoStore tools.TodoStore) AgentCallbacks {
	var spin *Spinner
	return AgentCallbacks{
		OnContent: func(content string) {
			renderMarkdown(os.Stderr, content)
		},
		OnThinking: func(active bool) {
			if active {
				var todos []tools.TodoItem
				if todoStore != nil {
					todos = todoStore.Get()
				}
				spin = StartSpinner(os.Stderr, todos)
			} else if spin != nil {
				spin.Stop()
				spin = nil
			}
		},
		OnEvent: func(e internal.Event) {
			renderEvent(os.Stderr, e)
		},
		OnError: func(err error) {
			fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
		},
	}
}

// runSingleShot runs a single prompt through the agent loop and exits.
func runSingleShot(config *AgentConfig, prompt string) {
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

	runAgentLoop(ctx, config, &messages, toolDefs, singleShotCallbacks(config.TodoStore))

	title := "BitCode: " + notify.Truncate(config.TaskTitle, 40)
	notify.Send(title, "Finished working")
}

// runInteractive runs the interactive REPL mode with a persistent TUI.
func runInteractive(config *AgentConfig) {
	printWelcomeBanner(config.Model, config.Reasoning)

	// Build slash command list for autocomplete
	slashCommands := buildSlashCommands(config)
	submitCh := make(chan InputResult, 1)

	model := newSessionModel(config, slashCommands, submitCh)
	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))

	// Orchestrator goroutine manages agent lifecycle
	go runOrchestrator(p, config, submitCh)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

func buildSlashCommands(config *AgentConfig) []SlashCommand {
	commands := []SlashCommand{
		{Name: "new", Description: "Start a new conversation", Source: "builtin"},
		{Name: "reasoning", Description: "Set reasoning effort (none/low/medium/high/xhigh)", Source: "builtin"},
		{Name: "turns", Description: "Get or set max agent turns", Source: "builtin"},
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

// buildInstructionFilesReminderContent creates reminder content listing discovered instruction files.
func buildInstructionFilesReminderContent(files []string) string {
	var sb strings.Builder
	sb.WriteString("The following instruction files are available. Read them when working in or near their directories:\n")
	for _, f := range files {
		fmt.Fprintf(&sb, " - %s\n", f)
	}
	return sb.String()
}

// buildSkillReminderContent creates reminder content listing available skills.
func buildSkillReminderContent(sm *skills.Manager) string {
	skillList := sm.List()
	if len(skillList) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Available skills (invoke via the Skill tool):\n")
	for _, s := range skillList {
		fmt.Fprintf(&sb, "- %s", s.Name)
		if s.Description != "" {
			fmt.Fprintf(&sb, ": %s", s.Description)
		}
		if s.Trigger != "" {
			fmt.Fprintf(&sb, " (Trigger: %s)", s.Trigger)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
