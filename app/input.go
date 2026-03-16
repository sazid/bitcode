package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/tools"
	"github.com/sazid/bitcode/internal/version"
)

// SlashCommand represents a completable slash command or skill.
type SlashCommand struct {
	Name        string
	Description string
	Source      string // "builtin", "project", "user"
}

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

	// Autocomplete state
	commands    []SlashCommand
	suggestions []SlashCommand
	showSuggest bool
	suggestIdx  int
}

func newInputModel(todos []tools.TodoItem, commands []SlashCommand) inputModel {
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
		commands: commands,
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
			if m.showSuggest {
				m.showSuggest = false
				m.suggestions = nil
				return m, nil
			}
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
			m.showSuggest = false
			m.suggestions = nil
			return m, nil
		}

		// Autocomplete key handling when suggestions are visible
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

	// Update autocomplete suggestions after textarea processes the keystroke
	m.updateSuggestions()

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

func (m *inputModel) updateSuggestions() {
	val := m.textarea.Value()

	// Only show suggestions when input starts with "/", is single-line, and has no spaces
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

	// Sort: prefix matches first, then alphabetical
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

func (m inputModel) renderSuggestions() string {
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

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(todoStatus)
	sb.WriteString(borderStyle.Render(m.textarea.View()))
	sb.WriteString("\n")

	if m.showSuggest && len(m.suggestions) > 0 {
		sb.WriteString(m.renderSuggestions())
	}

	sb.WriteString(hint)
	return sb.String()
}

// readInput launches a bubbletea program to collect user input.
func readInput(store tools.TodoStore, commands []SlashCommand) InputResult {
	model := newInputModel(store.Get(), commands)
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
		labelStyle.Render("Version:   ")+version.String(),
	))
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
	fmt.Fprintf(os.Stderr, "  %s %s\n", cmdStyle.Render("/turns"), descStyle.Render("Get or set max agent turns (e.g. /turns 100)"))
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
