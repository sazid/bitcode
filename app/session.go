package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
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
	"github.com/sazid/bitcode/internal/tools"
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

type permPromptState int

const (
	permPromptChoosing permPromptState = iota
	permPromptFeedback
)

// --- Serializable state / non-serializable runtime split ---

// SessionState holds the pure, JSON-serializable portion of the session.
type SessionState struct {
	Phase         sessionState    `json:"phase"`
	SpinnerActive bool            `json:"spinner_active"`
	SpinnerFrame  int             `json:"spinner_frame"`
	SpinnerMsg    string          `json:"spinner_msg"`
	SpinnerAnim   int             `json:"spinner_anim"`
	OutputQueue   []string        `json:"output_queue,omitempty"`
	Commands      []SlashCommand  `json:"commands"`
	Suggestions   []SlashCommand  `json:"suggestions,omitempty"`
	ShowSuggest   bool            `json:"show_suggest"`
	SuggestIdx    int             `json:"suggest_idx"`
	PermToolName  string          `json:"perm_tool_name,omitempty"`
	PermDecision  *guard.Decision `json:"perm_decision,omitempty"`
	PermPhase     permPromptState `json:"perm_phase"`
	Width         int             `json:"width"`
	Height        int             `json:"height"`
	Quitting      bool            `json:"quitting"`
	TextContent   string          `json:"text_content"`
	TaskID        string          `json:"task_id"`
	TurnCount     int             `json:"turn_count"`
}

// SessionRuntime holds channels, widgets, and handles that cannot be serialized.
type SessionRuntime struct {
	textarea       textarea.Model
	permFeedback   textinput.Model
	submitCh       chan InputResult
	permRespCh     chan guard.PermissionResult
	agentCancel    context.CancelFunc
	todoStore      tools.TodoStore
	themes         *ThemeRegistry
	flushing       bool
	ticking        bool
	agentStartedAt time.Time
}

// --- Session model ---

type sessionModel struct {
	state   SessionState
	runtime SessionRuntime
}

func newSessionModel(config *AgentConfig, themes *ThemeRegistry, commands []SlashCommand, submitCh chan InputResult) sessionModel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything... (Enter for newline, Ctrl+S to submit)"
	ta.Prompt = "\u276f "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetHeight(2)
	ta.MaxHeight = 20
	ta.SetPromptFunc(2, func(lineIdx int) string {
		if lineIdx == 0 {
			return "\u276f "
		}
		return "  "
	})
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	t := themes.Active()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(t.Dim)
	ta.FocusedStyle.Text = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(t.Secondary)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(t.Dim)
	ta.Focus()

	ti := textinput.New()
	ti.Placeholder = "Type your instructions for the agent..."
	ti.CharLimit = 500

	return sessionModel{
		state: SessionState{
			Phase:      sessionIdle,
			SpinnerMsg: spinnerMessages[rand.Intn(len(spinnerMessages))],
			Commands:   commands,
			TaskID:     GenerateTaskID(),
		},
		runtime: SessionRuntime{
			textarea:     ta,
			permFeedback: ti,
			submitCh:     submitCh,
			todoStore:    config.TodoStore,
			themes:       themes,
		},
	}
}

