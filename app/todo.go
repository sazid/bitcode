package main

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/sazid/bitcode/internal/tools"
)

// RenderTodoStatus renders the todo list status in a consistent style.
// Returns empty string if no todos exist.
func RenderTodoStatus(t *Theme, todos []tools.TodoItem) string {
	if len(todos) == 0 {
		return ""
	}

	completed := 0
	var activeContent string
	for _, td := range todos {
		if td.Status == "completed" {
			completed++
		}
		if td.Status == "in_progress" && activeContent == "" {
			activeContent = td.Content
		}
	}
	todoStyle := lipgloss.NewStyle().Foreground(t.Primary).Faint(true)
	countStyle := lipgloss.NewStyle().Foreground(t.Dim).Faint(true)
	count := countStyle.Render(fmt.Sprintf("[%d/%d]", completed, len(todos)))
	if activeContent != "" {
		return fmt.Sprintf("  %s %s\n", count, todoStyle.Render("● "+activeContent))
	}
	return fmt.Sprintf("  %s %s\n", count, todoStyle.Render("tasks pending"))
}
