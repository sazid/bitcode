package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/tools"
)

type mockToolRegistry struct {
	tools map[string]bool
}

func newMockToolRegistry(names ...string) *mockToolRegistry {
	m := &mockToolRegistry{tools: make(map[string]bool)}
	for _, n := range names {
		m.tools[n] = true
	}
	return m
}

func (m *mockToolRegistry) ExecuteTool(toolName string, input string, eventsCh chan<- internal.Event) (tools.ToolResult, error) {
	if !m.tools[toolName] {
		return tools.ToolResult{}, fmt.Errorf("unknown tool: %s", toolName)
	}
	return tools.ToolResult{Content: "executed: " + toolName}, nil
}

func (m *mockToolRegistry) ToolDefinitions() []tools.ToolDefinition {
	var defs []tools.ToolDefinition
	for name := range m.tools {
		defs = append(defs, tools.ToolDefinition{Name: name, Type: "function"})
	}
	return defs
}

func TestFilteredRegistry_AllowedSubset(t *testing.T) {
	mock := newMockToolRegistry("Read", "Write", "Bash")
	fr := NewFilteredRegistry(mock, []string{"Read", "Bash"})

	// Allowed tool should work
	result, err := fr.ExecuteTool("Read", "", nil)
	if err != nil {
		t.Fatalf("expected no error for allowed tool Read, got: %v", err)
	}
	if result.Content != "executed: Read" {
		t.Fatalf("unexpected result content: %s", result.Content)
	}

	// Disallowed tool should error
	_, err = fr.ExecuteTool("Write", "", nil)
	if err == nil {
		t.Fatal("expected error for disallowed tool Write")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected 'not available' in error, got: %v", err)
	}

	// ToolDefinitions should return only allowed tools
	defs := fr.ToolDefinitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool definitions, got %d", len(defs))
	}
}

func TestFilteredRegistry_EmptyAllowed(t *testing.T) {
	mock := newMockToolRegistry("Read", "Write", "Bash")
	fr := NewFilteredRegistry(mock, []string{})

	// All tools should pass through
	for _, name := range []string{"Read", "Write", "Bash"} {
		result, err := fr.ExecuteTool(name, "", nil)
		if err != nil {
			t.Fatalf("expected no error for tool %s, got: %v", name, err)
		}
		if result.Content != "executed: "+name {
			t.Fatalf("unexpected result for %s: %s", name, result.Content)
		}
	}

	// ToolDefinitions should return all tools
	defs := fr.ToolDefinitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 tool definitions, got %d", len(defs))
	}
}

func TestFilteredRegistry_SingleAllowed(t *testing.T) {
	mock := newMockToolRegistry("Read", "Write", "Bash")
	fr := NewFilteredRegistry(mock, []string{"Read"})

	defs := fr.ToolDefinitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool definition, got %d", len(defs))
	}
	if defs[0].Name != "Read" {
		t.Fatalf("expected tool name Read, got %s", defs[0].Name)
	}
}
