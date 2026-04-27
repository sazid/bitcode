package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/reminder"
	"github.com/sazid/bitcode/internal/tools"
)

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	responses []llm.CompletionResponse
	requests  []llm.CompletionParams
	callIdx   int
	mu        sync.Mutex
}

func (m *mockProvider) Complete(_ context.Context, params llm.CompletionParams, _ func(llm.StreamDelta)) (*llm.CompletionResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, params)
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
	err    error
}

func (t *mockTool) Name() string                     { return t.name }
func (t *mockTool) Description() string              { return "mock " + t.name }
func (t *mockTool) ParametersSchema() map[string]any { return map[string]any{"type": "object"} }
func (t *mockTool) Execute(_ json.RawMessage, _ chan<- internal.Event) (tools.ToolResult, error) {
	if t.err != nil {
		return tools.ToolResult{}, t.err
	}
	return tools.ToolResult{Content: t.result}, nil
}

type slowMockTool struct {
	name   string
	result string
	wait   chan struct{}
}

func (t *slowMockTool) Name() string                     { return t.name }
func (t *slowMockTool) Description() string              { return "mock " + t.name }
func (t *slowMockTool) ParametersSchema() map[string]any { return map[string]any{"type": "object"} }
func (t *slowMockTool) Execute(_ json.RawMessage, _ chan<- internal.Event) (tools.ToolResult, error) {
	<-t.wait
	return tools.ToolResult{Content: t.result}, nil
}

