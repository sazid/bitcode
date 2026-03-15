package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/tools"
)

// InputResult represents the result of reading user input.
type InputResult struct {
	Text    string
	Command string // non-empty if input is a slash command
	EOF     bool   // true if user wants to exit
}

// Key bindings for the input model.
type inputKeyMap struct {
	Submit key.Binding
	Quit   key.Binding
}

var inputKeys = inputKeyMap{
	Submit: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "submit"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "exit"),
	),
}

// inputModel is the bubbletea model for the input prompt.
type inputModel struct {
	textarea  textarea.Model
	todos     []tools.TodoItem
	submitted bool
	quit      bool
	err       error
}

func newInputModel(todos []tools.TodoItem) inputModel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything... (Enter for newline, Ctrl+S to submit)"
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit
	ta.SetHeight(2)
	ta.MaxHeight = 20
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ta.FocusedStyle.Text = lipgloss.NewStyle()
	ta.FocusedStyle.Prompt = lipgloss.NewStyle()

	ta.Focus()

	return inputModel{
		textarea: ta,
		todos:    todos,
	}
}

func (m inputModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, inputKeys.Quit):
			m.quit = true
			return m, tea.Quit
		case key.Matches(msg, inputKeys.Submit):
			text := strings.TrimSpace(m.textarea.Value())
			if text != "" {
				m.submitted = true
				return m, tea.Quit
			}
			return m, nil
		case msg.Type == tea.KeyEscape:
			// Clear current input
			m.textarea.Reset()
			m.textarea.SetHeight(2)
			return m, nil
		case msg.Type == tea.KeyCtrlC:
			// Clear input on first Ctrl+C, quit on empty
			if strings.TrimSpace(m.textarea.Value()) == "" {
				m.quit = true
				return m, tea.Quit
			}
			m.textarea.Reset()
			m.textarea.SetHeight(2)
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.textarea.SetWidth(msg.Width - 6) // account for border + padding
	}

	// Pre-grow by 1 line before textarea processes the keystroke so its
	// internal viewport has room and doesn't scroll content off the top.
	m.resizeTextarea()
	if h := m.textarea.Height(); h < 20 {
		m.textarea.SetHeight(h + 1)
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)

	// Set exact height after content has changed.
	m.resizeTextarea()

	return m, cmd
}

func (m *inputModel) resizeTextarea() {
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

func (m inputModel) View() string {
	if m.submitted || m.quit {
		return ""
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Padding(0, 1)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	hint := hintStyle.Render("  ctrl+s submit · esc clear · ctrl+d exit")

	todoStatus := RenderTodoStatus(m.todos)

	return fmt.Sprintf("\n%s%s\n%s", todoStatus, borderStyle.Render(m.textarea.View()), hint)
}

// readInput launches a bubbletea program to collect user input.
func readInput(store tools.TodoStore) InputResult {
	model := newInputModel(store.Get())
	p := tea.NewProgram(model, tea.WithOutput(os.Stderr))

	finalModel, err := p.Run()
	if err != nil {
		return InputResult{EOF: true}
	}

	m := finalModel.(inputModel)

	if m.quit {
		return InputResult{EOF: true}
	}

	if m.submitted {
		text := strings.TrimSpace(m.textarea.Value())
		if strings.HasPrefix(text, "/") {
			return InputResult{Command: text}
		}
		return InputResult{Text: text}
	}

	return InputResult{}
}

// printWelcomeBanner displays the welcome banner with project info.
func printWelcomeBanner(model, reasoning string) {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")). // cyan
		PaddingLeft(1)

	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		PaddingLeft(2)

	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		PaddingLeft(2)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	cmdStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")) // yellow

	wd, _ := os.Getwd()

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, titleStyle.Render("⚡ "+ansi.SetHyperlink("https://github.com/sazid/bitcode")+"BitCode"+ansi.ResetHyperlink()+" [https://github.com/sazid/bitcode]"))
	fmt.Fprintln(os.Stderr, subtitleStyle.Render("AI-powered coding assistant by "+ansi.SetHyperlink("https://github.com/sazid")+"@sazid"+ansi.ResetHyperlink()))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, infoStyle.Render(
		labelStyle.Render("Model:     ")+model,
	))
	if reasoning == "" {
		reasoning = "default"
	}
	fmt.Fprintln(os.Stderr, infoStyle.Render(
		labelStyle.Render("Reasoning: ")+reasoning,
	))
	fmt.Fprintln(os.Stderr, infoStyle.Render(
		labelStyle.Render("Cwd:       ")+wd,
	))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, subtitleStyle.Render(
		"Tips: "+
			cmdStyle.Render("/help")+" for commands, "+
			cmdStyle.Render("/new")+" for new conversation",
	))
}

// printHelp displays available commands and skills.
func printHelp(skillMgr *skills.Manager) {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	cmdStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))

	sourceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, headerStyle.Render("  Commands"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/new"), descStyle.Render("Start a new conversation"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/reasoning"), descStyle.Render("Set reasoning effort (none/low/medium/high/xhigh)"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/help"), descStyle.Render("Show this help message"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/exit"), descStyle.Render("Exit BitCode"))

	if skillList := skillMgr.List(); len(skillList) > 0 {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, headerStyle.Render("  Skills"))
		for _, s := range skillList {
			desc := s.Description
			if desc == "" {
				desc = "(no description)"
			}
			src := sourceStyle.Render(fmt.Sprintf("[%s]", s.Source))
			fmt.Fprintf(os.Stderr, "  %s %s %s\n", cmdStyle.Render("/"+s.Name), descStyle.Render(desc), src)
		}
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, headerStyle.Render("  Keys"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Ctrl+S"), descStyle.Render("Submit input"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Enter"), descStyle.Render("New line"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Escape"), descStyle.Render("Clear input"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Ctrl+C"), descStyle.Render("Clear input / exit if empty"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Ctrl+D"), descStyle.Render("Exit"))
}
