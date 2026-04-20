package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/sazid/bitcode/internal/agent"
	"github.com/sazid/bitcode/internal/config"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/reminder"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/tools"
)

// buildToolManager creates and populates the tool manager with all built-in tools.
func buildToolManager() (*tools.Manager, *tools.CompactState, tools.TodoStore, *skills.Manager) {
	toolManager := tools.NewManager()
	toolManager.Register(&tools.ReadTool{})
	toolManager.Register(&tools.WriteTool{})
	toolManager.Register(&tools.EditTool{})
	toolManager.Register(&tools.GlobTool{})
	toolManager.Register(&tools.ShellTool{})
	toolManager.Register(&tools.WebSearchTool{})
	toolManager.Register(&tools.LineCountTool{})
	toolManager.Register(&tools.FileSizeTool{})

	compactState := tools.NewCompactState()
	toolManager.Register(&tools.CompactTool{State: compactState})

	todoStore := tools.NewTodoStore()
	toolManager.Register(&tools.TodoReadTool{Store: todoStore})
	toolManager.Register(&tools.TodoWriteTool{Store: todoStore})

	skillManager := skills.DefaultManager()
	toolManager.Register(&tools.SkillTool{SkillManager: skillManager})

	return toolManager, compactState, todoStore, skillManager
}

// discoverInstructionFiles finds CLAUDE.md and AGENTS.md files in the project
// and user config directories, returning a merged list for the system prompt.
func discoverInstructionFiles() []string {
	wd, _ := os.Getwd()
	discovered := config.DiscoverInstructionFiles(wd)

	var files []string
	files = append(files, discovered.ProjectFiles...)
	for _, f := range discovered.UserFiles {
		files = append(files, "~/"+f)
	}
	return files
}