func TestParseAgentType(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{
			name: "standard args",
			args: `{"agent_type":"explore","prompt":"Find files"}`,
			want: "explore",
		},
		{
			name: "reordered fields and whitespace",
			args: `{
				"prompt": "Plan the work",
				"agent_type": " plan "
			}`,
			want: "plan",
		},
		{
			name: "missing agent type",
			args: `{"prompt":"Find files"}`,
			want: "",
		},
		{
			name: "invalid json",
			args: `{"agent_type":`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAgentType(tt.args); got != tt.want {
				t.Fatalf("parseAgentType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppendDelegatedAgent(t *testing.T) {
	history := []string{"a", "b", "c", "d", "e", "f"}
	got := appendDelegatedAgent(history, `{"prompt":"Find files","agent_type":"explore"}`)
	want := []string{"b", "c", "d", "e", "f", "explore"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("appendDelegatedAgent() = %#v, want %#v", got, want)
	}

	unchanged := appendDelegatedAgent(got, `{"prompt":"Find files"}`)
	if strings.Join(unchanged, ",") != strings.Join(got, ",") {
		t.Fatalf("appendDelegatedAgent() changed history for missing agent_type: %#v", unchanged)
	}
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

// flakyProvider fails the first N calls, then succeeds.
type flakyProvider struct {
	failCount  int // how many times to fail before succeeding
	callCount  int
	mu         sync.Mutex
	successMsg string
}

func (f *flakyProvider) Complete(_ context.Context, _ llm.CompletionParams, _ func(llm.StreamDelta)) (*llm.CompletionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.callCount <= f.failCount {
		return nil, fmt.Errorf("API error on call %d", f.callCount)
	}
	return &llm.CompletionResponse{
		Message:      llm.TextMessage(llm.RoleAssistant, f.successMsg),
		FinishReason: llm.FinishStop,
	}, nil
}

func TestRunnerRetryOnError(t *testing.T) {
	provider := &flakyProvider{failCount: 2, successMsg: "recovered"}

	var errors []string
	cfg := &Config{
		Provider:   provider,
		Tools:      tools.NewManager(),
		MaxTurns:   10,
		MaxRetries: 5,
	}
	cb := Callbacks{
		OnError: func(err error) { errors = append(errors, err.Error()) },
	}

	runner := NewRunner(cfg, cb)
	result, err := runner.Run(context.Background(), []llm.Message{
		llm.TextMessage(llm.RoleUser, "hi"),
	})

	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result.Output != "recovered" {
		t.Errorf("expected output 'recovered', got %q", result.Output)
	}
	if provider.callCount != 3 {
		t.Errorf("expected 3 provider calls (2 failures + 1 success), got %d", provider.callCount)
	}
	// 2 error messages + 2 retry messages = 4
	if len(errors) != 4 {
		t.Errorf("expected 4 error callbacks (2 errors + 2 retry notices), got %d: %v", len(errors), errors)
	}
}

func TestRunnerRetryExhausted(t *testing.T) {
	provider := &flakyProvider{failCount: 10, successMsg: "should not reach"}

	cfg := &Config{
		Provider:   provider,
		Tools:      tools.NewManager(),
		MaxTurns:   10,
		MaxRetries: 2,
	}

	runner := NewRunner(cfg, Callbacks{})
	_, err := runner.Run(context.Background(), []llm.Message{
		llm.TextMessage(llm.RoleUser, "hi"),
	})

	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 1 initial + 2 retries = 3 calls
	if provider.callCount != 3 {
		t.Errorf("expected 3 provider calls (1 + 2 retries), got %d", provider.callCount)
	}
}

func TestRunnerStopWithIncompleteTodosInjectsStructuredReminder(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "I think I am done."),
				FinishReason: llm.FinishStop,
			},
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Let me finish the remaining tasks."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "TodoWrite", Arguments: `{"todos":[{"content":"Fix bug","status":"completed"},{"content":"Run tests","status":"completed"}]}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Now I am actually done."),
				FinishReason: llm.FinishStop,
			},
		},
	}

	store := tools.NewTodoStore()
	store.Set([]tools.TodoItem{
		{Content: "Inspect code", Status: "completed"},
		{Content: "Fix bug", Status: "in_progress"},
		{Content: "Run tests", Status: "pending"},
	})

	mgr := tools.NewManager()
	mgr.Register(&tools.TodoWriteTool{Store: store})

	cfg := &Config{
		Provider:  provider,
		Tools:     mgr,
		TodoStore: store,
		MaxTurns:  6,
	}

	runner := NewRunner(cfg, Callbacks{})
	result, err := runner.Run(context.Background(), []llm.Message{
		llm.TextMessage(llm.RoleSystem, "You are a test agent."),
		llm.TextMessage(llm.RoleUser, "Finish the task"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Now I am actually done." {
		t.Fatalf("unexpected final output: %q", result.Output)
	}
	if provider.callIdx != 3 {
		t.Fatalf("expected 3 provider calls, got %d", provider.callIdx)
	}
	if len(result.Messages) != 7 {
		t.Fatalf("expected 7 messages, got %d", len(result.Messages))
	}
	reminder := result.Messages[3].Text()
	if reminder == "" {
		t.Fatal("expected injected reminder text")
	}
	if want := "You tried to stop with incomplete todos."; !contains(reminder, want) {
		t.Fatalf("expected reminder to contain %q, got %q", want, reminder)
	}
	if want := "- [in_progress] Fix bug"; !contains(reminder, want) {
		t.Fatalf("expected reminder to contain %q, got %q", want, reminder)
	}
	if want := "- [pending] Run tests"; !contains(reminder, want) {
		t.Fatalf("expected reminder to contain %q, got %q", want, reminder)
	}
	if len(store.Get()) != 0 {
		t.Fatalf("expected todos to be cleared after completion, got %+v", store.Get())
	}
}

func TestRunnerToolFailureReturnsReflectionMessage(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Trying a read."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "Read", Arguments: `{"path":"missing.txt"}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Handled the failure."),
				FinishReason: llm.FinishStop,
			},
		},
	}

	mgr := tools.NewManager()
	mgr.Register(&mockTool{name: "Read", err: fmt.Errorf("boom")})

	cfg := &Config{
		Provider: provider,
		Tools:    mgr,
		MaxTurns: 5,
	}

	runner := NewRunner(cfg, Callbacks{})
	result, err := runner.Run(context.Background(), []llm.Message{
		llm.TextMessage(llm.RoleUser, "Read the file"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Handled the failure." {
		t.Fatalf("unexpected final output: %q", result.Output)
	}
	if len(result.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result.Messages))
	}
	toolMsg := result.Messages[2].Text()
	if want := "Tool call failed for Read."; !contains(toolMsg, want) {
		t.Fatalf("expected tool message to contain %q, got %q", want, toolMsg)
	}
	if want := "Arguments: {\"path\":\"missing.txt\"}"; !contains(toolMsg, want) {
		t.Fatalf("expected tool message to contain %q, got %q", want, toolMsg)
	}
	if want := "Reflect on why this failed"; !contains(toolMsg, want) {
		t.Fatalf("expected tool message to contain %q, got %q", want, toolMsg)
	}
}

