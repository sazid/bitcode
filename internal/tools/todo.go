package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/sazid/bitcode/internal"
)

// TodoItem represents a single task in the todo list.
type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`   // "pending", "in_progress", "completed"
	Priority string `json:"priority"` // "high", "medium", "low"
}

// TodoStore is the interface for todo list persistence.
type TodoStore interface {
	Get() []TodoItem
	Set(todos []TodoItem)
	HasIncomplete() bool
	Clear()
}

// MemoryTodoStore is the default in-memory implementation of TodoStore.
type MemoryTodoStore struct {
	mu    sync.RWMutex
	todos []TodoItem
}

// NewTodoStore creates a new in-memory TodoStore.
func NewTodoStore() *MemoryTodoStore {
	return &MemoryTodoStore{}
}

func (s *MemoryTodoStore) Get() []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]TodoItem, len(s.todos))
	copy(result, s.todos)
	return result
}

func (s *MemoryTodoStore) Set(todos []TodoItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = make([]TodoItem, len(todos))
	copy(s.todos, todos)
}

func (s *MemoryTodoStore) HasIncomplete() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.todos {
		if t.Status == "pending" || t.Status == "in_progress" {
			return true
		}
	}
	return false
}

func (s *MemoryTodoStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = nil
}

// formatTodos returns a human-readable representation of the todo list.
func formatTodos(todos []TodoItem) []string {
	lines := make([]string, 0, len(todos))
	for _, t := range todos {
		var icon string
		switch t.Status {
		case "completed":
			icon = "[✓]"
		case "in_progress":
			icon = "[~]"
		default:
			icon = "[ ]"
		}
		lines = append(lines, fmt.Sprintf("%s %s (%s)", icon, t.Content, t.Priority))
	}
	return lines
}

// --- TodoWrite ---

type todoWriteInput struct {
	Todos []TodoItem `json:"todos"`
}

// TodoWriteTool replaces the entire todo list.
type TodoWriteTool struct {
	Store TodoStore
}

var _ Tool = (*TodoWriteTool)(nil)

func (t *TodoWriteTool) Name() string { return "TodoWrite" }

func (t *TodoWriteTool) Description() string {
	return `Create and manage a structured task list for the current session.

Each call REPLACES the entire todo list. Use this tool to:
- Plan a multi-step task by writing all todos upfront
- Mark a todo as in_progress before starting it (only one at a time)
- Mark a todo as completed immediately after finishing it
- Add, remove, or reprioritize todos as you learn more mid-task

For any non-trivial task, the FIRST todo should be:
  "Write implementation plan to .bitcode/PLAN.md"
This allows resuming work across sessions.

You CANNOT stop working until all todos are completed.

Parameters:
- todos (required): Full replacement list of todo items
  Each item: { id, content, status, priority }
  status: "pending" | "in_progress" | "completed"
  priority: "high" | "medium" | "low"`
}

func (t *TodoWriteTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "The full replacement todo list",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Unique identifier for the todo item",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "Description of the task",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed"},
							"description": "Current status of the task",
						},
						"priority": map[string]any{
							"type":        "string",
							"enum":        []string{"high", "medium", "low"},
							"description": "Priority level of the task",
						},
					},
					"required": []string{"id", "content", "status", "priority"},
				},
			},
		},
		"required": []string{"todos"},
	}
}

func (t *TodoWriteTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params todoWriteInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	validStatuses := map[string]bool{"pending": true, "in_progress": true, "completed": true}
	validPriorities := map[string]bool{"high": true, "medium": true, "low": true}

	for i, item := range params.Todos {
		if item.ID == "" {
			return ToolResult{}, fmt.Errorf("todo item %d missing id", i)
		}
		if item.Content == "" {
			return ToolResult{}, fmt.Errorf("todo item %d missing content", i)
		}
		if !validStatuses[item.Status] {
			return ToolResult{}, fmt.Errorf("todo item %q has invalid status %q (must be pending, in_progress, or completed)", item.ID, item.Status)
		}
		if !validPriorities[item.Priority] {
			return ToolResult{}, fmt.Errorf("todo item %q has invalid priority %q (must be high, medium, or low)", item.ID, item.Priority)
		}
	}

	t.Store.Set(params.Todos)

	lines := formatTodos(params.Todos)
	preview := lines
	if len(preview) > 6 {
		preview = append(lines[:6], fmt.Sprintf("... and %d more", len(lines)-6))
	}

	completed := 0
	for _, item := range params.Todos {
		if item.Status == "completed" {
			completed++
		}
	}

	eventsCh <- internal.Event{
		Name:        t.Name(),
		Message:     fmt.Sprintf("%d/%d completed", completed, len(params.Todos)),
		Preview:     preview,
		PreviewType: internal.PreviewPlain,
	}

	return ToolResult{
		Content: fmt.Sprintf("Todo list updated (%d items, %d completed)", len(params.Todos), completed),
	}, nil
}

// --- TodoRead ---

// TodoReadTool reads the current todo list.
type TodoReadTool struct {
	Store TodoStore
}

var _ Tool = (*TodoReadTool)(nil)

func (t *TodoReadTool) Name() string { return "TodoRead" }

func (t *TodoReadTool) Description() string {
	return `Read the current todo list for this session.

Returns the full list of todos as JSON, or "No todos" if none exist.
Use this to check current progress before resuming work.`
}

func (t *TodoReadTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *TodoReadTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	todos := t.Store.Get()

	if len(todos) == 0 {
		eventsCh <- internal.Event{
			Name:        t.Name(),
			Message:     "No todos",
			PreviewType: internal.PreviewPlain,
		}
		return ToolResult{Content: "No todos"}, nil
	}

	data, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to marshal todos: %w", err)
	}

	lines := formatTodos(todos)
	preview := lines
	if len(preview) > 6 {
		preview = lines[:6]
	}

	completed := 0
	for _, item := range todos {
		if item.Status == "completed" {
			completed++
		}
	}

	eventsCh <- internal.Event{
		Name:        t.Name(),
		Message:     fmt.Sprintf("%d/%d completed", completed, len(todos)),
		Preview:     preview,
		PreviewType: internal.PreviewPlain,
	}

	return ToolResult{Content: strings.TrimSpace(string(data))}, nil
}
