package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sazid/bitcode/internal/llm"
)

// CommandDispatcher handles built-in slash commands and skill routing.
type CommandDispatcher struct {
	config *AgentConfig
	themes *ThemeRegistry
	p      *tea.Program
}

// DispatchResult describes what happened after dispatching a command.
type DispatchResult struct {
	Handled bool   // true if the command was fully handled (no agent input needed)
	Text    string // non-empty text to send to the agent (for skill invocations)
	Quit    bool   // true if the user wants to exit
}

func NewCommandDispatcher(config *AgentConfig, themes *ThemeRegistry, p *tea.Program) *CommandDispatcher {
	return &CommandDispatcher{config: config, themes: themes, p: p}
}

func (d *CommandDispatcher) Dispatch(command string, agentRunning bool, resetConversation func() ([]llm.Message, []llm.ToolDef)) DispatchResult {
	cmdParts := strings.SplitN(command, " ", 2)
	cmdName := cmdParts[0]
	cmdArgs := ""
	if len(cmdParts) > 1 {
		cmdArgs = strings.TrimSpace(cmdParts[1])
	}

	successStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Success) }
	dimStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Dim) }
	errorStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Error) }
	skillStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Secondary) }

	switch cmdName {
	case "/exit", "/quit":
		d.p.Quit()
		return DispatchResult{Handled: true, Quit: true}

	case "/new":
		if agentRunning {
			d.p.Send(appendOutputMsg(errorStyle().Render("\n  Cannot start new conversation while agent is running. Press Ctrl+C first.")))
		} else {
			d.config.TodoStore.Clear()
			resetConversation()
			d.p.Send(appendOutputMsg(successStyle().Render("\n  \u2713 Started new conversation")))
		}
		return DispatchResult{Handled: true}

	case "/help":
		var buf strings.Builder
		printHelp(&buf, d.themes.Active(), d.config.SkillManager)
		text := strings.TrimRight(buf.String(), "\n")
		if text != "" {
			d.p.Send(appendOutputMsg(text))
		}
		return DispatchResult{Handled: true}

	case "/turns":
		d.handleTurns(cmdArgs, dimStyle, errorStyle, successStyle)
		return DispatchResult{Handled: true}

	case "/reasoning":
		d.handleReasoning(cmdArgs, errorStyle, successStyle, dimStyle)
		return DispatchResult{Handled: true}

	case "/theme":
		d.handleTheme(cmdArgs, dimStyle, errorStyle, successStyle)
		return DispatchResult{Handled: true}

	default:
		return d.handleSkillOrUnknown(cmdName, cmdArgs, errorStyle, dimStyle, skillStyle)
	}
}

func (d *CommandDispatcher) handleTurns(args string, dimStyle, errorStyle, successStyle func() lipgloss.Style) {
	if args == "" {
		d.p.Send(appendOutputMsg(dimStyle().Render(fmt.Sprintf("\n  Current max turns: %d", d.config.MaxTurns))))
	} else {
		var n int
		if _, err := fmt.Sscan(args, &n); err != nil || n <= 0 {
			d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Invalid value: %s (must be a positive integer)", args))))
		} else {
			d.config.MaxTurns = n
			d.p.Send(appendOutputMsg(successStyle().Render(fmt.Sprintf("\n  \u2713 Max turns set to %d", d.config.MaxTurns))))
		}
	}
}

func (d *CommandDispatcher) handleReasoning(args string, errorStyle, successStyle, dimStyle func() lipgloss.Style) {
	validEfforts := []string{"none", "low", "medium", "high", "xhigh"}
	a := strings.ToLower(args)
	if a == "" || a == "default" || a == "clear" {
		d.config.Reasoning = ""
		d.p.Send(appendOutputMsg(successStyle().Render("\n  \u2713 Reasoning effort reset to default")))
	} else {
		valid := false
		for _, e := range validEfforts {
			if a == e {
				valid = true
				break
			}
		}
		if !valid {
			d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Invalid reasoning effort: %s", args))))
			d.p.Send(appendOutputMsg(dimStyle().Render("  Valid options: none, low, medium, high, xhigh, default")))
		} else {
			d.config.Reasoning = a
			d.p.Send(appendOutputMsg(successStyle().Render("\n  \u2713 Reasoning effort set to " + d.config.Reasoning)))
		}
	}
}

func (d *CommandDispatcher) handleTheme(args string, dimStyle, errorStyle, successStyle func() lipgloss.Style) {
	if args == "" {
		activeName := d.themes.Active().Name
		var listing strings.Builder
		listing.WriteString("\n  Themes:\n")
		for _, name := range d.themes.Names() {
			marker := "  "
			if name == activeName {
				marker = "* "
			}
			listing.WriteString(fmt.Sprintf("    %s%s\n", marker, name))
		}
		d.p.Send(appendOutputMsg(dimStyle().Render(listing.String())))
	} else {
		name := strings.ToLower(args)
		if d.themes.Set(name) {
			d.p.Send(appendOutputMsg(successStyle().Render(fmt.Sprintf("\n  \u2713 Theme set to %s", name))))
		} else {
			d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Unknown theme: %s", args))))
			d.p.Send(appendOutputMsg(dimStyle().Render("  Available: " + strings.Join(d.themes.Names(), ", "))))
		}
	}
}

func (d *CommandDispatcher) handleSkillOrUnknown(cmdName, cmdArgs string, errorStyle, dimStyle, skillStyle func() lipgloss.Style) DispatchResult {
	skillName := strings.TrimPrefix(cmdName, "/")
	if skill, ok := d.config.SkillManager.Get(skillName); ok {
		d.p.Send(appendOutputMsg(fmt.Sprintf("\n%s %s", skillStyle().Render("\u26a1"), skill.Name)))
		return DispatchResult{Handled: false, Text: skill.FormatPrompt(cmdArgs)}
	}

	d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Unknown command: %s", cmdName))))
	d.p.Send(appendOutputMsg(dimStyle().Render("  Type /help for available commands")))
	return DispatchResult{Handled: true}
}
