package agent

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/tools"
)

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	responses []llm.CompletionResponse
	callIdx   int
	mu        sync.Mutex
}

func (m *mockProvider) Complete(_ context.Context, _ llm.CompletionParams, _ func(llm.StreamDelta)) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callIdx >= len(m.responses) {
		return &llm.CompletionResponse{
			Message:      llm.TextMessage(llm.RoleAssistant, "no more responses"),
			FinishReason: llm.FinishStop,
		}, nil
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return &resp, nil
}

// mockTool implements tools.Tool for testing.
type mockTool struct {
	name   string
	result string
}

func (t *mockTool) Name() string              { return t.name }
func (t *mockTool) Description() string       { return "mock " + t.name }
func (t *mockTool) ParametersSchema() map[string]any { return map[string]any{"type": "object"} }
func (t *mockTool) Execute(_ json.RawMessage, _ chan<- internal.Event) (tools.ToolResult, error) {
	return tools.ToolResult{Content: t.result}, nil
}

func TestRunnerStopResponse(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Hello!"),
				FinishReason: llm.FinishStop,
			},
		},
	}

	cfg := &Config{
		Provider: provider,
		Tools:    tools.NewManager(),
		MaxTurns: 10,
	}

	runner := NewRunner(cfg, Callbacks{})
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "You are a test agent."),
		llm.TextMessage(llm.RoleUser, "Hi"),
	}

	result, err := runner.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Hello!" {
		t.Errorf("expected output 'Hello!', got %q", result.Output)
	}
	// system + user + assistant = 3 messages
	if len(result.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result.Messages))
	}
}

func TestRunnerToolCall(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Let me check."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "Read", Arguments: `{"path": "test.go"}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Done reading."),
				FinishReason: llm.FinishStop,
			},
		},
	}

	mgr := tools.NewManager()
	mgr.Register(&mockTool{name: "Read", result: "file contents here"})

	cfg := &Config{
		Provider: provider,
		Tools:    mgr,
		MaxTurns: 10,
	}

	runner := NewRunner(cfg, Callbacks{})
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "You are a test agent."),
		llm.TextMessage(llm.RoleUser, "Read test.go"),
	}

	result, err := runner.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Done reading." {
		t.Errorf("expected output 'Done reading.', got %q", result.Output)
	}
	// system + user + assistant(tool_call) + tool_result + assistant(done) = 5
	if len(result.Messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(result.Messages))
	}
}

func TestRunnerMaxTurns(t *testing.T) {
	// Provider always returns tool calls — should hit max turns
	provider := &mockProvider{
		responses: make([]llm.CompletionResponse, 100),
	}
	for i := range provider.responses {
		provider.responses[i] = llm.CompletionResponse{
			Message: llm.Message{
				Role:    llm.RoleAssistant,
				Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "calling"}},
				ToolCalls: []llm.ToolCall{
					{ID: "tc", Name: "Read", Arguments: `{}`},
				},
			},
			FinishReason: llm.FinishToolCalls,
		}
	}

	mgr := tools.NewManager()
	mgr.Register(&mockTool{name: "Read", result: "ok"})

	cfg := &Config{
		Provider: provider,
		Tools:    mgr,
		MaxTurns: 3,
	}

	runner := NewRunner(cfg, Callbacks{})
	messages := []llm.Message{
		llm.TextMessage(llm.RoleUser, "loop"),
	}

	result, err := runner.Run(context.Background(), messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have stopped after 3 turns
	if provider.callIdx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.callIdx)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRunnerContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "should not reach"),
				FinishReason: llm.FinishStop,
			},
		},
	}

	cfg := &Config{
		Provider: provider,
		Tools:    tools.NewManager(),
		MaxTurns: 10,
	}

	runner := NewRunner(cfg, Callbacks{})
	result, err := runner.Run(ctx, []llm.Message{llm.TextMessage(llm.RoleUser, "hi")})

	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result even on cancellation")
	}
	if provider.callIdx != 0 {
		t.Errorf("expected 0 provider calls on cancelled context, got %d", provider.callIdx)
	}
}