func (m sessionModel) Init() tea.Cmd {
	return textarea.Blink
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
		return m, nil

	case agentThinkingMsg:
		m.state.SpinnerActive = msg.active
		if msg.active {
			m.state.TurnCount++
			m.state.SpinnerFrame = 0
			m.state.SpinnerMsg = spinnerMessages[rand.Intn(len(spinnerMessages))]
			m.state.SpinnerAnim = int(randomSpinnerAnim())
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
		return m, nil

	case spinnerTickMsg:
		if !m.state.SpinnerActive {
			m.runtime.ticking = false
			return m, nil
		}
		m.state.SpinnerFrame++
		if m.state.SpinnerFrame%100 == 0 {
			m.state.SpinnerMsg = spinnerMessages[rand.Intn(len(spinnerMessages))]
			m.state.SpinnerAnim = int(randomSpinnerAnim())
		}
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
		m.state.PermPhase = permPromptChoosing
		m.runtime.textarea.Blur()
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, inputKeys.Quit): // ctrl+d
			m.state.Quitting = true
			close(m.runtime.submitCh)
			return m, tea.Quit

		case key.Matches(msg, inputKeys.Submit): // ctrl+s
			text := strings.TrimSpace(m.runtime.textarea.Value())
			if text == "" {
				return m, nil
			}
			m.runtime.textarea.Reset()
			m.runtime.textarea.SetHeight(2)
			m.state.ShowSuggest = false
			m.state.Suggestions = nil

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

		case msg.Type == tea.KeyCtrlC:
			if m.state.Phase == sessionAgentRunning && m.runtime.agentCancel != nil {
				m.runtime.agentCancel()
				return m, nil
			}
			if strings.TrimSpace(m.runtime.textarea.Value()) == "" {
				m.state.Quitting = true
				close(m.runtime.submitCh)
				return m, tea.Quit
			}
			m.runtime.textarea.Reset()
			m.runtime.textarea.SetHeight(2)
			m.state.ShowSuggest = false
			m.state.Suggestions = nil
			return m, nil

		case msg.Type == tea.KeyEscape:
			if m.state.ShowSuggest {
				m.state.ShowSuggest = false
				m.state.Suggestions = nil
				return m, nil
			}
			m.runtime.textarea.Reset()
			m.runtime.textarea.SetHeight(2)
			return m, nil
		}

		// Autocomplete key handling
		if m.state.ShowSuggest && len(m.state.Suggestions) > 0 {
			switch msg.Type {
			case tea.KeyUp:
				m.state.SuggestIdx--
				if m.state.SuggestIdx < 0 {
					m.state.SuggestIdx = len(m.state.Suggestions) - 1
				}
				return m, nil
			case tea.KeyDown:
				m.state.SuggestIdx++
				if m.state.SuggestIdx >= len(m.state.Suggestions) {
					m.state.SuggestIdx = 0
				}
				return m, nil
			case tea.KeyTab:
				selected := m.state.Suggestions[m.state.SuggestIdx]
				m.runtime.textarea.SetValue("/" + selected.Name)
				m.runtime.textarea.CursorEnd()
				m.state.ShowSuggest = false
				m.state.Suggestions = nil
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.state.Width = msg.Width
		m.state.Height = msg.Height
		m.runtime.textarea.SetWidth(msg.Width - 1)
	}

	// Pre-grow textarea before processing keystroke
	m.resizeTextarea()
	if h := m.runtime.textarea.Height(); h < 20 {
		m.runtime.textarea.SetHeight(h + 1)
	}

	var cmd tea.Cmd
	m.runtime.textarea, cmd = m.runtime.textarea.Update(msg)
	m.resizeTextarea()
	m.updateSuggestions()

	return m, cmd
}

func (m sessionModel) updatePermission(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.state.PermPhase == permPromptFeedback {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.Type {
			case tea.KeyEnter:
				if feedback := strings.TrimSpace(m.runtime.permFeedback.Value()); feedback != "" {
					m.runtime.permRespCh <- guard.PermissionResult{Feedback: feedback}
					m.runtime.permFeedback.Reset()
					m.state.Phase = sessionAgentRunning
					m.runtime.textarea.Focus()
					return m, nil
				}
				return m, nil
			case tea.KeyEsc, tea.KeyCtrlC:
				m.state.PermPhase = permPromptChoosing
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.runtime.permFeedback, cmd = m.runtime.permFeedback.Update(msg)
		return m, cmd
	}

	// Choosing state
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(keyMsg.String()) {
		case "y":
			m.runtime.permRespCh <- guard.PermissionResult{Approved: true, Cache: false}
			m.state.Phase = sessionAgentRunning
			m.runtime.textarea.Focus()
			return m, nil
		case "a":
			m.runtime.permRespCh <- guard.PermissionResult{Approved: true, Cache: true}
			m.state.Phase = sessionAgentRunning
			m.runtime.textarea.Focus()
			return m, nil
		case "n":
			m.runtime.permRespCh <- guard.PermissionResult{Approved: false}
			m.state.Phase = sessionAgentRunning
			m.runtime.textarea.Focus()
			return m, nil
		case "t":
			m.state.PermPhase = permPromptFeedback
			return m, m.runtime.permFeedback.Focus()
		}
	}
	return m, nil
}

