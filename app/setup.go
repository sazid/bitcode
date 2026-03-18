package main

import (
	"fmt"
	"os"
	"strings"

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
	toolManager.Register(&tools.BashTool{})
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
		Content: "The conversation is getting long. Use the Compact tool to summarize the conversation and free up context space. Include all important context in your summary so you can continue working effectively. If the current task is already complete, suggest starting a new conversation with /new instead.",
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
