package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/tools"
)

func TestAgentToolBasic(t *testing.T) {
	// Create a mock provider that echoes the user prompt
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Found 5 test files."),
				FinishReason: llm.FinishStop,
			},
		},
	}

	registry := NewRegistry()
	registry.Register(Definition{
		Name:        "explore",
		Description: "Fast explorer",
		Prompt:      "You are an explorer.",
		MaxTurns:    10,
		Tools:       []string{"Read"},
	})

	parentTools := tools.NewManager()
	parentTools.Register(&mockTool{name: "Read", result: "file contents"})
	parentTools.Register(&mockTool{name: "Write", result: "ok"})

	parentConfig := &Config{
		Provider: provider,
		Model:    "test-model",
		Tools:    parentTools,
	}

	agentTool := &AgentTool{
		Registry:     registry,
		ParentConfig: parentConfig,
		ctx:          context.Background(),
	}

	// Execute the tool
	input, _ := json.Marshal(agentToolInput{
		AgentType: "explore",
		Prompt:    "Find test files",
	})

	eventsCh := make(chan internal.Event, 32)
	result, err := agentTool.Execute(input, eventsCh)
	close(eventsCh)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "Found 5 test files." {
		t.Errorf("expected 'Found 5 test files.', got %q", result.Content)
	}

	// Verify events were prefixed
	var events []internal.Event
	for e := range eventsCh {
		events = append(events, e)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (start + finish), got %d", len(events))
	}
	if events[0].Message != "Starting subagent" {
		t.Errorf("expected 'Starting subagent', got %q", events[0].Message)
	}
}

func TestAgentToolExcludesAgentFromSubagent(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "done"),
				FinishReason: llm.FinishStop,
			},
		},
	}

	registry := NewRegistry()
	registry.Register(Definition{
		Name:        "general-purpose",
		Description: "General",
		Prompt:      "You are general.",
		MaxTurns:    10,
		Tools:       []string{}, // empty = all except Agent
	})

	parentTools := tools.NewManager()
	parentTools.Register(&mockTool{name: "Read", result: "ok"})
	parentTools.Register(&mockTool{name: "Agent", result: "should not be here"})

	parentConfig := &Config{
		Provider: provider,
		Model:    "test-model",
		Tools:    parentTools,
	}

	agentTool := &AgentTool{
		Registry:     registry,
		ParentConfig: parentConfig,
		ctx:          context.Background(),
	}

	input, _ := json.Marshal(agentToolInput{
		AgentType: "general-purpose",
		Prompt:    "do something",
	})

	eventsCh := make(chan internal.Event, 32)
	result, err := agentTool.Execute(input, eventsCh)
	close(eventsCh)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "done" {
		t.Errorf("unexpected content: %q", result.Content)
	}
}

func TestAgentToolUnknownType(t *testing.T) {
	registry := NewRegistry()
	parentConfig := &Config{
		Provider: &mockProvider{},
		Tools:    tools.NewManager(),
	}

	agentTool := &AgentTool{
		Registry:     registry,
		ParentConfig: parentConfig,
		ctx:          context.Background(),
	}

	input, _ := json.Marshal(agentToolInput{
		AgentType: "nonexistent",
		Prompt:    "do something",
	})

	eventsCh := make(chan internal.Event, 32)
	_, err := agentTool.Execute(input, eventsCh)
	close(eventsCh)

	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestAgentToolToolFiltering(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "trying write"}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "Write", Arguments: `{}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "write failed as expected"),
				FinishReason: llm.FinishStop,
			},
		},
	}

	registry := NewRegistry()
	registry.Register(Definition{
		Name:     "readonly",
		Prompt:   "Read only agent.",
		MaxTurns: 10,
		Tools:    []string{"Read"}, // only Read allowed
	})

	parentTools := tools.NewManager()
	parentTools.Register(&mockTool{name: "Read", result: "ok"})
	parentTools.Register(&mockTool{name: "Write", result: "ok"})

	parentConfig := &Config{
		Provider: provider,
		Model:    "test-model",
		Tools:    parentTools,
	}

	agentTool := &AgentTool{
		Registry:     registry,
		ParentConfig: parentConfig,
		ctx:          context.Background(),
	}

	input, _ := json.Marshal(agentToolInput{
		AgentType: "readonly",
		Prompt:    "try to write",
	})

	eventsCh := make(chan internal.Event, 32)
	result, err := agentTool.Execute(input, eventsCh)
	close(eventsCh)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The subagent should have gotten an error when trying to use Write,
	// then responded with text
	if result.Content != "write failed as expected" {
		t.Errorf("unexpected content: %q", result.Content)
	}
}
