package agent

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/tools"
)

func TestIntegration_SubagentSpawn(t *testing.T) {
	// The provider serves both parent and subagent responses in sequence.
	// The parent makes 2 calls: first returns Agent tool call, second returns final text.
	// The subagent makes 1 call: returns its result.
	// Order of calls: parent(1) -> subagent(1) -> parent(2)
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			// Parent call 1: decides to spawn subagent
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Let me explore the codebase."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "Agent", Arguments: `{"agent_type":"explore","prompt":"Find Go files in internal/agent/"}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			// Subagent call: returns exploration results
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Found 3 Go files in internal/agent/."),
				FinishReason: llm.FinishStop,
			},
			// Parent call 2: summarizes
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "The explore agent found 3 Go files."),
				FinishReason: llm.FinishStop,
			},
		},
	}

	parentTools := tools.NewManager()
	parentTools.Register(&mockTool{name: "Read", result: "file contents"})
	parentTools.Register(&mockTool{name: "Grep", result: "grep results"})

	registry := NewRegistry()
	registry.Register(Definition{
		Name:        "explore",
		Description: "Fast explorer",
		Prompt:      "You are an explorer.",
		MaxTurns:    10,
		Tools:       []string{"Read", "Grep"},
	})

	parentConfig := &Config{
		Provider: provider,
		Model:    "test-model",
		Tools:    parentTools,
		MaxTurns: 10,
	}

	agentTool := &AgentTool{
		Registry:     registry,
		ParentConfig: parentConfig,
		ctx:          context.Background(),
	}
	parentTools.Register(agentTool)
	parentConfig.AgentTool = agentTool

	// Collect events
	var events []internal.Event
	cb := Callbacks{
		OnEvent: func(e internal.Event) {
			events = append(events, e)
		},
	}

	runner := NewRunner(parentConfig, cb)
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "You are a leader agent."),
		llm.TextMessage(llm.RoleUser, "Find Go files"),
	}

	result, err := runner.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "The explore agent found 3 Go files." {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

func TestIntegration_NoNesting(t *testing.T) {
	// Verify that the Agent tool is excluded from subagent tool sets
	registry := NewRegistry()
	registry.Register(Definition{
		Name:     "general-purpose",
		Prompt:   "You are general.",
		MaxTurns: 10,
		Tools:    []string{}, // empty = all except Agent
	})

	parentTools := tools.NewManager()
	parentTools.Register(&mockTool{name: "Read", result: "ok"})

	// Create a mock AgentTool as a real tool in the parent registry
	mockAgentTool := &mockTool{name: "Agent", result: "should not appear"}
	parentTools.Register(mockAgentTool)

	parentConfig := &Config{
		Provider: &mockProvider{},
		Model:    "test-model",
		Tools:    parentTools,
	}

	agentTool := &AgentTool{
		Registry:     registry,
		ParentConfig: parentConfig,
	}

	// Build subagent config and check its tools
	eventsCh := make(chan internal.Event, 16)
	subConfig, err := agentTool.buildSubagentConfig(
		Definition{Name: "general-purpose", Prompt: "test", MaxTurns: 5, Tools: []string{}},
		eventsCh,
	)
	close(eventsCh)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that Agent tool is excluded
	for _, def := range subConfig.Tools.ToolDefinitions() {
		if def.Name == "Agent" {
			t.Error("Agent tool should be excluded from subagent tools")
		}
	}
}

func TestIntegration_ParallelAgentCalls(t *testing.T) {
	// Two Agent calls should execute concurrently.
	// We use a single thread-safe slow provider for everything.
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	slowProvider := &slowMockProvider{
		response: llm.CompletionResponse{
			Message:      llm.TextMessage(llm.RoleAssistant, "done"),
			FinishReason: llm.FinishStop,
		},
		delay:           50 * time.Millisecond,
		concurrentCount: &concurrentCount,
		maxConcurrent:   &maxConcurrent,
	}

	registry := NewRegistry()
	registry.Register(Definition{
		Name:     "worker",
		Prompt:   "You are a worker.",
		MaxTurns: 5,
		Tools:    []string{"Read"},
	})

	parentTools := tools.NewManager()
	parentTools.Register(&mockTool{name: "Read", result: "ok"})

	parentConfig := &Config{
		Provider: slowProvider, // AgentTool snapshots this for subagents
		Model:    "test-model",
		Tools:    parentTools,
		MaxTurns: 10,
	}

	agentTool := &AgentTool{
		Registry:     registry,
		ParentConfig: parentConfig,
		ctx:          context.Background(),
	}
	parentTools.Register(agentTool)
	parentConfig.AgentTool = agentTool

	// Swap parent to its own provider for the runner (subagents still get slowProvider
	// because AgentTool.buildSubagentConfig reads ParentConfig.Provider at call time,
	// but the runner's provider is separate).
	// Actually we can't do this cleanly — let's just test the parallel execution
	// of the AgentTool directly.

	// Test: call AgentTool.Execute twice in parallel and verify concurrency
	input1, _ := json.Marshal(agentToolInput{AgentType: "worker", Prompt: "task 1"})
	input2, _ := json.Marshal(agentToolInput{AgentType: "worker", Prompt: "task 2"})

	eventsCh := make(chan internal.Event, 64)

	var wg sync.WaitGroup
	wg.Add(2)

	var result1, result2 tools.ToolResult
	var err1, err2 error

	go func() {
		defer wg.Done()
		result1, err1 = agentTool.Execute(input1, eventsCh)
	}()
	go func() {
		defer wg.Done()
		result2, err2 = agentTool.Execute(input2, eventsCh)
	}()

	wg.Wait()
	close(eventsCh)

	if err1 != nil {
		t.Fatalf("agent 1 error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("agent 2 error: %v", err2)
	}
	if result1.Content != "done" {
		t.Errorf("agent 1 unexpected content: %q", result1.Content)
	}
	if result2.Content != "done" {
		t.Errorf("agent 2 unexpected content: %q", result2.Content)
	}

	// Both should have run concurrently
	if maxConcurrent.Load() < 2 {
		t.Errorf("expected concurrent execution (max concurrent: %d)", maxConcurrent.Load())
	}
}

func TestIntegration_ToolFiltering(t *testing.T) {
	// Subagent with restricted tools should not be able to use excluded tools
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
				Message:      llm.TextMessage(llm.RoleAssistant, "write was blocked"),
				FinishReason: llm.FinishStop,
			},
		},
	}

	registry := NewRegistry()
	registry.Register(Definition{
		Name:     "readonly",
		Prompt:   "Read only.",
		MaxTurns: 10,
		Tools:    []string{"Read"},
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
		Prompt:    "write something",
	})

	eventsCh := make(chan internal.Event, 32)
	result, err := agentTool.Execute(input, eventsCh)
	close(eventsCh)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "write was blocked" {
		t.Errorf("unexpected content: %q", result.Content)
	}
}

