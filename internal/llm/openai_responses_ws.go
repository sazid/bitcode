package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	wsConnectTimeout = 30 * time.Second
	wsMaxMessageSize = 10 * 1024 * 1024 // 10MB
)

// OpenAIResponsesWSProvider implements SessionProvider for the OpenAI Responses API over WebSocket.
type OpenAIResponsesWSProvider struct {
	httpProvider *OpenAIResponsesProvider // fallback for non-WS operations
	apiKey       string
	baseURL      string

	mu   sync.Mutex
	conn *websocket.Conn
}

// NewOpenAIResponsesWSProvider creates a WebSocket-enabled Responses API provider.
func NewOpenAIResponsesWSProvider(apiKey, baseURL string) *OpenAIResponsesWSProvider {
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	return &OpenAIResponsesWSProvider{
		httpProvider: NewOpenAIResponsesProvider(apiKey, baseURL),
		apiKey:       apiKey,
		baseURL:      strings.TrimRight(baseURL, "/"),
	}
}

// Connect establishes a WebSocket connection to the Responses API.
func (p *OpenAIResponsesWSProvider) Connect(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		return nil // already connected
	}

	connectCtx, cancel := context.WithTimeout(ctx, wsConnectTimeout)
	defer cancel()

	wsURL := strings.Replace(p.baseURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL += "/v1/responses"

	conn, _, err := websocket.Dial(connectCtx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + p.apiKey},
		},
	})
	if err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}
	conn.SetReadLimit(wsMaxMessageSize)
	p.conn = conn
	return nil
}

// Close gracefully closes the WebSocket connection.
func (p *OpenAIResponsesWSProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn == nil {
		return nil
	}

	err := p.conn.Close(websocket.StatusNormalClosure, "closing")
	p.conn = nil
	return err
}

// IsConnected returns whether the WebSocket connection is active.
func (p *OpenAIResponsesWSProvider) IsConnected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.conn != nil
}

// Complete implements Provider by delegating to CompleteStateful.
func (p *OpenAIResponsesWSProvider) Complete(ctx context.Context, params CompletionParams, onDelta func(StreamDelta)) (*CompletionResponse, error) {
	resp, err := p.CompleteStateful(ctx, StatefulCompletionParams{
		CompletionParams: params,
	}, onDelta)
	if err != nil {
		return nil, err
	}
	return &resp.CompletionResponse, nil
}

// CompleteStateful dispatches to WebSocket or HTTP based on connection state.
func (p *OpenAIResponsesWSProvider) CompleteStateful(ctx context.Context, params StatefulCompletionParams, onDelta func(StreamDelta)) (*StatefulCompletionResponse, error) {
	p.mu.Lock()
	conn := p.conn
	p.mu.Unlock()

	if conn != nil {
		resp, err := p.completeOverWebSocket(ctx, conn, params, onDelta)
		if err != nil {
			// If WebSocket fails, try to reconnect once and retry
			p.mu.Lock()
			p.conn = nil
			p.mu.Unlock()

			if reconnErr := p.Connect(ctx); reconnErr == nil {
				p.mu.Lock()
				conn = p.conn
				p.mu.Unlock()
				if conn != nil {
					return p.completeOverWebSocket(ctx, conn, params, onDelta)
				}
			}
			// Fall back to HTTP
			return p.httpProvider.CompleteStateful(ctx, params, onDelta)
		}
		return resp, nil
	}

	// No WebSocket connection; use HTTP SSE
	return p.httpProvider.CompleteStateful(ctx, params, onDelta)
}

// --- WebSocket request/response handling ---