func (m sessionModel) View() string {
	if m.state.Quitting {
		return ""
	}

	t := m.runtime.themes.Active()
	var sb strings.Builder

	// Todo status
	if m.runtime.todoStore != nil {
		if ts := RenderTodoStatus(t, m.runtime.todoStore.Get()); ts != "" {
			sb.WriteString(ts)
		}
	}

	// Spinner (when agent active) with animated message and elapsed time
	if m.state.SpinnerActive {
		bits := randomBinary(6)
		localFrame := m.state.SpinnerFrame % 100
		animatedMsg := renderAnimatedMsg(t, m.state.SpinnerMsg, spinnerAnimKind(m.state.SpinnerAnim), localFrame)
		elapsed := ""
		if !m.runtime.agentStartedAt.IsZero() {
			elapsed = t.ANSIDim() + " (" + formatDuration(time.Since(m.runtime.agentStartedAt)) + ")" + t.ANSIReset()
		}
		fmt.Fprintf(&sb, "\n  %s%s%s %s%s\n", t.ANSI(t.Primary), bits, t.ANSIReset(), animatedMsg, elapsed)
	}

	// Permission prompt (if in that state)
	if m.state.Phase == sessionPermissionPrompt {
		sb.WriteString(m.renderPermissionPrompt())
		return sb.String()
	}

	// Textarea with horizontal-line borders
	w := m.state.Width
	if w <= 0 {
		w = 80
	}
	lineStyle := lipgloss.NewStyle().Foreground(t.Dim)
	idStyle := lipgloss.NewStyle().Foreground(t.Info)

	// Top border with task ID right-aligned: ──────────── swift-falcon-a7 ──
	if m.state.TaskID != "" {
		label := m.state.TaskID
		// suffix: " label ──" = len(label) + 4 visible chars
		prefixLen := w - len(label) - 4
		if prefixLen < 4 {
			prefixLen = 4
		}
		sb.WriteString(lineStyle.Render(strings.Repeat("\u2500", prefixLen)+" ") + idStyle.Render(label) + lineStyle.Render(" \u2500\u2500"))
	} else {
		sb.WriteString(lineStyle.Render(strings.Repeat("\u2500", w)))
	}
	sb.WriteString("\n")
	sb.WriteString(lipgloss.NewStyle().PaddingLeft(1).Render(m.runtime.textarea.View()))
	sb.WriteString("\n")
	sb.WriteString(lineStyle.Render(strings.Repeat("\u2500", w)))
	sb.WriteString("\n")

	// Autocomplete suggestions
	if m.state.ShowSuggest && len(m.state.Suggestions) > 0 {
		sb.WriteString(m.renderSuggestions())
	}

	// Context-dependent hints with turn count
	hintStyle := lipgloss.NewStyle().Foreground(t.Dim)
	turnInfo := ""
	if m.state.TurnCount > 0 {
		turnInfo = fmt.Sprintf(" \u00b7 turn %d", m.state.TurnCount)
	}
	if m.state.Phase == sessionAgentRunning {
		sb.WriteString(hintStyle.Render(fmt.Sprintf("  ctrl+s send message \u00b7 ctrl+c interrupt \u00b7 ctrl+d exit%s", turnInfo)))
	} else {
		sb.WriteString(hintStyle.Render(fmt.Sprintf("  ctrl+s submit \u00b7 esc clear \u00b7 ctrl+d exit%s", turnInfo)))
	}

	return sb.String()
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
	fmt.Fprintf(&sb, "\n%s\u26a0 Guard: %s%s\n", t.ANSI(t.Warning), reason, t.ANSIReset())
	fmt.Fprintf(&sb, "  Tool: %s\n", m.state.PermToolName)
	if command != "" {
		fmt.Fprintf(&sb, "  %s$ %s%s\n", t.ANSIDim(), command, t.ANSIReset())
	}

	if m.state.PermPhase == permPromptFeedback {
		fmt.Fprintf(&sb, "\n  Tell the agent what to do:\n  %s\n", m.runtime.permFeedback.View())
		fmt.Fprintf(&sb, "  %sEnter to submit \u00b7 Esc to cancel%s\n", t.ANSIDim(), t.ANSIReset())
	} else {
		fmt.Fprintf(&sb, "\n  [%sy%s] Allow once  [%sa%s] Always allow  [%sn%s] Deny  [%st%s] Tell what to do\n",
			t.ANSI(t.Success), t.ANSIReset(),
			t.ANSI(t.Success), t.ANSIReset(),
			t.ANSI(t.Error), t.ANSIReset(),
			t.ANSI(t.Link), t.ANSIReset())
	}
	return sb.String()
}

