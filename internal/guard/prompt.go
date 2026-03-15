package guard

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// TerminalPermissionHandler creates a PermissionHandler that prompts the user
// in the terminal. pauseThinking/resumeThinking control the spinner.
func TerminalPermissionHandler(pauseThinking, resumeThinking func()) PermissionHandler {
	return func(toolName string, decision Decision) PermissionResult {
		if pauseThinking != nil {
			pauseThinking()
		}
		defer func() {
			if resumeThinking != nil {
				resumeThinking()
			}
		}()

		return runPermissionPrompt(toolName, decision)
	}
}

// AutoDenyHandler returns a PermissionHandler that always denies (for non-interactive mode).
func AutoDenyHandler() PermissionHandler {
	return func(_ string, _ Decision) PermissionResult {
		return PermissionResult{Approved: false}
	}
}

// --- Bubbletea permission prompt ---

type permState int

const (
	permStateChoosing permState = iota
	permStateFeedback
)

type permChoice int

const (
	permDeny permChoice = iota
	permAllowOnce
	permAlwaysAllow
	permTellWhatToDo
)

type permModel struct {
	toolName string
	decision Decision
	choice   permChoice
	decided  bool
	state    permState
	feedback textinput.Model
}

func (m permModel) Init() tea.Cmd { return textinput.Blink }

func (m permModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.state == permStateFeedback {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.Type {
			case tea.KeyEnter:
				if strings.TrimSpace(m.feedback.Value()) != "" {
					m.choice = permTellWhatToDo
					m.decided = true
					return m, tea.Quit
				}
				return m, nil
			case tea.KeyEsc, tea.KeyCtrlC:
				m.state = permStateChoosing
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.feedback, cmd = m.feedback.Update(msg)
		return m, cmd
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(keyMsg.String()) {
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
		case "t":
			m.state = permStateFeedback
			return m, m.feedback.Focus()
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
		case permTellWhatToDo:
			return "\033[34m  Instructions sent to agent\033[0m\n"
		default:
			return "\033[31m  Denied\033[0m\n"
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\033[33m\u26a0 Guard: %s\033[0m\n", m.decision.Reason)
	fmt.Fprintf(&sb, "  Tool: %s\n", m.toolName)
	if m.decision.Command != "" {
		fmt.Fprintf(&sb, "  \033[2m$ %s\033[0m\n", m.decision.Command)
	}

	if m.state == permStateFeedback {
		fmt.Fprintf(&sb, "\n  Tell the agent what to do:\n  > %s\n", m.feedback.View())
		fmt.Fprintf(&sb, "  \033[2mEnter to submit · Esc to cancel\033[0m\n")
	} else {
		fmt.Fprintf(&sb, "\n  [\033[32my\033[0m] Allow once  [\033[32ma\033[0m] Always allow  [\033[31mn\033[0m] Deny  [\033[34mt\033[0m] Tell what to do\n")
	}
	return sb.String()
}

func runPermissionPrompt(toolName string, decision Decision) PermissionResult {
	ti := textinput.New()
	ti.Placeholder = "Type your instructions for the agent..."
	ti.CharLimit = 500

	model := permModel{
		toolName: toolName,
		decision: decision,
		feedback: ti,
	}

	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return PermissionResult{Approved: false}
	}

	m := finalModel.(permModel)
	switch m.choice {
	case permAllowOnce:
		return PermissionResult{Approved: true, Cache: false}
	case permAlwaysAllow:
		return PermissionResult{Approved: true, Cache: true}
	case permTellWhatToDo:
		return PermissionResult{Feedback: strings.TrimSpace(m.feedback.Value())}
	default:
		return PermissionResult{Approved: false}
	}
}
