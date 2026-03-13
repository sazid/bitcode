package guard

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// TerminalPermissionHandler creates a PermissionHandler that prompts the user
// in the terminal. pauseThinking/resumeThinking control the spinner.
func TerminalPermissionHandler(pauseThinking, resumeThinking func()) PermissionHandler {
	return func(toolName string, decision Decision) bool {
		if pauseThinking != nil {
			pauseThinking()
		}
		defer func() {
			if resumeThinking != nil {
				resumeThinking()
			}
		}()

		result := runPermissionPrompt(toolName, decision)
		return result
	}
}

// AutoDenyHandler returns a PermissionHandler that always denies (for non-interactive mode).
func AutoDenyHandler() PermissionHandler {
	return func(_ string, _ Decision) bool {
		return false
	}
}

// --- Bubbletea permission prompt ---

type permChoice int

const (
	permDeny permChoice = iota
	permAllowOnce
	permAlwaysAllow
)

type permModel struct {
	toolName string
	decision Decision
	choice   permChoice
	decided  bool
}

func (m permModel) Init() tea.Cmd { return nil }

func (m permModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.choice = permAllowOnce
			m.decided = true
			return m, tea.Quit
		case "a":
			m.choice = permAlwaysAllow
			m.decided = true
			return m, tea.Quit
		case "n", "q", "ctrl+c", "esc":
			m.choice = permDeny
			m.decided = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m permModel) View() string {
	if m.decided {
		switch m.choice {
		case permAllowOnce:
			return "\033[32m  Allowed\033[0m\n"
		case permAlwaysAllow:
			return "\033[32m  Always allowed\033[0m\n"
		default:
			return "\033[31m  Denied\033[0m\n"
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\033[33m\u26a0 Guard: %s\033[0m\n", m.decision.Reason)
	fmt.Fprintf(&sb, "  Tool: %s\n", m.toolName)
	fmt.Fprintf(&sb, "\n  [\033[32my\033[0m] Allow once  [\033[32ma\033[0m] Always allow  [\033[31mn\033[0m] Deny\n")
	return sb.String()
}

func runPermissionPrompt(toolName string, decision Decision) bool {
	model := permModel{
		toolName: toolName,
		decision: decision,
	}

	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return false
	}

	m := finalModel.(permModel)
	return m.choice == permAllowOnce || m.choice == permAlwaysAllow
}