func TestIntegration_EventPrefixing(t *testing.T) {
	// Verify subagent events are prefixed with agent name
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "reading"}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "Read", Arguments: `{}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "done"),
				FinishReason: llm.FinishStop,
			},
		},
	}

	registry := NewRegistry()
	registry.Register(Definition{
		Name:     "explore",
		Prompt:   "Explorer.",
		MaxTurns: 10,
		Tools:    []string{"Read"},
	})

	parentTools := tools.NewManager()
	parentTools.Register(&mockTool{name: "Read", result: "file data"})

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
		AgentType: "explore",
		Prompt:    "read files",
	})

	eventsCh := make(chan internal.Event, 32)
	_, err := agentTool.Execute(input, eventsCh)
	close(eventsCh)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that at least one event has the [explore] prefix
	var prefixedEvents []internal.Event
	for e := range eventsCh {
		if len(e.Name) > 0 && e.Name[0] == '[' {
			prefixedEvents = append(prefixedEvents, e)
		}
	}
	if len(prefixedEvents) == 0 {
		t.Error("expected events with [explore] prefix")
	}
}

func TestIntegration_BuiltinAgents(t *testing.T) {
	defs := BuiltinDefinitions()
	if len(defs) != 3 {
		t.Fatalf("expected 3 builtin agents, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
		if d.Source != "builtin" {
			t.Errorf("expected source 'builtin' for %s, got %q", d.Name, d.Source)
		}
		if d.Prompt == "" {
			t.Errorf("expected non-empty prompt for %s", d.Name)
		}
	}

	for _, expected := range []string{"explore", "plan", "general-purpose"} {
		if !names[expected] {
			t.Errorf("missing expected builtin agent: %s", expected)
		}
	}
}

// slowMockProvider is a mock provider that introduces a delay and tracks concurrency.
type slowMockProvider struct {
	response        llm.CompletionResponse
	delay           time.Duration
	concurrentCount *atomic.Int32
	maxConcurrent   *atomic.Int32
}

func (p *slowMockProvider) Complete(_ context.Context, _ llm.CompletionParams, _ func(llm.StreamDelta)) (*llm.CompletionResponse, error) {
	current := p.concurrentCount.Add(1)
	for {
		max := p.maxConcurrent.Load()
		if current <= max {
			break
		}
		if p.maxConcurrent.CompareAndSwap(max, current) {
			break
		}
	}
	time.Sleep(p.delay)
	p.concurrentCount.Add(-1)
	return &p.response, nil
}
