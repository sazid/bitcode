package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	submitted bool
	quit      bool
	err       error
}

func newInputModel() inputModel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything... (Enter for newline, Ctrl+S to submit)"
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit
	ta.SetHeight(3)
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
			m.textarea.SetHeight(3)
			return m, nil
		case msg.Type == tea.KeyCtrlC:
			// Clear input on first Ctrl+C, quit on empty
			if strings.TrimSpace(m.textarea.Value()) == "" {
				m.quit = true
				return m, tea.Quit
			}
			m.textarea.Reset()
			m.textarea.SetHeight(3)
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.textarea.SetWidth(msg.Width - 4) // account for prompt prefix
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)

	// Auto-grow height based on content (minimum 3 lines)
	lines := strings.Count(m.textarea.Value(), "\n") + 1
	if lines < 3 {
		lines = 3
	}
	if lines > 20 {
		lines = 20
	}
	m.textarea.SetHeight(lines)

	return m, cmd
}

func (m inputModel) View() string {
	promptStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")) // cyan

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	prompt := promptStyle.Render("> ")
	hint := hintStyle.Render("  ctrl+s submit · esc clear · ctrl+d exit")

	return fmt.Sprintf("\n%s%s\n%s", prompt, m.textarea.View(), hint)
}

// readInput launches a bubbletea program to collect user input.
func readInput() InputResult {
	model := newInputModel()
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
func printWelcomeBanner(model string) {
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
	fmt.Fprintln(os.Stderr, titleStyle.Render("⚡ BitCode"))
	fmt.Fprintln(os.Stderr, subtitleStyle.Render("AI-powered coding assistant"))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, infoStyle.Render(
		labelStyle.Render("Model: ")+model,
	))
	fmt.Fprintln(os.Stderr, infoStyle.Render(
		labelStyle.Render("Cwd:   ")+wd,
	))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, subtitleStyle.Render(
		"Tips: "+
			cmdStyle.Render("/help")+" for commands, "+
			cmdStyle.Render("/new")+" for new conversation",
	))
}

// printHelp displays available commands.
func printHelp() {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	cmdStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("3")).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, headerStyle.Render("  Commands"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/new"), descStyle.Render("Start a new conversation"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/help"), descStyle.Render("Show this help message"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/exit"), descStyle.Render("Exit BitCode"))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, headerStyle.Render("  Keys"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Ctrl+S"), descStyle.Render("Submit input"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Enter"), descStyle.Render("New line"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Escape"), descStyle.Render("Clear input"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Ctrl+C"), descStyle.Render("Clear input / exit if empty"))
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("Ctrl+D"), descStyle.Render("Exit"))
}
