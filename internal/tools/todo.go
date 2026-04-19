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
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "in_progress", "completed", "cancelled" (write-only for updates)
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
		lines = append(lines, fmt.Sprintf("%s %s", icon, t.Content))
	}
	return lines
}

// --- TodoWrite ---

type todoWriteInput struct {
	Todos []TodoItem `json:"todos"`
}

// TodoWriteTool incrementally updates the todo list.
type TodoWriteTool struct {
	Store TodoStore
}

var _ Tool = (*TodoWriteTool)(nil)

func (t *TodoWriteTool) Name() string { return "TodoWrite" }

func (t *TodoWriteTool) Description() string {
	return `Create and manage a structured task list for the current session.

Each call PATCHES the existing todo list instead of replacing it.
Use this tool to:
- Add new tasks by sending new { content, status } items
- Update an existing task by sending the same content with a new status
- Remove a task by sending status: "cancelled"
- Keep exactly one task in_progress at a time
- Mark tasks completed immediately after implementation and verification

Matching is done by content. Unmentioned todos are preserved automatically.
Statuses: "pending" | "in_progress" | "completed" | "cancelled"`
}

func (t *TodoWriteTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "Todo items to add, update, or cancel. Unmentioned existing todos remain unchanged.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type":        "string",
							"description": "Task description. Used as the unique key to match an existing todo.",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
							"description": "Current task status. Use cancelled to remove the item from the list.",
						},
					},
					"required": []string{"content", "status"},
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

	if len(params.Todos) == 0 {
		eventsCh <- internal.Event{
			Name:        t.Name(),
			Message:     "No changes",
			PreviewType: internal.PreviewPlain,
		}
		return ToolResult{Content: "No todo changes provided. Existing todos were left unchanged."}, nil
	}

	validStatuses := map[string]bool{"pending": true, "in_progress": true, "completed": true, "cancelled": true}
	updates := make(map[string]TodoItem, len(params.Todos))
	orderedUpdates := make([]TodoItem, 0, len(params.Todos))

	for i, item := range params.Todos {
		if strings.TrimSpace(item.Content) == "" {
			return ToolResult{}, fmt.Errorf("todo item %d missing content", i)
		}
		if !validStatuses[item.Status] {
			return ToolResult{}, fmt.Errorf("todo item %q has invalid status %q (must be pending, in_progress, completed, or cancelled)", item.Content, item.Status)
		}
		if _, exists := updates[item.Content]; exists {
			return ToolResult{}, fmt.Errorf("duplicate todo item content %q in a single update", item.Content)
		}
		updates[item.Content] = TodoItem{Content: item.Content, Status: item.Status}
		orderedUpdates = append(orderedUpdates, TodoItem{Content: item.Content, Status: item.Status})
	}

	existing := t.Store.Get()
	merged := make([]TodoItem, 0, len(existing)+len(orderedUpdates))
	seenExisting := make(map[string]bool, len(existing))

	for _, item := range existing {
		update, ok := updates[item.Content]
		if !ok {
			merged = append(merged, item)
			continue
		}
		seenExisting[item.Content] = true
		if update.Status == "cancelled" {
			continue
		}
		merged = append(merged, TodoItem{Content: item.Content, Status: update.Status})
	}

	for _, item := range orderedUpdates {
		if seenExisting[item.Content] || item.Status == "cancelled" {
			continue
		}
		merged = append(merged, TodoItem{Content: item.Content, Status: item.Status})
	}

	inProgress := 0
	completed := 0
	for _, item := range merged {
		if item.Status == "in_progress" {
			inProgress++
		}
		if item.Status == "completed" {
			completed++
		}
	}
	if inProgress > 1 {
		return ToolResult{}, fmt.Errorf("todo list has %d items in_progress; keep exactly one item in_progress at a time", inProgress)
	}

	if len(merged) == 0 {
		t.Store.Clear()
		eventsCh <- internal.Event{
			Name:        t.Name(),
			Message:     "Todo list cleared",
			Preview:     []string{"No todos remaining."},
			PreviewType: internal.PreviewPlain,
		}
		return ToolResult{Content: "Todo list updated. No todos remaining."}, nil
	}

	if completed == len(merged) {
		t.Store.Clear()
		eventsCh <- internal.Event{
			Name:        t.Name(),
			Message:     "All todos completed - cleared",
			Preview:     []string{"All tasks completed!"},
			PreviewType: internal.PreviewPlain,
		}
		return ToolResult{Content: "All todos completed. List cleared."}, nil
	}

	t.Store.Set(merged)

	lines := formatTodos(merged)
	preview := lines
	if len(preview) > 6 {
		preview = append(lines[:6], fmt.Sprintf("... and %d more", len(lines)-6))
	}

	eventsCh <- internal.Event{
		Name:        t.Name(),
		Message:     fmt.Sprintf("%d/%d completed", completed, len(merged)),
		Preview:     preview,
		PreviewType: internal.PreviewPlain,
	}

	return ToolResult{
		Content: fmt.Sprintf("Todo list updated (%d total, %d completed, %d changes applied)", len(merged), completed, len(params.Todos)),
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

Returns the full list of active and completed todos as JSON, or "No todos" if none exist.
Use this to review current progress before resuming work or before finishing.`
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
