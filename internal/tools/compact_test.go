package tools

import (
	"encoding/json"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func TestCompactState_SetAndTake(t *testing.T) {
	state := NewCompactState()

	// Initially empty
	if got := state.TakeSummary(); got != "" {
		t.Fatalf("expected empty summary, got %q", got)
	}

	// Set and take
	state.SetSummary("work done so far")
	if got := state.TakeSummary(); got != "work done so far" {
		t.Fatalf("expected summary, got %q", got)
	}

	// Take again should be empty (cleared)
	if got := state.TakeSummary(); got != "" {
		t.Fatalf("expected empty after take, got %q", got)
	}
}

func TestCompactState_OverwritesPending(t *testing.T) {
	state := NewCompactState()
	state.SetSummary("first")
	state.SetSummary("second")

	if got := state.TakeSummary(); got != "second" {
		t.Fatalf("expected latest summary, got %q", got)
	}
}

func TestCompactTool_Execute(t *testing.T) {
	state := NewCompactState()
	tool := &CompactTool{State: state}

	eventsCh := make(chan internal.Event, 8)
	input, _ := json.Marshal(compactInput{Summary: "user asked to refactor auth module; read 3 files; created auth_v2.go"})

	result, err := tool.Execute(input, eventsCh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content == "" {
		t.Fatal("expected non-empty result content")
	}

	// Summary should be stored in state
	if got := state.TakeSummary(); got != "user asked to refactor auth module; read 3 files; created auth_v2.go" {
		t.Fatalf("expected summary in state, got %q", got)
	}

	// Event should have been emitted
	if len(eventsCh) != 1 {
		t.Fatalf("expected 1 event, got %d", len(eventsCh))
	}
	e := <-eventsCh
	if e.Name != "Compact" {
		t.Fatalf("expected Compact event, got %q", e.Name)
	}
}

func TestCompactTool_EmptySummary(t *testing.T) {
	state := NewCompactState()
	tool := &CompactTool{State: state}

	eventsCh := make(chan internal.Event, 8)
	input, _ := json.Marshal(compactInput{Summary: ""})

	_, err := tool.Execute(input, eventsCh)
	if err == nil {
		t.Fatal("expected error for empty summary")
	}
}

func TestCompactTool_InvalidJSON(t *testing.T) {
	state := NewCompactState()
	tool := &CompactTool{State: state}

	eventsCh := make(chan internal.Event, 8)
	_, err := tool.Execute(json.RawMessage(`{invalid`), eventsCh)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
