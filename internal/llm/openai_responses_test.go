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

func TestResponsesProvider_SyncComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/responses") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %q", r.Header.Get("Authorization"))
		}

		var req responsesRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "gpt-5" {
			t.Errorf("unexpected model: %q", req.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responsesResponse{
			ID:     "resp_123",
			Status: "completed",
			Output: []responsesOutputItem{
				{
					Type: "message",
					Role: "assistant",
					Content: []responsesOutputContent{
						{Type: "output_text", Text: "Hello!"},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := NewOpenAIResponsesProvider("test-key", server.URL)
	resp, err := p.Complete(context.Background(), CompletionParams{
		Model:    "gpt-5",
		Messages: []Message{TextMessage(RoleUser, "Hi")},
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Text() != "Hello!" {
		t.Errorf("unexpected text: %q", resp.Message.Text())
	}
	if resp.FinishReason != FinishStop {
		t.Errorf("unexpected finish reason: %q", resp.FinishReason)
	}
}

func TestResponsesProvider_StatefulWithPreviousResponseID(t *testing.T) {
	var capturedReq responsesRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedReq)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responsesResponse{
			ID:     "resp_456",
			Status: "completed",
			Output: []responsesOutputItem{
				{
					Type: "message",
					Content: []responsesOutputContent{
						{Type: "output_text", Text: "Continued."},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := NewOpenAIResponsesProvider("test-key", server.URL)
	resp, err := p.CompleteStateful(context.Background(), StatefulCompletionParams{
		CompletionParams: CompletionParams{
			Model:    "gpt-5",
			Messages: []Message{TextMessage(RoleUser, "Continue")},
		},
		PreviousResponseID: "resp_123",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq.PreviousResponseID != "resp_123" {
		t.Errorf("expected previous_response_id=resp_123, got %q", capturedReq.PreviousResponseID)
	}
	if resp.ResponseID != "resp_456" {
		t.Errorf("expected response_id=resp_456, got %q", resp.ResponseID)
	}
}

func TestResponsesProvider_FunctionCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responsesResponse{
			ID:     "resp_789",
			Status: "completed",
			Output: []responsesOutputItem{
				{
					Type:      "function_call",
					CallID:    "call_1",
					Name:      "read_file",
					Arguments: `{"path":"main.go"}`,
				},
			},
		})
	}))
	defer server.Close()

	p := NewOpenAIResponsesProvider("test-key", server.URL)
	resp, err := p.Complete(context.Background(), CompletionParams{
		Model:    "gpt-5",
		Messages: []Message{TextMessage(RoleUser, "Read main.go")},
		Tools:    []ToolDef{{Name: "read_file", Description: "Read a file", Parameters: map[string]any{}}},
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
	if tc.Name != "read_file" || tc.ID != "call_1" {
		t.Errorf("unexpected tool call: %+v", tc)
	}
}

func TestResponsesProvider_StreamingText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"resp_s1"}}` + "\n\n",
			`event: response.output_text.delta` + "\n" + `data: {"type":"response.output_text.delta","delta":"Hello"}` + "\n\n",
			`event: response.output_text.delta` + "\n" + `data: {"type":"response.output_text.delta","delta":" world"}` + "\n\n",
			`event: response.output_text.done` + "\n" + `data: {"type":"response.output_text.done"}` + "\n\n",
			`event: response.completed` + "\n" + `data: {"type":"response.completed","response":{"id":"resp_s1","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"Hello world"}]}]}}` + "\n\n",
		}

		for _, ev := range events {
			io.WriteString(w, ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewOpenAIResponsesProvider("test-key", server.URL)

	var deltas []string
	resp, err := p.CompleteStateful(context.Background(), StatefulCompletionParams{
		CompletionParams: CompletionParams{
			Model:    "gpt-5",
			Messages: []Message{TextMessage(RoleUser, "Hi")},
		},
	}, func(d StreamDelta) {
		if d.Type == DeltaText {
			deltas = append(deltas, d.Text)
		}
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ResponseID != "resp_s1" {
		t.Errorf("unexpected response ID: %q", resp.ResponseID)
	}
	if resp.Message.Text() != "Hello world" {
		t.Errorf("unexpected text: %q", resp.Message.Text())
	}
	if joined := strings.Join(deltas, ""); joined != "Hello world" {
		t.Errorf("unexpected deltas: %q", joined)
	}
}

func TestResponsesProvider_StreamingFunctionCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		events := []string{
			`event: response.created` + "\n" + `data: {"type":"response.created","response":{"id":"resp_fc"}}` + "\n\n",
			`event: response.output_item.added` + "\n" + `data: {"type":"function_call","output_index":0,"call_id":"call_1","name":"bash"}` + "\n\n",
			`event: response.function_call_arguments.delta` + "\n" + `data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"cmd\":"}` + "\n\n",
			`event: response.function_call_arguments.delta` + "\n" + `data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"\"ls\"}"}` + "\n\n",
			`event: response.function_call_arguments.done` + "\n" + `data: {"type":"response.function_call_arguments.done","output_index":0}` + "\n\n",
			`event: response.completed` + "\n" + `data: {"type":"response.completed","response":{"id":"resp_fc","status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"bash","arguments":"{\"cmd\":\"ls\"}"}]}}` + "\n\n",
		}

		for _, ev := range events {
			io.WriteString(w, ev)
			flusher.Flush()
		}
	}))
	defer server.Close()

	p := NewOpenAIResponsesProvider("test-key", server.URL)

	var toolArgDeltas []string
	resp, err := p.CompleteStateful(context.Background(), StatefulCompletionParams{
		CompletionParams: CompletionParams{
			Model:    "gpt-5",
			Messages: []Message{TextMessage(RoleUser, "Run ls")},
			Tools:    []ToolDef{{Name: "bash", Description: "Run command", Parameters: map[string]any{}}},
		},
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
	if tc.Name != "bash" || tc.Arguments != `{"cmd":"ls"}` {
		t.Errorf("unexpected tool call: %+v", tc)
	}
}

func TestResponsesProvider_InputConversion(t *testing.T) {
	var capturedReq responsesRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedReq)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responsesResponse{
			ID:     "resp_ic",
			Status: "completed",
			Output: []responsesOutputItem{
				{Type: "message", Content: []responsesOutputContent{{Type: "output_text", Text: "ok"}}},
			},
		})
	}))
	defer server.Close()

	p := NewOpenAIResponsesProvider("test-key", server.URL)
	_, err := p.Complete(context.Background(), CompletionParams{
		Model: "gpt-5",
		Messages: []Message{
			TextMessage(RoleSystem, "You are helpful."),
			TextMessage(RoleUser, "Read main.go"),
			{
				Role:    RoleAssistant,
				Content: []ContentBlock{{Type: ContentText, Text: "Reading..."}},
				ToolCalls: []ToolCall{{
					ID: "call_1", Name: "read_file", Arguments: `{"path":"main.go"}`,
				}},
			},
			{
				Role:       RoleTool,
				Content:    []ContentBlock{{Type: ContentText, Text: "package main"}},
				ToolCallID: "call_1",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// System should go to instructions
	if capturedReq.Instructions != "You are helpful." {
		t.Errorf("expected instructions, got %q", capturedReq.Instructions)
	}

	// Parse the input items
	var items []responsesInputItem
	json.Unmarshal(capturedReq.Input, &items)

	// Should have: user message, assistant message, function_call, function_call_output
	if len(items) != 4 {
		t.Fatalf("expected 4 input items, got %d", len(items))
	}
	if items[0].Type != "message" || items[0].Role != "user" {
		t.Errorf("expected user message, got %+v", items[0])
	}
	if items[1].Type != "message" || items[1].Role != "assistant" {
		t.Errorf("expected assistant message, got %+v", items[1])
	}
	if items[2].Type != "function_call" || items[2].Name != "read_file" {
		t.Errorf("expected function_call, got %+v", items[2])
	}
	if items[3].Type != "function_call_output" || items[3].CallID != "call_1" {
		t.Errorf("expected function_call_output, got %+v", items[3])
	}
}
