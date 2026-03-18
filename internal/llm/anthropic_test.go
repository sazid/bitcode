package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicProvider_SyncComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected api key 'test-key', got %q", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicAPIVersion {
			t.Errorf("unexpected anthropic-version: %q", r.Header.Get("anthropic-version"))
		}

		var req anthropicRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "claude-sonnet-4-6" {
			t.Errorf("unexpected model: %q", req.Model)
		}
		if req.Stream {
			t.Error("expected stream=false for sync")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:   "msg_123",
			Role: "assistant",
			Content: []anthropicContent{
				{Type: "text", Text: "Hello from Claude!"},
			},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)
	resp, err := p.Complete(context.Background(), CompletionParams{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{TextMessage(RoleUser, "Hi")},
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Text() != "Hello from Claude!" {
		t.Errorf("unexpected text: %q", resp.Message.Text())
	}
	if resp.FinishReason != FinishStop {
		t.Errorf("unexpected finish reason: %q", resp.FinishReason)
	}
}

func TestAnthropicProvider_ToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify tools were sent
		if len(req.Tools) != 1 || req.Tools[0].Name != "read_file" {
			t.Errorf("unexpected tools: %+v", req.Tools)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			ID:   "msg_456",
			Role: "assistant",
			Content: []anthropicContent{
				{Type: "text", Text: "Let me read that file."},
				{Type: "tool_use", ID: "toolu_1", Name: "read_file", Input: json.RawMessage(`{"path":"main.go"}`)},
			},
			StopReason: "tool_use",
		})
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)
	resp, err := p.Complete(context.Background(), CompletionParams{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{TextMessage(RoleUser, "Read main.go")},
		Tools: []ToolDef{{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
		}},
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinishReason != FinishToolCalls {
		t.Errorf("expected FinishToolCalls, got %q", resp.FinishReason)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.Name != "read_file" || tc.ID != "toolu_1" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
}

func TestAnthropicProvider_StreamingText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req anthropicRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			t.Error("expected stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}` + "\n\n",
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}` + "\n\n",
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}` + "\n\n",
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}` + "\n\n",
		}

		for _, ev := range events {
			io.WriteString(w, ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)

	var deltas []string
	resp, err := p.Complete(context.Background(), CompletionParams{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{TextMessage(RoleUser, "Hi")},
	}, func(d StreamDelta) {
		if d.Type == DeltaText {
			deltas = append(deltas, d.Text)
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Text() != "Hello world" {
		t.Errorf("unexpected text: %q", resp.Message.Text())
	}
	if resp.FinishReason != FinishStop {
		t.Errorf("unexpected finish reason: %q", resp.FinishReason)
	}
	if joined := strings.Join(deltas, ""); joined != "Hello world" {
		t.Errorf("unexpected deltas: %q", joined)
	}
}

func TestAnthropicProvider_StreamingToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"bash"}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":"}}` + "\n\n",
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}` + "\n\n",
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}` + "\n\n",
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}` + "\n\n",
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}` + "\n\n",
		}

		for _, ev := range events {
			io.WriteString(w, ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)

	var toolArgDeltas []string
	resp, err := p.Complete(context.Background(), CompletionParams{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{TextMessage(RoleUser, "Run ls")},
		Tools:    []ToolDef{{Name: "bash", Description: "Run a command", Parameters: map[string]any{}}},
	}, func(d StreamDelta) {
		if d.Type == DeltaToolArgs {
			toolArgDeltas = append(toolArgDeltas, d.Text)
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinishReason != FinishToolCalls {
		t.Errorf("expected FinishToolCalls, got %q", resp.FinishReason)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
	tc := resp.Message.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Name != "bash" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if tc.Arguments != `{"cmd":"ls"}` {
		t.Errorf("unexpected arguments: %q", tc.Arguments)
	}
}

func TestAnthropicProvider_SystemPromptExtraction(t *testing.T) {
	var capturedReq anthropicRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedReq)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Role:       "assistant",
			Content:    []anthropicContent{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)
	_, err := p.Complete(context.Background(), CompletionParams{
		Model: "claude-sonnet-4-6",
		Messages: []Message{
			TextMessage(RoleSystem, "You are helpful."),
			TextMessage(RoleUser, "Hi"),
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// System should be extracted to top-level, not in messages
	var systemStr string
	json.Unmarshal(capturedReq.System, &systemStr)
	if systemStr != "You are helpful." {
		t.Errorf("expected system prompt, got %q", systemStr)
	}
	if len(capturedReq.Messages) != 1 {
		t.Errorf("expected 1 message (user only), got %d", len(capturedReq.Messages))
	}
	if capturedReq.Messages[0].Role != "user" {
		t.Errorf("expected user role, got %q", capturedReq.Messages[0].Role)
	}
}

func TestAnthropicProvider_ToolResultConversion(t *testing.T) {
	var capturedReq anthropicRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedReq)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Role:       "assistant",
			Content:    []anthropicContent{{Type: "text", Text: "File contents shown."}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)
	_, err := p.Complete(context.Background(), CompletionParams{
		Model: "claude-sonnet-4-6",
		Messages: []Message{
			TextMessage(RoleSystem, "You are helpful."),
			TextMessage(RoleUser, "Read main.go"),
			{
				Role:    RoleAssistant,
				Content: []ContentBlock{{Type: ContentText, Text: "Let me read that."}},
				ToolCalls: []ToolCall{{
					ID: "toolu_1", Name: "read_file", Arguments: `{"path":"main.go"}`,
				}},
			},
			{
				Role:       RoleTool,
				Content:    []ContentBlock{{Type: ContentText, Text: "package main\n\nfunc main() {}"}},
				ToolCallID: "toolu_1",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tool result should be converted to user message with tool_result content
	if len(capturedReq.Messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(capturedReq.Messages))
	}
	// The tool result message should be role=user with tool_result type
	toolMsg := capturedReq.Messages[2]
	if toolMsg.Role != "user" {
		t.Errorf("expected tool result as user role, got %q", toolMsg.Role)
	}
	if len(toolMsg.Content) == 0 || toolMsg.Content[0].Type != "tool_result" {
		t.Errorf("expected tool_result content, got %+v", toolMsg.Content)
	}
	if toolMsg.Content[0].ToolUseID != "toolu_1" {
		t.Errorf("expected tool_use_id=toolu_1, got %q", toolMsg.Content[0].ToolUseID)
	}
}

func TestAnthropicProvider_ConsecutiveRoleMerging(t *testing.T) {
	var capturedReq anthropicRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedReq)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anthropicResponse{
			Role:       "assistant",
			Content:    []anthropicContent{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	p := NewAnthropicProvider("test-key", server.URL)

	// Two consecutive tool results (both become user role) should be merged
	_, err := p.Complete(context.Background(), CompletionParams{
		Model: "claude-sonnet-4-6",
		Messages: []Message{
			TextMessage(RoleUser, "Do two things"),
			{
				Role: RoleAssistant,
				ToolCalls: []ToolCall{
					{ID: "t1", Name: "a", Arguments: "{}"},
					{ID: "t2", Name: "b", Arguments: "{}"},
				},
			},
			{Role: RoleTool, Content: []ContentBlock{{Type: ContentText, Text: "result1"}}, ToolCallID: "t1"},
			{Role: RoleTool, Content: []ContentBlock{{Type: ContentText, Text: "result2"}}, ToolCallID: "t2"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be: user, assistant, user (merged tool results)
	if len(capturedReq.Messages) != 3 {
		t.Fatalf("expected 3 messages after merging, got %d", len(capturedReq.Messages))
	}
	mergedUser := capturedReq.Messages[2]
	if mergedUser.Role != "user" {
		t.Errorf("expected user role for merged tool results, got %q", mergedUser.Role)
	}
	if len(mergedUser.Content) != 2 {
		t.Errorf("expected 2 content blocks in merged message, got %d", len(mergedUser.Content))
	}
}
