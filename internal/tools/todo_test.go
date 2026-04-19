package tools

import (
	"encoding/json"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func executeTodoWrite(t *testing.T, store TodoStore, input todoWriteInput) (ToolResult, []internal.Event, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	tool := &TodoWriteTool{Store: store}
	ch := makeEventsCh()
	result, execErr := tool.Execute(raw, ch)
	close(ch)

	var events []internal.Event
	for e := range ch {
		events = append(events, e)
	}
	return result, events, execErr
}

func executeTodoRead(t *testing.T, store TodoStore) (ToolResult, []internal.Event, error) {
	t.Helper()
	tool := &TodoReadTool{Store: store}
	ch := makeEventsCh()
	result, execErr := tool.Execute(json.RawMessage(`{}`), ch)
	close(ch)

	var events []internal.Event
	for e := range ch {
		events = append(events, e)
	}
	return result, events, execErr
}

func TestTodoWriteTool_IncrementalUpdates(t *testing.T) {
	store := NewTodoStore()

	_, _, err := executeTodoWrite(t, store, todoWriteInput{Todos: []TodoItem{
		{Content: "Inspect code", Status: "pending"},
		{Content: "Implement change", Status: "pending"},
	}})
	if err != nil {
		t.Fatalf("unexpected error creating initial todos: %v", err)
	}

	result, _, err := executeTodoWrite(t, store, todoWriteInput{Todos: []TodoItem{
		{Content: "Inspect code", Status: "completed"},
		{Content: "Implement change", Status: "in_progress"},
		{Content: "Verify change", Status: "pending"},
	}})
	if err != nil {
		t.Fatalf("unexpected error updating todos: %v", err)
	}
	if result.Content != "Todo list updated (3 total, 1 completed, 3 changes applied)" {
		t.Fatalf("unexpected result content: %q", result.Content)
	}

	todos := store.Get()
	if len(todos) != 3 {
		t.Fatalf("expected 3 todos, got %d", len(todos))
	}
	if todos[0].Content != "Inspect code" || todos[0].Status != "completed" {
		t.Fatalf("unexpected first todo: %+v", todos[0])
	}
	if todos[1].Content != "Implement change" || todos[1].Status != "in_progress" {
		t.Fatalf("unexpected second todo: %+v", todos[1])
	}
	if todos[2].Content != "Verify change" || todos[2].Status != "pending" {
		t.Fatalf("unexpected third todo: %+v", todos[2])
	}
}

func TestTodoWriteTool_CancelRemovesItem(t *testing.T) {
	store := NewTodoStore()
	store.Set([]TodoItem{
		{Content: "Inspect code", Status: "completed"},
		{Content: "Implement change", Status: "in_progress"},
	})

	_, _, err := executeTodoWrite(t, store, todoWriteInput{Todos: []TodoItem{
		{Content: "Inspect code", Status: "cancelled"},
	}})
	if err != nil {
		t.Fatalf("unexpected error cancelling todo: %v", err)
	}

	todos := store.Get()
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo after cancellation, got %d", len(todos))
	}
	if todos[0].Content != "Implement change" {
		t.Fatalf("unexpected remaining todo: %+v", todos[0])
	}
}

func TestTodoWriteTool_RejectsMultipleInProgress(t *testing.T) {
	store := NewTodoStore()

	_, _, err := executeTodoWrite(t, store, todoWriteInput{Todos: []TodoItem{
		{Content: "Task one", Status: "in_progress"},
		{Content: "Task two", Status: "in_progress"},
	}})
	if err == nil {
		t.Fatal("expected error for multiple in_progress todos")
	}
}

func TestTodoWriteTool_AllCompletedClearsList(t *testing.T) {
	store := NewTodoStore()
	store.Set([]TodoItem{{Content: "Inspect code", Status: "in_progress"}})

	result, events, err := executeTodoWrite(t, store, todoWriteInput{Todos: []TodoItem{
		{Content: "Inspect code", Status: "completed"},
	}})
	if err != nil {
		t.Fatalf("unexpected error completing todo: %v", err)
	}
	if result.Content != "All todos completed. List cleared." {
		t.Fatalf("unexpected result content: %q", result.Content)
	}
	if len(store.Get()) != 0 {
		t.Fatalf("expected store to be cleared, got %v", store.Get())
	}
	if len(events) != 1 || events[0].Message != "All todos completed - cleared" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestTodoWriteTool_RejectsDuplicateContentInSingleUpdate(t *testing.T) {
	store := NewTodoStore()

	_, _, err := executeTodoWrite(t, store, todoWriteInput{Todos: []TodoItem{
		{Content: "Inspect code", Status: "pending"},
		{Content: "Inspect code", Status: "completed"},
	}})
	if err == nil {
		t.Fatal("expected duplicate content error")
	}
}

func TestTodoReadTool_ReturnsJSON(t *testing.T) {
	store := NewTodoStore()
	store.Set([]TodoItem{
		{Content: "Inspect code", Status: "completed"},
		{Content: "Implement change", Status: "in_progress"},
	})

	result, events, err := executeTodoRead(t, store)
	if err != nil {
		t.Fatalf("unexpected error reading todos: %v", err)
	}
	if result.Content != "[\n  {\n    \"content\": \"Inspect code\",\n    \"status\": \"completed\"\n  },\n  {\n    \"content\": \"Implement change\",\n    \"status\": \"in_progress\"\n  }\n]" {
		t.Fatalf("unexpected todo json: %q", result.Content)
	}
	if len(events) != 1 || events[0].Message != "1/2 completed" {
		t.Fatalf("unexpected events: %+v", events)
	}
}
