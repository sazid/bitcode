package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestWSProvider_ConnectAndComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("ws accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Read the request
		_, data, err := conn.Read(r.Context())
		if err != nil {
			t.Fatalf("ws read: %v", err)
		}

		var req wsRequest
		json.Unmarshal(data, &req)
		if req.Type != "response.create" {
			t.Errorf("expected response.create, got %q", req.Type)
		}

		// Send streaming events
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_ws1"}},
			{"type": "response.output_text.delta", "delta": "Hello"},
			{"type": "response.output_text.delta", "delta": " via WS"},
			{"type": "response.completed", "response": map[string]any{
				"id":     "resp_ws1",
				"status": "completed",
				"output": []map[string]any{
					{
						"type": "message",
						"content": []map[string]any{
							{"type": "output_text", "text": "Hello via WS"},
						},
					},
				},
			}},
		}

		for _, ev := range events {
			data, _ := json.Marshal(ev)
			conn.Write(r.Context(), websocket.MessageText, data)
		}
	}))
	defer server.Close()

	wsURL := strings.Replace(server.URL, "http://", "http://", 1)
	p := NewOpenAIResponsesWSProvider("test-key", wsURL)

	ctx := context.Background()
	if err := p.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer p.Close()

	if !p.IsConnected() {
		t.Fatal("expected connected")
	}

	var deltas []string
	resp, err := p.CompleteStateful(ctx, StatefulCompletionParams{
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
	if resp.ResponseID != "resp_ws1" {
		t.Errorf("unexpected response ID: %q", resp.ResponseID)
	}
	if resp.Message.Text() != "Hello via WS" {
		t.Errorf("unexpected text: %q", resp.Message.Text())
	}
	if joined := strings.Join(deltas, ""); joined != "Hello via WS" {
		t.Errorf("unexpected deltas: %q", joined)
	}
}

func TestWSProvider_FallbackToHTTP(t *testing.T) {
	// HTTP server for fallback (not WebSocket)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responsesResponse{
			ID:     "resp_http",
			Status: "completed",
			Output: []responsesOutputItem{
				{
					Type: "message",
					Content: []responsesOutputContent{
						{Type: "output_text", Text: "HTTP fallback"},
					},
				},
			},
		})
	}))
	defer server.Close()

	p := NewOpenAIResponsesWSProvider("test-key", server.URL)

	// Don't connect WebSocket — should fall back to HTTP
	resp, err := p.Complete(context.Background(), CompletionParams{
		Model:    "gpt-5",
		Messages: []Message{TextMessage(RoleUser, "Hi")},
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message.Text() != "HTTP fallback" {
		t.Errorf("unexpected text: %q", resp.Message.Text())
	}
}

func TestWSProvider_FunctionCallOverWS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("ws accept: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		// Read request
		conn.Read(r.Context())

		// Send function call events
		events := []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_fc"}},
			{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{
				"type": "function_call", "call_id": "call_ws1", "name": "bash",
			}},
			{"type": "response.function_call_arguments.delta", "output_index": 0, "delta": `{"cmd":"ls`},
			{"type": "response.function_call_arguments.delta", "output_index": 0, "delta": `"}`},
			{"type": "response.completed", "response": map[string]any{
				"id":     "resp_fc",
				"status": "completed",
				"output": []map[string]any{
					{"type": "function_call", "call_id": "call_ws1", "name": "bash", "arguments": `{"cmd":"ls"}`},
				},
			}},
		}

		for _, ev := range events {
			data, _ := json.Marshal(ev)
			conn.Write(r.Context(), websocket.MessageText, data)
		}
	}))
	defer server.Close()

	p := NewOpenAIResponsesWSProvider("test-key", strings.Replace(server.URL, "http://", "http://", 1))

	ctx := context.Background()
	p.Connect(ctx)
	defer p.Close()

	resp, err := p.CompleteStateful(ctx, StatefulCompletionParams{
		CompletionParams: CompletionParams{
			Model:    "gpt-5",
			Messages: []Message{TextMessage(RoleUser, "Run ls")},
			Tools:    []ToolDef{{Name: "bash", Description: "Run command", Parameters: map[string]any{}}},
		},
	}, func(d StreamDelta) {})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.FinishReason != FinishToolCalls {
		t.Errorf("expected FinishToolCalls, got %q", resp.FinishReason)
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Message.ToolCalls))
	}
}