func TestRunnerParallelizesContiguousReadOnlyToolCalls(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Inspecting files."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "Read", Arguments: `{"file_path":"a.go"}`},
						{ID: "tc2", Name: "Glob", Arguments: `{"pattern":"**/*.go"}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Done."),
				FinishReason: llm.FinishStop,
			},
		},
	}

	wait := make(chan struct{})
	mgr := tools.NewManager()
	mgr.Register(&slowMockTool{name: "Read", result: "read result", wait: wait})
	mgr.Register(&slowMockTool{name: "Glob", result: "glob result", wait: wait})

	cfg := &Config{
		Provider: provider,
		Tools:    mgr,
		MaxTurns: 4,
	}

	runner := NewRunner(cfg, Callbacks{})
	resultCh := make(chan struct {
		result *Result
		err    error
	}, 1)
	go func() {
		result, err := runner.Run(context.Background(), []llm.Message{
			llm.TextMessage(llm.RoleUser, "Inspect the repo"),
		})
		resultCh <- struct {
			result *Result
			err    error
		}{result: result, err: err}
	}()

	select {
	case outcome := <-resultCh:
		t.Fatalf("runner returned before tools were released: %+v", outcome)
	default:
	}

	close(wait)
	outcome := <-resultCh
	if outcome.err != nil {
		t.Fatalf("unexpected error: %v", outcome.err)
	}
	if outcome.result.Output != "Done." {
		t.Fatalf("unexpected final output: %q", outcome.result.Output)
	}
	if len(outcome.result.Messages) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(outcome.result.Messages))
	}
	if got := outcome.result.Messages[2].Text(); got != "read result" {
		t.Fatalf("expected first tool result to preserve order, got %q", got)
	}
	if got := outcome.result.Messages[3].Text(); got != "glob result" {
		t.Fatalf("expected second tool result to preserve order, got %q", got)
	}
}

func TestRunnerDoomLoopReminderInjected(t *testing.T) {
	provider := &mockProvider{
		responses: []llm.CompletionResponse{
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Retrying reads."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc1", Name: "Read", Arguments: `{"path":"a.go"}`},
						{ID: "tc2", Name: "Read", Arguments: `{"path":"b.go"}`},
						{ID: "tc3", Name: "Read", Arguments: `{"path":"c.go"}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "Retrying reads again."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc4", Name: "Read", Arguments: `{"path":"a.go"}`},
						{ID: "tc5", Name: "Read", Arguments: `{"path":"b.go"}`},
						{ID: "tc6", Name: "Read", Arguments: `{"path":"c.go"}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message: llm.Message{
					Role:    llm.RoleAssistant,
					Content: []llm.ContentBlock{{Type: llm.ContentText, Text: "One more retry."}},
					ToolCalls: []llm.ToolCall{
						{ID: "tc7", Name: "Read", Arguments: `{"path":"a.go"}`},
						{ID: "tc8", Name: "Read", Arguments: `{"path":"b.go"}`},
						{ID: "tc9", Name: "Read", Arguments: `{"path":"c.go"}`},
					},
				},
				FinishReason: llm.FinishToolCalls,
			},
			{
				Message:      llm.TextMessage(llm.RoleAssistant, "Breaking out of the loop."),
				FinishReason: llm.FinishStop,
			},
		},
	}

	mgr := tools.NewManager()
	mgr.Register(&mockTool{name: "Read", result: "ok"})

	reminders := reminder.NewManager()
	reminders.Register(reminder.Reminder{
		ID:      "doom-loop",
		Content: "You appear to be repeating the same tool-call pattern without making progress.",
		Schedule: reminder.Schedule{
			Kind:      reminder.ScheduleCondition,
			MaxFires:  1,
			Condition: reminder.ParseConditionString("repeated_tool_chain:Read>Read>Read|3"),
		},
		Active: true,
	})

	cfg := &Config{
		Provider:  provider,
		Tools:     mgr,
		Reminders: reminders,
		MaxTurns:  6,
	}

	runner := NewRunner(cfg, Callbacks{})
	result, err := runner.Run(context.Background(), []llm.Message{
		llm.TextMessage(llm.RoleUser, "Keep trying reads"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Breaking out of the loop." {
		t.Fatalf("unexpected final output: %q", result.Output)
	}
	if provider.callIdx != 4 {
		t.Fatalf("expected 4 provider calls, got %d", provider.callIdx)
	}
	if len(provider.requests) != 4 {
		t.Fatalf("expected 4 captured requests, got %d", len(provider.requests))
	}
	thirdRequest := provider.requests[3]
	if len(thirdRequest.Messages) == 0 {
		t.Fatal("expected third request to contain messages")
	}
	lastPrompt := thirdRequest.Messages[len(thirdRequest.Messages)-1].Text()
	if want := "You appear to be repeating the same tool-call pattern without making progress."; !contains(lastPrompt, want) {
		t.Fatalf("expected injected doom-loop reminder in final request, got %q", lastPrompt)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