func (m *sessionModel) resizeTextarea() {
	visLines := 0
	textWidth := m.runtime.textarea.Width() - 2
	if textWidth <= 0 {
		textWidth = 1
	}
	for line := range strings.SplitSeq(m.runtime.textarea.Value(), "\n") {
		if len(line) > textWidth {
			visLines += (len(line) + textWidth - 1) / textWidth
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
	m.runtime.textarea.SetHeight(visLines)
}

func (m *sessionModel) updateSuggestions() {
	val := m.runtime.textarea.Value()
	if !strings.HasPrefix(val, "/") || strings.Contains(val, "\n") || strings.Contains(val, " ") {
		m.state.ShowSuggest = false
		m.state.Suggestions = nil
		m.state.SuggestIdx = 0
		return
	}

	prefix := strings.ToLower(strings.TrimPrefix(val, "/"))

	var filtered []SlashCommand
	for _, cmd := range m.state.Commands {
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

	m.state.Suggestions = filtered
	m.state.ShowSuggest = len(filtered) > 0
	if m.state.SuggestIdx >= len(filtered) {
		m.state.SuggestIdx = 0
	}
}

func (m sessionModel) renderSuggestions() string {
	t := m.runtime.themes.Active()
	nameStyle := lipgloss.NewStyle().Foreground(t.Command)
	descStyle := lipgloss.NewStyle().Foreground(t.Dim)
	selectedStyle := lipgloss.NewStyle().Background(t.SelectedBg)
	sourceStyle := lipgloss.NewStyle().Foreground(t.Dim).Faint(true)

	maxShow := 8
	count := len(m.state.Suggestions)
	if count > maxShow {
		count = maxShow
	}

	var sb strings.Builder
	for i := 0; i < count; i++ {
		cmd := m.state.Suggestions[i]
		name := nameStyle.Render("/" + cmd.Name)
		desc := descStyle.Render(cmd.Description)

		line := fmt.Sprintf("  %s  %s", name, desc)
		if cmd.Source != "" && cmd.Source != "builtin" {
			line += " " + sourceStyle.Render("["+cmd.Source+"]")
		}

		if i == m.state.SuggestIdx {
			line = selectedStyle.Render(line)
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	if len(m.state.Suggestions) > maxShow {
		sb.WriteString(descStyle.Render(fmt.Sprintf("  ... and %d more", len(m.state.Suggestions)-maxShow)))
		sb.WriteString("\n")
	}

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
			userMsgStyle := lipgloss.NewStyle().
				Background(ut.UserMsgBg).
				Bold(true).
				Foreground(ut.Primary)
			p.Send(appendOutputMsg("\n" + userMsgStyle.Render(fmt.Sprintf(" > %s ", text))))

			if lifecycle.IsRunning() {
				p.Send(appendOutputMsg(dimStyle().Render("  (message will be delivered to the agent)")))
				lifecycle.InjectMessage(text)
			} else {
				config.TaskTitle = text
				messages = append(messages, llm.TextMessage(llm.RoleUser, text))
				lifecycle.Start(context.Background(), &messages, toolDefs)
			}

		case <-lifecycle.doneCh:
			lifecycle.running = false
		}
	}
}
