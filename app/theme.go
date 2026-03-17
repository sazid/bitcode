package main

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds semantic color definitions for the TUI.
type Theme struct {
	Name string

	// Semantic colors
	Primary   lipgloss.Color // titles, user msg fg, todo, file lists
	Secondary lipgloss.Color // prompt arrow, skill accent
	Success   lipgloss.Color // success msgs, bullets, diff adds
	Error     lipgloss.Color // errors, bullets, diff removes, stderr
	Warning   lipgloss.Color // guard warnings
	Info      lipgloss.Color // descriptions, info text
	Command   lipgloss.Color // slash commands, keywords
	Dim       lipgloss.Color // hints, placeholders, borders, dim text
	Link      lipgloss.Color // permission "tell" option

	// Backgrounds
	SelectedBg lipgloss.Color // autocomplete selected item
	UserMsgBg  lipgloss.Color // user message highlight

	// Glamour
	GlamourStyle string // "dark" or "light"
}

// ANSI returns an ANSI-256 foreground escape sequence for the given color.
func (t *Theme) ANSI(c lipgloss.Color) string {
	n, err := strconv.Atoi(string(c))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\033[38;5;%dm", n)
}

// ANSIDim returns the ANSI dim/faint escape sequence.
func (t *Theme) ANSIDim() string {
	return "\033[2m"
}

// ANSIReset returns the ANSI reset escape sequence.
func (t *Theme) ANSIReset() string {
	return "\033[0m"
}

var (
	themeMu    sync.RWMutex
	active     *Theme
	themeStore = map[string]*Theme{}
)

// RegisterTheme adds a theme to the registry.
func RegisterTheme(t *Theme) {
	themeMu.Lock()
	defer themeMu.Unlock()
	themeStore[t.Name] = t
	if active == nil {
		active = t
	}
}

// ActiveTheme returns the current active theme.
func ActiveTheme() *Theme {
	themeMu.RLock()
	defer themeMu.RUnlock()
	return active
}

// SetTheme switches the active theme by name. Returns false if not found.
func SetTheme(name string) bool {
	themeMu.Lock()
	defer themeMu.Unlock()
	t, ok := themeStore[name]
	if !ok {
		return false
	}
	active = t
	return true
}

// ThemeNames returns a sorted list of registered theme names.
func ThemeNames() []string {
	themeMu.RLock()
	defer themeMu.RUnlock()
	names := make([]string, 0, len(themeStore))
	for n := range themeStore {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
