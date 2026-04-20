package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
)

// --- Custom messages for agent-to-TUI communication ---

type agentThinkingMsg struct{ active bool }
type agentDoneMsg struct{}
type agentStartMsg struct{ cancel context.CancelFunc }
type spinnerTickMsg time.Time
type appendOutputMsg string
type flushOutputMsg struct{}
type permRequestMsg struct {
	toolName   string
	decision   guard.Decision
	responseCh chan guard.PermissionResult
}
type newConversationMsg struct{ taskID string }

var tuiSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// --- outputExec implements tea.ExecCommand to write text with the renderer fully stopped ---

type outputExec struct {
	text       string
	viewHeight int // how many view lines to clear before writing
	w          io.Writer
}

func (o *outputExec) Run() error {
	// Move cursor up to the top of the view area and erase it
	if o.viewHeight > 1 {
		fmt.Fprintf(o.w, "\033[%dA", o.viewHeight-1)
	}
	for i := 0; i < o.viewHeight; i++ {
		fmt.Fprint(o.w, "\033[2K") // erase entire line
		if i < o.viewHeight-1 {
			fmt.Fprint(o.w, "\n") // move to next line
		}
	}
	// Move back to top of cleared area
	if o.viewHeight > 1 {
		fmt.Fprintf(o.w, "\033[%dA", o.viewHeight-1)
	}
	fmt.Fprint(o.w, "\r")

	// Write the output text — this goes into the terminal scrollback
	fmt.Fprintln(o.w, o.text)
	return nil
}

func (o *outputExec) SetStdin(io.Reader)    {}
func (o *outputExec) SetStdout(w io.Writer) { o.w = w }
func (o *outputExec) SetStderr(w io.Writer) {
	if o.w == nil {
		o.w = w
	}
}

// --- Session states ---

type sessionState int

const (
	sessionIdle sessionState = iota
	sessionAgentRunning
	sessionPermissionPrompt
)

// --- Serializable state / non-serializable runtime split ---

// SessionState holds the pure, JSON-serializable portion of the session.
type SessionState struct {
	Phase         sessionState    `json:"phase"`
	SpinnerActive bool            `json:"spinner_active"`
	SpinnerFrame  int             `json:"spinner_frame"`
	OutputQueue   []string        `json:"output_queue,omitempty"`
	Commands      []SlashCommand  `json:"commands"`
	PermToolName  string          `json:"perm_tool_name,omitempty"`
	PermDecision  *guard.Decision `json:"perm_decision,omitempty"`
	Width         int             `json:"width"`
	Height        int             `json:"height"`
	Quitting      bool            `json:"quitting"`
	TaskID        string          `json:"task_id"`
	TurnCount     int             `json:"turn_count"`
	Status        sessionStatus   `json:"status,omitempty"`
}

// SessionRuntime holds channels, widgets, and handles that cannot be serialized.
type SessionRuntime struct {
	input          textinput.Model
	submitCh       chan InputResult
	permRespCh     chan guard.PermissionResult
	agentCancel    context.CancelFunc
	themes         *ThemeRegistry
	flushing       bool
	ticking        bool
	agentStartedAt time.Time
}