// wsRequest mirrors the response.create event format for the Responses API WebSocket.
// All fields are top-level siblings of "type", not nested inside a "response" object.
type wsRequest struct {
	Type               string          `json:"type"`
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input,omitempty"`
	Tools              []responsesTool `json:"tools,omitempty"`
	Instructions       string          `json:"instructions,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	MaxOutputTokens    int             `json:"max_output_tokens,omitempty"`
}

func (p *OpenAIResponsesWSProvider) completeOverWebSocket(
	ctx context.Context,
	conn *websocket.Conn,
	params StatefulCompletionParams,
	onDelta func(StreamDelta),
) (*StatefulCompletionResponse, error) {
	// Build the request using the same logic as HTTP provider
	httpReq := p.httpProvider.buildRequest(params, true)

	wsReq := wsRequest{
		Type:               "response.create",
		Model:              httpReq.Model,
		Input:              httpReq.Input,
		Tools:              httpReq.Tools,
		Instructions:       httpReq.Instructions,
		PreviousResponseID: httpReq.PreviousResponseID,
		MaxOutputTokens:    httpReq.MaxOutputTokens,
	}

	reqJSON, err := json.Marshal(wsReq)
	if err != nil {
		return nil, fmt.Errorf("marshal ws request: %w", err)
	}

	if err := conn.Write(ctx, websocket.MessageText, reqJSON); err != nil {
		return nil, fmt.Errorf("ws write: %w", err)
	}

	// Read streaming events from WebSocket messages
	msg := Message{Role: RoleAssistant}
	var responseID string
	hasFunctionCalls := false

	var textBuf strings.Builder
	type funcCallState struct {
		callID  string
		name    string
		argsBuf strings.Builder
	}
	funcCalls := make(map[int]*funcCallState)

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 || err == io.EOF {
				break
			}
			return nil, fmt.Errorf("ws read: %w", err)
		}

		var ev responsesStreamEvent
		if err := json.Unmarshal(data, &ev); err != nil {
			continue
		}

		// Also check the top-level type field
		eventType := ev.Type

		switch eventType {
		case "response.created":
			if ev.Response != nil {
				responseID = ev.Response.ID
			}

		case "response.output_item.added":
			// Check if this is a function_call item
			// The event data has the item info
			var itemEvent struct {
				Type        string `json:"type"`
				OutputIndex int    `json:"output_index"`
				Item        struct {
					Type   string `json:"type"`
					CallID string `json:"call_id"`
					Name   string `json:"name"`
				} `json:"item"`
			}
			json.Unmarshal(data, &itemEvent)
			if itemEvent.Item.Type == "function_call" {
				funcCalls[itemEvent.OutputIndex] = &funcCallState{
					callID: itemEvent.Item.CallID,
					name:   itemEvent.Item.Name,
				}
				if onDelta != nil && itemEvent.Item.Name != "" {
					onDelta(StreamDelta{
						Type:         DeltaToolName,
						ToolCallID:   itemEvent.Item.CallID,
						ToolCallName: itemEvent.Item.Name,
					})
				}
			}

		case "response.output_text.delta":
			textBuf.WriteString(ev.Delta)
			if onDelta != nil {
				onDelta(StreamDelta{Type: DeltaText, Text: ev.Delta})
			}

		case "response.function_call_arguments.delta":
			if fc := funcCalls[ev.OutputIndex]; fc != nil {
				fc.argsBuf.WriteString(ev.Delta)
				if onDelta != nil {
					onDelta(StreamDelta{
						Type:         DeltaToolArgs,
						Text:         ev.Delta,
						ToolCallID:   fc.callID,
						ToolCallName: fc.name,
					})
				}
			}

		case "response.completed":
			// Full response available
			if ev.Response != nil {
				result := responsesToCompletion(*ev.Response)
				result.ResponseID = responseID
				return result, nil
			}
			goto buildFromAccumulated

		case "error":
			return nil, fmt.Errorf("ws error event: %s", string(data))
		}
	}

buildFromAccumulated:
	if text := textBuf.String(); text != "" {
		msg.Content = append(msg.Content, ContentBlock{Type: ContentText, Text: text})
	}
	for _, fc := range funcCalls {
		hasFunctionCalls = true
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:        fc.callID,
			Name:      fc.name,
			Arguments: fc.argsBuf.String(),
		})
	}

	finishReason := FinishStop
	if hasFunctionCalls {
		finishReason = FinishToolCalls
	}

	return &StatefulCompletionResponse{
		CompletionResponse: CompletionResponse{
			Message:      msg,
			FinishReason: finishReason,
		},
		ResponseID: responseID,
	}, nil
}
