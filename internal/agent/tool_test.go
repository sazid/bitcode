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
	if result.Content != "<explore_result>\n<task>Find test files</task>\n<report>Found 5 test files.</report>\n</explore_result>" {
		t.Errorf("expected structured explore result, got %q", result.Content)
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

func TestNormalizeSubagentOutput(t *testing.T) {
	tests := []struct {
		name      string
		agentType string
		task      string
		output    string
		want      string
	}{
		{
			name:      "plain explore output becomes report",
			agentType: "explore",
			task:      "Find test files",
			output:    "Found 5 test files.",
			want:      "<explore_result>\n<task>Find test files</task>\n<report>Found 5 test files.</report>\n</explore_result>",
		},
		{
			name:      "markdown explore sections become tags",
			agentType: "explore",
			task:      "Trace auth",
			output:    "Preamble\n\n## Summary\nAuth starts in main.\n\n## Findings\nCall path uses <token> & cache.\n\n## Relevant Files\n- app/main.go\n\n## Caveat\nNeeds live config.",
			want:      "<explore_result>\n<task>Trace auth</task>\n<summary>Auth starts in main.</summary>\n<findings>Call path uses &lt;token&gt; &amp; cache.</findings>\n<relevant_files>- app/main.go</relevant_files>\n<notes>Preamble\n\n## Caveat\nNeeds live config.</notes>\n</explore_result>",
		},
		{
			name:      "plan output uses plan result tag",
			agentType: "plan",
			task:      "Plan refactor",
			output:    "## Steps\n1. Move interfaces.\n\n## Verification\ngo test ./internal/agent",
			want:      "<plan_result>\n<task>Plan refactor</task>\n<steps>1. Move interfaces.</steps>\n<verification>go test ./internal/agent</verification>\n</plan_result>",
		},
		{
			name:      "already structured output is preserved",
			agentType: "explore",
			task:      "Find files",
			output:    "  <explore_result>\n<summary>Done</summary>\n</explore_result>  ",
			want:      "<explore_result>\n<summary>Done</summary>\n</explore_result>",
		},
		{
			name:      "general purpose output is untouched",
			agentType: "general-purpose",
			task:      "Do it",
			output:    "Done",
			want:      "Done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSubagentOutput(tt.agentType, tt.task, tt.output)
			if got != tt.want {
				t.Fatalf("normalizeSubagentOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}
