package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/sazid/bitcode/internal/tools"
)

// RenderTodoStatus renders the todo list status in a consistent style.
// Returns empty string if no todos exist.
func RenderTodoStatus(todos []tools.TodoItem) string {
	if len(todos) == 0 {
		return ""
	}

	completed := 0
	var activeContent string
	for _, t := range todos {
		if t.Status == "completed" {
			completed++
		}
		if t.Status == "in_progress" && activeContent == "" {
			activeContent = t.Content
		}
	}
	todoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Faint(true)
	countStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Faint(true)
	count := countStyle.Render(fmt.Sprintf("[%d/%d]", completed, len(todos)))
	if activeContent != "" {
		return fmt.Sprintf("  %s %s\n", count, todoStyle.Render("● "+activeContent))
	}
	return fmt.Sprintf("  %s %s\n", count, todoStyle.Render("tasks pending"))
}