type sessionStatus struct {
	Project   string `json:"project,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

// --- Session model ---

type sessionModel struct {
	state   SessionState
	runtime SessionRuntime
}

func newSessionModel(config *AgentConfig, themes *ThemeRegistry, commands []SlashCommand, submitCh chan InputResult, status sessionStatus) sessionModel {
	input := textinput.New()
	input.Placeholder = "Ask BitCode"
	input.Prompt = "> "
	input.CharLimit = 0
	input.Focus()

	t := themes.Active()
	input.PlaceholderStyle = lipgloss.NewStyle().Foreground(t.Dim)
	input.TextStyle = lipgloss.NewStyle()
	input.PromptStyle = lipgloss.NewStyle().Foreground(t.Primary)

	return sessionModel{
		state: SessionState{
			Phase:    sessionIdle,
			Commands: commands,
			TaskID:   GenerateTaskID(),
			Status:   status,
		},
		runtime: SessionRuntime{
			input:    input,
			submitCh: submitCh,
			themes:   themes,
		},
	}
}

func (m sessionModel) Init() tea.Cmd {
	return textinput.Blink
}

// viewHeight returns how many terminal lines the current View() occupies.
func (m sessionModel) viewHeight() int {
	return strings.Count(m.View(), "\n") + 1
}

func (m sessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle permission prompt state
	if m.state.Phase == sessionPermissionPrompt {
		return m.updatePermission(msg)
	}

	switch msg := msg.(type) {
	case newConversationMsg:
		m.state.TaskID = msg.taskID
		m.state.TurnCount = 0
		return m, nil

	case agentStartMsg:
		m.runtime.agentCancel = msg.cancel
		m.state.Phase = sessionAgentRunning
		m.runtime.agentStartedAt = time.Now()
		m.runtime.input.Blur()
		return m, nil

	case agentThinkingMsg:
		m.state.SpinnerActive = msg.active
		if msg.active {
			m.state.TurnCount++
			m.state.SpinnerFrame = 0
			if !m.runtime.ticking {
				m.runtime.ticking = true
				return m, m.tickSpinner()
			}
		}
		return m, nil

	case agentDoneMsg:
		m.state.Phase = sessionIdle
		m.state.SpinnerActive = false
		m.runtime.ticking = false
		m.runtime.agentCancel = nil
		m.runtime.agentStartedAt = time.Time{}
		m.runtime.input.Focus()
		return m, nil

	case spinnerTickMsg:
		if !m.state.SpinnerActive {
			m.runtime.ticking = false
			return m, nil
		}
		m.state.SpinnerFrame++
		return m, m.tickSpinner()

	case appendOutputMsg:
		m.state.OutputQueue = append(m.state.OutputQueue, string(msg))
		if !m.runtime.flushing {
			m.runtime.flushing = true
			return m, func() tea.Msg { return flushOutputMsg{} }
		}
		return m, nil

	case flushOutputMsg:
		if len(m.state.OutputQueue) == 0 {
			m.runtime.flushing = false
			return m, nil
		}
		combined := strings.Join(m.state.OutputQueue, "\n")
		m.state.OutputQueue = nil
		vh := m.viewHeight()
		return m, tea.Exec(&outputExec{text: combined, viewHeight: vh, w: os.Stderr}, func(err error) tea.Msg {
			return flushOutputMsg{}
		})

	case permRequestMsg:
		m.state.Phase = sessionPermissionPrompt
		m.state.PermToolName = msg.toolName
		m.state.PermDecision = &msg.decision
		m.runtime.permRespCh = msg.responseCh
		m.runtime.input.Blur()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, inputKeys.Quit): // ctrl+d
			m.state.Quitting = true
			close(m.runtime.submitCh)
			return m, tea.Quit

		case msg.Type == tea.KeyCtrlC:
			if m.state.Phase == sessionAgentRunning && m.runtime.agentCancel != nil {
				m.runtime.agentCancel()
				m.runtime.input.Reset()
				return m, nil
			}
			if strings.TrimSpace(m.runtime.input.Value()) == "" {
				m.state.Quitting = true
				close(m.runtime.submitCh)
				return m, tea.Quit
			}
			m.runtime.input.Reset()
			return m, nil
		}

		if m.state.Phase == sessionAgentRunning {
			return m, nil
		}

		switch {
		case key.Matches(msg, inputKeys.Submit):
			text := strings.TrimSpace(m.runtime.input.Value())
			if text == "" {
				return m, nil
			}
			m.runtime.input.Reset()

			var result InputResult
			if strings.HasPrefix(text, "/") {
				result = InputResult{Command: text}
			} else {
				result = InputResult{Text: text}
			}
			select {
			case m.runtime.submitCh <- result:
			default:
			}
			return m, nil

		case msg.Type == tea.KeyEscape:
			m.runtime.input.Reset()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.state.Width = msg.Width
		m.state.Height = msg.Height
		inputWidth := msg.Width - 4
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.runtime.input.Width = inputWidth
	}

	var cmd tea.Cmd
	m.runtime.input, cmd = m.runtime.input.Update(msg)

	return m, cmd
}

func (m sessionModel) updatePermission(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch strings.ToLower(keyMsg.String()) {
	case "y":
		return m.finishPermissionPrompt(guard.PermissionResult{Approved: true, Cache: false})
	case "a":
		return m.finishPermissionPrompt(guard.PermissionResult{Approved: true, Cache: true})
	case "n", "esc", "ctrl+c":
		return m.finishPermissionPrompt(guard.PermissionResult{Approved: false})
	default:
		return m, nil
	}
}

func (m sessionModel) finishPermissionPrompt(result guard.PermissionResult) (tea.Model, tea.Cmd) {
	m.runtime.permRespCh <- result
	m.state.Phase = sessionAgentRunning
	m.state.PermToolName = ""
	m.state.PermDecision = nil
	m.runtime.input.Focus()
	return m, nil
}

func (m sessionModel) View() string {
	if m.state.Quitting {
		return ""
	}

	t := m.runtime.themes.Active()
	var sb strings.Builder

	if m.state.Phase == sessionPermissionPrompt {
		return m.renderPermissionPrompt()
	}

	if m.state.SpinnerActive {
		frame := tuiSpinnerFrames[m.state.SpinnerFrame%len(tuiSpinnerFrames)]
		elapsed := ""
		if !m.runtime.agentStartedAt.IsZero() {
			elapsed = fmt.Sprintf(" %s(%s)%s", t.ANSIDim(), formatDuration(time.Since(m.runtime.agentStartedAt)), t.ANSIReset())
		}
		fmt.Fprintf(&sb, "  %s%s%s %sWorking…%s%s\n",
			t.ANSIDim(), frame, t.ANSIReset(),
			t.ANSIDim(), t.ANSIReset(),
			elapsed,
		)
		fmt.Fprint(&sb, m.renderStatusLine(true))
		return sb.String()
	}

	fmt.Fprintf(&sb, "%s\n", m.runtime.input.View())
	fmt.Fprint(&sb, m.renderStatusLine(false))
	return sb.String()
}

func (m sessionModel) renderStatusLine(running bool) string {
	t := m.runtime.themes.Active()
	sep := fmt.Sprintf("%s · %s", t.ANSIDim(), t.ANSIReset())

	segments := make([]string, 0, 8)
	if project := strings.TrimSpace(m.state.Status.Project); project != "" {
		segments = append(segments, t.ANSI(t.Primary)+compactStatusLabel(project, 18)+t.ANSIReset())
	}
	if branch := strings.TrimSpace(m.state.Status.Branch); branch != "" {
		segments = append(segments, t.ANSI(t.Secondary)+compactStatusLabel(branch, 18)+t.ANSIReset())
	}
	if model := strings.TrimSpace(m.state.Status.Model); model != "" {
		segments = append(segments, t.ANSIDim()+compactStatusLabel(model, 28)+t.ANSIReset())
	}
	if reasoning := strings.TrimSpace(m.state.Status.Reasoning); reasoning != "" {
		segments = append(segments, t.ANSIDim()+"reasoning:"+compactStatusLabel(reasoning, 12)+t.ANSIReset())
	}
	if running {
		segments = append(segments, t.ANSIDim()+"Ctrl+C interrupt"+t.ANSIReset())
	} else {
		segments = append(segments,
			t.ANSIDim()+"Enter send"+t.ANSIReset(),
			t.ANSIDim()+"Esc clear"+t.ANSIReset(),
			t.ANSIDim()+"Ctrl+D exit"+t.ANSIReset(),
		)
	}

	return "  " + strings.Join(segments, sep)
}

func compactStatusLabel(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "…"
}

func (m sessionModel) renderPermissionPrompt() string {
	t := m.runtime.themes.Active()
	var sb strings.Builder
	reason := ""
	command := ""
	if m.state.PermDecision != nil {
		reason = m.state.PermDecision.Reason
		command = m.state.PermDecision.Command
	}

	title := "Permission"
	if m.state.PermToolName != "" {
		title = fmt.Sprintf("Permission %s", m.state.PermToolName)
	}
	fmt.Fprintf(&sb, "\n%s%s%s\n", t.ANSI(t.Warning), title, t.ANSIReset())
	if reason != "" {
		fmt.Fprintf(&sb, "  %s%s%s\n", t.ANSIDim(), reason, t.ANSIReset())
	}
	if command != "" {
		fmt.Fprintf(&sb, "  %s$ %s%s\n", t.ANSIDim(), command, t.ANSIReset())
	}
	fmt.Fprintf(&sb, "\n  %s[y]%s once  %s[a]%s always  %s[n]%s deny\n",
		t.ANSI(t.Success), t.ANSIReset(),
		t.ANSI(t.Success), t.ANSIReset(),
		t.ANSI(t.Error), t.ANSIReset(),
	)
	fmt.Fprintf(&sb, "  %sEsc or Ctrl+C deny%s\n", t.ANSIDim(), t.ANSIReset())
	return sb.String()
}

func (m sessionModel) tickSpinner() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg(t)
	})
}

// --- Session callbacks for the agent loop ---

func sessionCallbacks(p *tea.Program, themes *ThemeRegistry) AgentCallbacks {
	return AgentCallbacks{
		OnContent: func(content string) {
			var buf strings.Builder
			renderMarkdown(&buf, themes.Active(), content)
			text := strings.TrimRight(buf.String(), "\n")
			if text != "" {
				p.Send(appendOutputMsg(text))
			}
		},
		OnThinking: func(active bool) {
			p.Send(agentThinkingMsg{active: active})
		},
		OnEvent: func(e internal.Event) {
			var buf strings.Builder
			renderEvent(&buf, themes.Active(), e)
			text := strings.TrimRight(buf.String(), "\n")
			if text != "" {
				p.Send(appendOutputMsg(text))
			}
		},
		OnError: func(err error) {
			et := themes.Active()
			p.Send(appendOutputMsg(fmt.Sprintf("%sError: %v%s", et.ANSI(et.Error), err, et.ANSIReset())))
		},
	}
}

// --- Orchestrator goroutine ---

func runOrchestrator(p *tea.Program, config *AgentConfig, themes *ThemeRegistry, submitCh chan InputResult) {
	messages, toolDefs := newConversation(config)

	dispatcher := NewCommandDispatcher(config, themes, p)
	lifecycle := NewAgentLifecycle(config, themes, p)

	dimStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(themes.Active().Dim) }

	for {
		select {
		case result, ok := <-submitCh:
			if !ok {
				return
			}

			// Update running state
			lifecycle.IsRunning()

			text := ""
			if result.Command != "" {
				dr := dispatcher.Dispatch(result.Command, lifecycle.IsRunning(), func() ([]llm.Message, []llm.ToolDef) {
					messages, toolDefs = newConversation(config)
					lifecycle.Reset()
					return messages, toolDefs
				})
				if dr.Quit {
					return
				}
				if dr.Handled {
					// If the command returned messages (e.g., from /resume), update the orchestrator state
					if len(dr.Messages) > 0 {
						messages = dr.Messages
					}
					continue
				}
				text = dr.Text
			} else {
				text = result.Text
			}

			if text == "" {
				continue
			}

			ut := themes.Active()
			userMsgStyle := lipgloss.NewStyle().Foreground(ut.Info)
			p.Send(appendOutputMsg("\n" + userMsgStyle.Render("> "+text)))

			if lifecycle.IsRunning() {
				p.Send(appendOutputMsg(dimStyle().Render("  queued for agent")))
				lifecycle.InjectMessage(text)
			} else {
				config.TaskTitle = text
				// Create a new conversation if none is active
				if config.ConvManager != nil && config.ConvID == "" {
					if conv, err := config.ConvManager.Create(text); err == nil {
						config.ConvID = conv.ID
						p.Send(newConversationMsg{taskID: conv.ID})
					}
				}
				userMsg := llm.TextMessage(llm.RoleUser, text)
				messages = append(messages, userMsg)
				// Persist user message
				if config.ConvManager != nil && config.ConvID != "" {
					if err := config.ConvManager.AppendMessage(config.ConvID, userMsg); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to persist user message: %v\n", err)
					}
				}
				lifecycle.Start(context.Background(), &messages, toolDefs)
			}

		case <-lifecycle.doneCh:
			lifecycle.running = false
		}
	}
}
