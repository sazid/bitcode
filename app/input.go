package main

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/sazid/bitcode/internal/skills"
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
		key.WithKeys("enter"),
		key.WithHelp("enter", "submit"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "exit"),
	),
}

// printWelcomeBanner displays the small logo icon and startup info.
func printWelcomeBanner(t *Theme, model, reasoning, providerInfo string) {
	icon := lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	name := lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	dim := lipgloss.NewStyle().Foreground(t.Dim)

	wd, _ := os.Getwd()
	if reasoning == "" {
		reasoning = "default"
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, " %s   %s %s\n",
		icon.Render("▄▀▀▄▄▀▀▄"),
		name.Render("BitCode"),
		dim.Render(version.String()))
	fmt.Fprintf(os.Stderr, " %s   %s\n",
		icon.Render("█▄▄██▄▄█"),
		dim.Render(model+" · "+reasoning))
	fmt.Fprintf(os.Stderr, "  %s    %s\n",
		icon.Render("▀▀  ▀▀"),
		dim.Render(providerInfo+" · "+wd))
	fmt.Fprintln(os.Stderr)
}

// printHelp displays available commands and skills.
func printHelp(w io.Writer, t *Theme, skillMgr skills.SkillProvider) {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Primary)

	cmdStyle := lipgloss.NewStyle().
		Foreground(t.Command).
		Width(12)

	descStyle := lipgloss.NewStyle().
		Foreground(t.Info)

	sourceStyle := lipgloss.NewStyle().
		Foreground(t.Dim)

	fmt.Fprintln(w)
	fmt.Fprintln(w, headerStyle.Render("  Commands"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/new"), descStyle.Render("Start a new conversation"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/history"), descStyle.Render("List recent conversations (--all for all dirs)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/search"), descStyle.Render("Search conversations (e.g. /search TODO --all)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/resume"), descStyle.Render("Resume a conversation (e.g. /resume abc123)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/fork"), descStyle.Render("Fork a conversation (e.g. /fork abc123 5)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/rename"), descStyle.Render("Rename current conversation"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/reasoning"), descStyle.Render("Set reasoning effort (none/low/medium/high/xhigh)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/turns"), descStyle.Render("Get or set max agent turns (e.g. /turns 100)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/theme"), descStyle.Render("Switch theme (dark/light/mono)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/stats"), descStyle.Render("Show session telemetry stats"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/help"), descStyle.Render("Show this help message"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/exit"), descStyle.Render("Exit BitCode"))

	if skillList := skillMgr.List(); len(skillList) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, headerStyle.Render("  Skills"))
		for _, s := range skillList {
			desc := s.Description
			if desc == "" {
				desc = "(no description)"
			}
			src := sourceStyle.Render(fmt.Sprintf("[%s]", s.Source))
			fmt.Fprintf(w, "  %s %s %s\n", cmdStyle.Render("/"+s.Name), descStyle.Render(desc), src)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, headerStyle.Render("  Keys"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Enter"), descStyle.Render("Submit input / send message to agent"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Escape"), descStyle.Render("Clear input"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Ctrl+C"), descStyle.Render("Interrupt agent / clear input / exit"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Ctrl+D"), descStyle.Render("Exit"))
}
