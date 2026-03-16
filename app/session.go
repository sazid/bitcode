package main

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/notify"
	"github.com/sazid/bitcode/internal/tools"
)

// --- Custom messages for agent-to-TUI communication ---

type agentThinkingMsg struct{ active bool }
type agentDoneMsg struct{}
type agentStartMsg struct{ cancel context.CancelFunc }
type spinnerTickMsg time.Time
type permRequestMsg struct {
	toolName   string
	decision   guard.Decision
	responseCh chan guard.PermissionResult
}

// --- Session states ---

type sessionState int

const (
	sessionIdle sessionState = iota
	sessionAgentRunning
	sessionPermissionPrompt
)

type permPromptState int

const (
	permPromptChoosing permPromptState = iota
	permPromptFeedback
)

// --- Session model ---

type sessionModel struct {
	textarea textarea.Model
	state    sessionState

	// Spinner
	spinnerActive bool
	spinnerFrame  int
	spinnerMsg    string

	// Autocomplete
	commands    []SlashCommand
	suggestions []SlashCommand
	showSuggest bool
	suggestIdx  int

	// Permission prompt
	permToolName   string
	permDecision   guard.Decision
	permResponseCh chan guard.PermissionResult
	permState      permPromptState
	permFeedback   textinput.Model

	// References
	submitCh    chan InputResult
	agentCancel context.CancelFunc
	todoStore   tools.TodoStore

	quitting bool
}

func newSessionModel(config *AgentConfig, commands []SlashCommand, submitCh chan InputResult) sessionModel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything... (Enter for newline, Ctrl+S to submit)"
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(2)
	ta.MaxHeight = 20
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ta.FocusedStyle.Text = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle()
	ta.Focus()

	ti := textinput.New()
	ti.Placeholder = "Type your instructions for the agent..."
	ti.CharLimit = 500

	return sessionModel{
		textarea:     ta,
		state:        sessionIdle,
		spinnerMsg:   spinnerMessages[rand.Intn(len(spinnerMessages))],
		commands:     commands,
		submitCh:     submitCh,
		todoStore:    config.TodoStore,
		permFeedback: ti,
	}
}

