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

// ThemeRegistry manages registered themes and tracks the active theme.
type ThemeRegistry struct {
	mu     sync.RWMutex
	active *Theme
	store  map[string]*Theme
}

// NewThemeRegistry creates an empty ThemeRegistry.
func NewThemeRegistry() *ThemeRegistry {
	return &ThemeRegistry{
		store: make(map[string]*Theme),
	}
}

// Register adds a theme to the registry. The first registered theme becomes active.
func (r *ThemeRegistry) Register(t *Theme) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[t.Name] = t
	if r.active == nil {
		r.active = t
	}
}

// Active returns the current active theme.
func (r *ThemeRegistry) Active() *Theme {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// Set switches the active theme by name. Returns false if not found.
func (r *ThemeRegistry) Set(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.store[name]
	if !ok {
		return false
	}
	r.active = t
	return true
}

// Names returns a sorted list of registered theme names.
func (r *ThemeRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.store))
	for n := range r.store {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
