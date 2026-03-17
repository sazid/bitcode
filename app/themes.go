package main

import "github.com/charmbracelet/lipgloss"

func init() {
	RegisterTheme(darkTheme())
	RegisterTheme(lightTheme())
	RegisterTheme(monoTheme())
	// Dark is the default — ensure it's active even if registration order changes.
	SetTheme("dark")
}

func darkTheme() *Theme {
	return &Theme{
		Name:         "dark",
		Primary:      lipgloss.Color("6"),
		Secondary:    lipgloss.Color("5"),
		Success:      lipgloss.Color("2"),
		Error:        lipgloss.Color("1"),
		Warning:      lipgloss.Color("3"),
		Info:         lipgloss.Color("7"),
		Command:      lipgloss.Color("3"),
		Dim:          lipgloss.Color("240"),
		Link:         lipgloss.Color("4"),
		SelectedBg:   lipgloss.Color("237"),
		UserMsgBg:    lipgloss.Color("236"),
		GlamourStyle: "dark",
	}
}

func lightTheme() *Theme {
	return &Theme{
		Name:         "light",
		Primary:      lipgloss.Color("25"),
		Secondary:    lipgloss.Color("90"),
		Success:      lipgloss.Color("28"),
		Error:        lipgloss.Color("124"),
		Warning:      lipgloss.Color("136"),
		Info:         lipgloss.Color("238"),
		Command:      lipgloss.Color("136"),
		Dim:          lipgloss.Color("245"),
		Link:         lipgloss.Color("27"),
		SelectedBg:   lipgloss.Color("254"),
		UserMsgBg:    lipgloss.Color("255"),
		GlamourStyle: "light",
	}
}

func monoTheme() *Theme {
	return &Theme{
		Name:         "mono",
		Primary:      lipgloss.Color("250"),
		Secondary:    lipgloss.Color("250"),
		Success:      lipgloss.Color("250"),
		Error:        lipgloss.Color("250"),
		Warning:      lipgloss.Color("250"),
		Info:         lipgloss.Color("246"),
		Command:      lipgloss.Color("255"),
		Dim:          lipgloss.Color("240"),
		Link:         lipgloss.Color("250"),
		SelectedBg:   lipgloss.Color("237"),
		UserMsgBg:    lipgloss.Color("236"),
		GlamourStyle: "dark",
	}
}