func (m sessionModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m sessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle permission prompt state
	if m.state == sessionPermissionPrompt {
		return m.updatePermission(msg)
	}

	switch msg := msg.(type) {
	case agentStartMsg:
		m.agentCancel = msg.cancel
		m.state = sessionAgentRunning
		return m, nil

	case agentThinkingMsg:
		m.spinnerActive = msg.active
		if msg.active {
			m.spinnerFrame = 0
			m.spinnerMsg = spinnerMessages[rand.Intn(len(spinnerMessages))]
			return m, m.tickSpinner()
		}
		return m, nil

	case agentDoneMsg:
		m.state = sessionIdle
		m.spinnerActive = false
		m.agentCancel = nil
		return m, nil

	case spinnerTickMsg:
		if !m.spinnerActive {
			return m, nil
		}
		m.spinnerFrame++
		if m.spinnerFrame%45 == 0 {
			m.spinnerMsg = spinnerMessages[rand.Intn(len(spinnerMessages))]
		}
		return m, m.tickSpinner()

	case permRequestMsg:
		m.state = sessionPermissionPrompt
		m.permToolName = msg.toolName
		m.permDecision = msg.decision
		m.permResponseCh = msg.responseCh
		m.permState = permPromptChoosing
		m.textarea.Blur()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, inputKeys.Quit): // ctrl+d
			m.quitting = true
			close(m.submitCh)
			return m, tea.Quit

		case key.Matches(msg, inputKeys.Submit): // ctrl+s
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			m.textarea.Reset()
			m.textarea.SetHeight(2)
			m.showSuggest = false
			m.suggestions = nil

			var result InputResult
			if strings.HasPrefix(text, "/") {
				result = InputResult{Command: text}
			} else {
				result = InputResult{Text: text}
			}
			select {
			case m.submitCh <- result:
			default:
			}
			return m, nil

		case msg.Type == tea.KeyCtrlC:
			if m.state == sessionAgentRunning && m.agentCancel != nil {
				m.agentCancel()
				return m, nil
			}
			if strings.TrimSpace(m.textarea.Value()) == "" {
				m.quitting = true
				close(m.submitCh)
				return m, tea.Quit
			}
			m.textarea.Reset()
			m.textarea.SetHeight(2)
			m.showSuggest = false
			m.suggestions = nil
			return m, nil

		case msg.Type == tea.KeyEscape:
			if m.showSuggest {
				m.showSuggest = false
				m.suggestions = nil
				return m, nil
			}
			m.textarea.Reset()
			m.textarea.SetHeight(2)
			return m, nil
		}

		// Autocomplete key handling
		if m.showSuggest && len(m.suggestions) > 0 {
			switch msg.Type {
			case tea.KeyUp:
				m.suggestIdx--
				if m.suggestIdx < 0 {
					m.suggestIdx = len(m.suggestions) - 1
				}
				return m, nil
			case tea.KeyDown:
				m.suggestIdx++
				if m.suggestIdx >= len(m.suggestions) {
					m.suggestIdx = 0
				}
				return m, nil
			case tea.KeyTab:
				selected := m.suggestions[m.suggestIdx]
				m.textarea.SetValue("/" + selected.Name)
				m.textarea.CursorEnd()
				m.showSuggest = false
				m.suggestions = nil
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.textarea.SetWidth(msg.Width - 6)
	}

	// Pre-grow textarea before processing keystroke
	m.resizeTextarea()
	if h := m.textarea.Height(); h < 20 {
		m.textarea.SetHeight(h + 1)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.resizeTextarea()
	m.updateSuggestions()

	return m, cmd
}

func (m sessionModel) updatePermission(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.permState == permPromptFeedback {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.Type {
			case tea.KeyEnter:
				if feedback := strings.TrimSpace(m.permFeedback.Value()); feedback != "" {
					m.permResponseCh <- guard.PermissionResult{Feedback: feedback}
					m.permFeedback.Reset()
					m.state = sessionAgentRunning
					m.textarea.Focus()
					return m, nil
				}
				return m, nil
			case tea.KeyEsc, tea.KeyCtrlC:
				m.permState = permPromptChoosing
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.permFeedback, cmd = m.permFeedback.Update(msg)
		return m, cmd
	}

	// Choosing state
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(keyMsg.String()) {
		case "y":
			m.permResponseCh <- guard.PermissionResult{Approved: true, Cache: false}
			m.state = sessionAgentRunning
			m.textarea.Focus()
			return m, nil
		case "a":
			m.permResponseCh <- guard.PermissionResult{Approved: true, Cache: true}
			m.state = sessionAgentRunning
			m.textarea.Focus()
			return m, nil
		case "n":
			m.permResponseCh <- guard.PermissionResult{Approved: false}
			m.state = sessionAgentRunning
			m.textarea.Focus()
			return m, nil
		case "t":
			m.permState = permPromptFeedback
			return m, m.permFeedback.Focus()
		}
	}
	return m, nil
}

func (m sessionModel) View() string {
	if m.quitting {
		return ""
	}

	var sb strings.Builder

	// Todo status
	if m.todoStore != nil {
		if ts := RenderTodoStatus(m.todoStore.Get()); ts != "" {
			sb.WriteString(ts)
		}
	}

	// Spinner (when agent active)
	if m.spinnerActive {
		frames := [...]string{"\u28cb", "\u2819", "\u2839", "\u2838", "\u283c", "\u2834", "\u2826", "\u2827", "\u2807", "\u280f"}
		frame := frames[m.spinnerFrame%len(frames)]
		sb.WriteString(fmt.Sprintf("\033[2m  %s %s\033[0m\n", frame, m.spinnerMsg))
	}

	// Permission prompt (if in that state)
	if m.state == sessionPermissionPrompt {
		sb.WriteString(m.renderPermissionPrompt())
		return sb.String()
	}

	// Textarea
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(0, 1)

	sb.WriteString(borderStyle.Render(m.textarea.View()))
	sb.WriteString("\n")

	// Autocomplete suggestions
	if m.showSuggest && len(m.suggestions) > 0 {
		sb.WriteString(m.renderSuggestions())
	}

	// Context-dependent hints
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	if m.state == sessionAgentRunning {
		sb.WriteString(hintStyle.Render("  ctrl+s send message \u00b7 ctrl+c interrupt \u00b7 ctrl+d exit"))
	} else {
		sb.WriteString(hintStyle.Render("  ctrl+s submit \u00b7 esc clear \u00b7 ctrl+d exit"))
	}

	return sb.String()
}

func (m sessionModel) renderPermissionPrompt() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n\033[33m\u26a0 Guard: %s\033[0m\n", m.permDecision.Reason)
	fmt.Fprintf(&sb, "  Tool: %s\n", m.permToolName)
	if m.permDecision.Command != "" {
		fmt.Fprintf(&sb, "  \033[2m$ %s\033[0m\n", m.permDecision.Command)
	}

	if m.permState == permPromptFeedback {
		fmt.Fprintf(&sb, "\n  Tell the agent what to do:\n  %s\n", m.permFeedback.View())
		fmt.Fprintf(&sb, "  \033[2mEnter to submit \u00b7 Esc to cancel\033[0m\n")
	} else {
		fmt.Fprintf(&sb, "\n  [\033[32my\033[0m] Allow once  [\033[32ma\033[0m] Always allow  [\033[31mn\033[0m] Deny  [\033[34mt\033[0m] Tell what to do\n")
	}
	return sb.String()
}

func (m *sessionModel) resizeTextarea() {
	visLines := 0
	width := m.textarea.Width()
	for line := range strings.SplitSeq(m.textarea.Value(), "\n") {
		if width > 0 && len(line) > width {
			visLines += (len(line) + width - 1) / width
		} else {
			visLines++
		}
	}
	if visLines < 2 {
		visLines = 2
	}
	if visLines > 20 {
		visLines = 20
	}
	m.textarea.SetHeight(visLines)
}

func (m *sessionModel) updateSuggestions() {
	val := m.textarea.Value()
	if !strings.HasPrefix(val, "/") || strings.Contains(val, "\n") || strings.Contains(val, " ") {
		m.showSuggest = false
		m.suggestions = nil
		m.suggestIdx = 0
		return
	}

	prefix := strings.ToLower(strings.TrimPrefix(val, "/"))

	var filtered []SlashCommand
	for _, cmd := range m.commands {
		if strings.Contains(strings.ToLower(cmd.Name), prefix) {
			filtered = append(filtered, cmd)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		iPrefix := strings.HasPrefix(strings.ToLower(filtered[i].Name), prefix)
		jPrefix := strings.HasPrefix(strings.ToLower(filtered[j].Name), prefix)
		if iPrefix != jPrefix {
			return iPrefix
		}
		return filtered[i].Name < filtered[j].Name
	})

	m.suggestions = filtered
	m.showSuggest = len(filtered) > 0
	if m.suggestIdx >= len(filtered) {
		m.suggestIdx = 0
	}
}

func (m sessionModel) renderSuggestions() string {
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	selectedStyle := lipgloss.NewStyle().Background(lipgloss.Color("237"))
	sourceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Faint(true)

	maxShow := 8
	count := len(m.suggestions)
	if count > maxShow {
		count = maxShow
	}

	var sb strings.Builder
	for i := 0; i < count; i++ {
		cmd := m.suggestions[i]
		name := nameStyle.Render("/" + cmd.Name)
		desc := descStyle.Render(cmd.Description)

		line := fmt.Sprintf("  %s  %s", name, desc)
		if cmd.Source != "" && cmd.Source != "builtin" {
			line += " " + sourceStyle.Render("["+cmd.Source+"]")
		}

		if i == m.suggestIdx {
			line = selectedStyle.Render(line)
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	if len(m.suggestions) > maxShow {
		sb.WriteString(descStyle.Render(fmt.Sprintf("  ... and %d more", len(m.suggestions)-maxShow)))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m sessionModel) tickSpinner() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// --- programWriter adapts p.Println for use as io.Writer ---

type programWriter struct{ p *tea.Program }

func (pw *programWriter) Write(b []byte) (int, error) {
	text := strings.TrimRight(string(b), "\n")
	if text != "" {
		pw.p.Println(text)
	}
	return len(b), nil
}

// --- Session callbacks for the agent loop ---

func sessionCallbacks(p *tea.Program, config *AgentConfig) AgentCallbacks {
	pw := &programWriter{p: p}
	return AgentCallbacks{
		OnContent: func(content string) {
			renderMarkdown(pw, content)
		},
		OnThinking: func(active bool) {
			p.Send(agentThinkingMsg{active: active})
		},
		OnEvent: func(e internal.Event) {
			renderEvent(pw, e)
		},
		OnError: func(err error) {
			p.Println(fmt.Sprintf("\033[31mError: %v\033[0m", err))
		},
	}
}

// --- Orchestrator goroutine ---

func runOrchestrator(p *tea.Program, config *AgentConfig, submitCh chan InputResult) {
	messages, toolDefs := newConversation(config)

	var agentRunning bool
	agentDoneCh := make(chan struct{}, 1)
	injectedMessages := make(chan string, 8)
	config.InjectedMessages = injectedMessages

	// Styles
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	userStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	skillStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

	// Wire permission handler to route through the TUI
	if config.GuardMgr != nil {
		config.GuardMgr.SetPermissionHandler(func(toolName string, decision guard.Decision) guard.PermissionResult {
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

	for {
		select {
		case result, ok := <-submitCh:
			if !ok {
				// Channel closed (ctrl+d)
				return
			}

			// Check if agent just finished (avoid stale agentRunning state)
			select {
			case <-agentDoneCh:
				agentRunning = false
			default:
			}

			// Handle slash commands
			text := ""
			if result.Command != "" {
				cmdParts := strings.SplitN(result.Command, " ", 2)
				cmdName := cmdParts[0]
				cmdArgs := ""
				if len(cmdParts) > 1 {
					cmdArgs = strings.TrimSpace(cmdParts[1])
				}

				handled := true
				switch cmdName {
				case "/exit", "/quit":
					p.Quit()
					return
				case "/new":
					if agentRunning {
						p.Println(errorStyle.Render("\n  Cannot start new conversation while agent is running. Press Ctrl+C first."))
					} else {
						config.TodoStore.Clear()
						messages, toolDefs = newConversation(config)
						p.Println(successStyle.Render("\n  \u2713 Started new conversation"))
					}
				case "/help":
					pw := &programWriter{p: p}
					printHelp(pw, config.SkillManager)
				case "/turns":
					if cmdArgs == "" {
						p.Println(dimStyle.Render(fmt.Sprintf("\n  Current max turns: %d", config.MaxTurns)))
					} else {
						var n int
						if _, err := fmt.Sscan(cmdArgs, &n); err != nil || n <= 0 {
							p.Println(errorStyle.Render(fmt.Sprintf("\n  Invalid value: %s (must be a positive integer)", cmdArgs)))
						} else {
							config.MaxTurns = n
							p.Println(successStyle.Render(fmt.Sprintf("\n  \u2713 Max turns set to %d", config.MaxTurns)))
						}
					}
				case "/reasoning":
					validEfforts := []string{"none", "low", "medium", "high", "xhigh"}
					args := strings.ToLower(cmdArgs)
					if args == "" || args == "default" || args == "clear" {
						config.Reasoning = ""
						p.Println(successStyle.Render("\n  \u2713 Reasoning effort reset to default"))
					} else {
						valid := false
						for _, e := range validEfforts {
							if args == e {
								valid = true
								break
							}
						}
						if !valid {
							p.Println(errorStyle.Render(fmt.Sprintf("\n  Invalid reasoning effort: %s", cmdArgs)))
							p.Println(dimStyle.Render("  Valid options: none, low, medium, high, xhigh, default"))
						} else {
							config.Reasoning = args
							p.Println(successStyle.Render("\n  \u2713 Reasoning effort set to " + config.Reasoning))
						}
					}
				default:
					skillName := strings.TrimPrefix(cmdName, "/")
					if skill, ok := config.SkillManager.Get(skillName); ok {
						text = skill.FormatPrompt(cmdArgs)
						p.Println(fmt.Sprintf("\n%s %s", skillStyle.Render("\u26a1"), skill.Name))
						handled = false
					} else {
						p.Println(errorStyle.Render(fmt.Sprintf("\n  Unknown command: %s", cmdName)))
						p.Println(dimStyle.Render("  Type /help for available commands"))
					}
				}
				if handled {
					continue
				}
			} else {
				text = result.Text
			}

			if text == "" {
				continue
			}

			// Show the user's message
			p.Println(fmt.Sprintf("\n%s %s", userStyle.Render(">"), text))

			if agentRunning {
				// Inject message mid-flight
				p.Println(dimStyle.Render("  (message will be delivered to the agent)"))
				select {
				case injectedMessages <- text:
				default:
				}
			} else {
				// Start agent
				config.TaskTitle = text
				messages = append(messages, llm.TextMessage(llm.RoleUser, text))
				agentRunning = true

				ctx, cancel := context.WithCancel(context.Background())
				p.Send(agentStartMsg{cancel: cancel})

				go func() {
					defer func() {
						if r := recover(); r != nil {
							p.Println(fmt.Sprintf("\033[31mAgent panic: %v\033[0m", r))
						}
						p.Send(agentDoneMsg{})
						agentDoneCh <- struct{}{}
						title := "BitCode: " + notify.Truncate(config.TaskTitle, 40)
						notify.Send(title, "Waiting for input")
					}()
					runAgentLoop(ctx, config, &messages, toolDefs, sessionCallbacks(p, config))
				}()
			}

		case <-agentDoneCh:
			agentRunning = false
		}
	}
}