// buildReminderManager creates the reminder manager with built-in and plugin reminders.
func buildReminderManager(skillMgr skills.SkillProvider, instructionFiles []string) *reminder.Manager {
	mgr := reminder.NewManager()

	mgr.Register(reminder.Reminder{
		ID:       "skill-availability",
		Content:  buildSkillReminderContent(skillMgr),
		Schedule: reminder.Schedule{Kind: reminder.ScheduleOneShot},
		Source:   "builtin",
		Priority: 0,
		Active:   true,
	})

	mgr.Register(reminder.Reminder{
		ID:      "conversation-length",
		Content: "Context quality drops as the conversation gets longer. If the task still needs more work, use Compact proactively before the context window gets crowded. Preserve the key requirements, files examined, decisions made, open todos, and the next verification steps in the summary. If the task is already done, suggest starting a fresh conversation with /new instead of continuing to accumulate history.",
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

	mgr.Register(reminder.Reminder{
		ID:      "core-behavior",
		Content: "Remember the operating procedure: understand the task, explore before editing, plan when the work is non-trivial, delegate to explore or plan subagents early when the task is large, ambiguous, or cross-file, implement only what was asked, verify changes before declaring success, keep todos updated for multi-step work, and use Compact proactively when context quality starts dropping.",
		Schedule: reminder.Schedule{
			Kind:         reminder.ScheduleTurn,
			TurnInterval: 17,
		},
		Source:   "builtin",
		Priority: 1,
		Active:   true,
	})

	mgr.Register(reminder.Reminder{
		ID:      "subagent-delegation",
		Content: "If the task is still growing in scope, spans multiple files, or needs isolated research before coding, consider delegating now: use the explore subagent for read-only codebase investigation and the plan subagent for implementation design and sequencing.",
		Schedule: reminder.Schedule{
			Kind:     reminder.ScheduleCondition,
			MaxFires: 2,
			Condition: func(state *reminder.ConversationState) bool {
				return state.Turn >= 6 || len(state.RecentToolCallChains) >= 4
			},
		},
		Source:   "builtin",
		Priority: 2,
		Active:   true,
	})

	mgr.Register(reminder.Reminder{
		ID:      "verification-gate",
		Content: "If you made code or configuration changes, do not declare the task complete until you have run the best available verification step and checked the result. If verification is unavailable, state exactly what you inspected manually and what remains unverified.",
		Schedule: reminder.Schedule{
			Kind:         reminder.ScheduleTurn,
			TurnInterval: 19,
		},
		Source:   "builtin",
		Priority: 2,
		Active:   true,
	})

	mgr.Register(reminder.Reminder{
		ID:      "todo-discipline",
		Content: "For multi-step work, keep TodoWrite current: create actionable items, keep one item in_progress, complete items immediately after implementation plus verification, and add new work as soon as you discover it.",
		Schedule: reminder.Schedule{
			Kind:         reminder.ScheduleTurn,
			TurnInterval: 13,
		},
		Source:   "builtin",
		Priority: 2,
		Active:   true,
	})

	mgr.Register(reminder.Reminder{
		ID:      "doom-loop-detection",
		Content: "You appear to be repeating the same tool-call pattern without making progress. Stop, summarize what is failing, inspect the latest results carefully, and choose a different approach instead of retrying the same sequence again.",
		Schedule: reminder.Schedule{
			Kind:      reminder.ScheduleCondition,
			MaxFires:  3,
			Condition: reminder.ParseConditionString("repeated_tool_chain:Read>Read>Read|3"),
		},
		Source:   "builtin",
		Priority: 3,
		Active:   true,
	})

	if len(instructionFiles) > 0 {
		mgr.Register(reminder.Reminder{
			ID:      "instruction-files",
			Content: buildInstructionFilesReminderContent(instructionFiles),
			Schedule: reminder.Schedule{
				Kind:         reminder.ScheduleTurn,
				TurnInterval: 10,
			},
			Source:   "builtin",
			Priority: 0,
			Active:   true,
		})

		mgr.Register(reminder.Reminder{
			ID:      "update-instruction-files",
			Content: "You've done significant work in this session. Before finishing, consider whether the project's instruction files (CLAUDE.md, AGENTS.md) should be updated to reflect important changes — such as new conventions, architecture decisions, build commands, or key patterns. Only update if genuinely relevant and important; skip for minor or routine changes.",
			Schedule: reminder.Schedule{
				Kind:     reminder.ScheduleCondition,
				MaxFires: 1,
				Condition: func(state *reminder.ConversationState) bool {
					return state.Turn >= 15
				},
			},
			Source:   "builtin",
			Priority: 2,
			Active:   true,
		})
	}

	for _, r := range reminder.LoadPlugins() {
		mgr.Register(r)
	}

	return mgr
}

// buildGuardManager creates the guard manager with built-in rules, plugin rules,
// and the optional LLM guard agent.
func buildGuardManager(mainCfg llm.ProviderConfig) *guard.Manager {
	mgr := guard.NewManager()
	mgr.AddRule(&guard.DangerousCommandRule{})
	mgr.AddRule(&guard.WorkingDirRule{})
	mgr.AddRule(&guard.SensitiveFileRule{})
	for _, r := range guard.LoadPlugins() {
		mgr.AddRule(r)
	}
	mgr.AddRule(&guard.DefaultPolicyRule{})

	// Optional LLM guard agent (enabled by default unless explicitly disabled)
	//
	// Environment variables:
	//   BITCODE_GUARD         — "false" to disable the LLM guard agent
	//   BITCODE_GUARD_MODEL   — model for guard (default: same as main)
	//   BITCODE_GUARD_API_KEY — API key for guard (default: same as main)
	//   BITCODE_GUARD_BASE_URL — base URL for guard (default: same as main)
	//   BITCODE_GUARD_PROVIDER — backend for guard (default: same as main)
	//   BITCODE_GUARD_MAX_TURNS — max turns for guard agent (default: 0 = unlimited)
	if os.Getenv("BITCODE_GUARD") != "false" {
		guardCfg := llm.ProviderConfig{
			Backend: envOrDefault("BITCODE_GUARD_PROVIDER", mainCfg.Backend),
			Model:   envOrDefault("BITCODE_GUARD_MODEL", mainCfg.Model),
			BaseURL: envOrDefault("BITCODE_GUARD_BASE_URL", mainCfg.BaseURL),
			APIKey:  envOrDefault("BITCODE_GUARD_API_KEY", mainCfg.APIKey),
		}
		guardProvider, err := llm.NewProvider(guardCfg)
		if err != nil {
			guardProvider, _ = llm.NewProvider(mainCfg)
		}
		guardSkillMgr := guard.NewGuardSkillManager()

		maxTurns := 0
		if v := os.Getenv("BITCODE_GUARD_MAX_TURNS"); v != "" {
			fmt.Sscan(v, &maxTurns)
		}

		mgr.SetLLMValidator(guard.NewGuardAgent(guardProvider, guardCfg.Model, guardSkillMgr, maxTurns))
	}

	return mgr
}

// envOrDefault returns the environment variable value, or fallback if empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// buildAgentRegistry creates the agent registry with built-in and user-defined agent definitions.
func buildAgentRegistry() *agent.Registry {
	registry := agent.NewRegistry()
	for _, def := range agent.BuiltinDefinitions() {
		registry.Register(def)
	}
	for _, def := range agent.LoadDefinitions() {
		registry.Register(def)
	}
	return registry
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
func buildSkillReminderContent(sm skills.SkillProvider) string {
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
