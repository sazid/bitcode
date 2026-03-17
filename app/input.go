package main

import (
	"fmt"
	"io"
	"os"
	"strings"

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
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "submit"),
	),
	Quit: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "exit"),
	),
}

// printWelcomeBanner displays startup info (printed once to scrollback).
func printWelcomeBanner(model, reasoning string) {
	t := ActiveTheme()

	infoStyle := lipgloss.NewStyle().
		Foreground(t.Info).
		PaddingLeft(2)

	labelStyle := lipgloss.NewStyle().
		Foreground(t.Dim)

	wd, _ := os.Getwd()

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, renderLogo())
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
}

// logoLines is the pre-rendered half-block logo for "BITCODE".
// Uses Unicode block elements (█ ▀ ▄) for a solid, filled appearance.
var logoLines = []string{
	"██▀▀▀█▄  ▀███▀  ▀▀███▀▀  ▄█▀▀▀▀  ▄█▀▀▀█▄  ██▀▀█▄   ██▀▀▀▀▀",
	"██▄▄▄█▀   ███     ███    ██      ██   ██  ██   ██  ██▄▄▄▄ ",
	"██   ██   ███     ███    ██      ██   ██  ██   ██  ██     ",
	"██▄▄▄█▀  ▄███▄    ███    ▀█▄▄▄▄  ▀█▄▄▄█▀  ██▄▄█▀   ██▄▄▄▄▄",
}

// renderLogo renders the block-character logo in the theme's primary color.
func renderLogo() string {
	t := ActiveTheme()
	style := lipgloss.NewStyle().Bold(true).Foreground(t.Primary)
	var sb strings.Builder
	for _, line := range logoLines {
		sb.WriteString("  ")
		sb.WriteString(style.Render(line))
		sb.WriteRune('\n')
	}
	return sb.String()
}

// printHelp displays available commands and skills.
func printHelp(w io.Writer, skillMgr *skills.Manager) {
	t := ActiveTheme()

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
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/reasoning"), descStyle.Render("Set reasoning effort (none/low/medium/high/xhigh)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/turns"), descStyle.Render("Get or set max agent turns (e.g. /turns 100)"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("/theme"), descStyle.Render("Switch theme (dark/light/mono)"))
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
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Ctrl+S"), descStyle.Render("Submit input / send message to agent"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Enter"), descStyle.Render("New line"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Escape"), descStyle.Render("Clear input"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Ctrl+C"), descStyle.Render("Interrupt agent / clear input / exit"))
	fmt.Fprintf(w, "  %s %s\n", cmdStyle.Render("Ctrl+D"), descStyle.Render("Exit"))
}
