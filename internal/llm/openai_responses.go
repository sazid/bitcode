package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sazid/bitcode/internal/llm/sse"
)

const defaultOpenAIBaseURL = "https://api.openai.com"

// OpenAIResponsesProvider implements StatefulProvider for the OpenAI Responses API.
type OpenAIResponsesProvider struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
}

// NewOpenAIResponsesProvider creates a new OpenAI Responses API provider.
func NewOpenAIResponsesProvider(apiKey, baseURL string) *OpenAIResponsesProvider {
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	return &OpenAIResponsesProvider{
		httpClient: &http.Client{},
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

// Complete implements Provider by delegating to CompleteStateful with no previous response.
func (p *OpenAIResponsesProvider) Complete(ctx context.Context, params CompletionParams, onDelta func(StreamDelta)) (*CompletionResponse, error) {
	resp, err := p.CompleteStateful(ctx, StatefulCompletionParams{
		CompletionParams: params,
	}, onDelta)
	if err != nil {
		return nil, err
	}
	return &resp.CompletionResponse, nil
}

// CompleteStateful implements StatefulProvider with previous_response_id support.
func (p *OpenAIResponsesProvider) CompleteStateful(ctx context.Context, params StatefulCompletionParams, onDelta func(StreamDelta)) (*StatefulCompletionResponse, error) {
	body := p.buildRequest(params, onDelta != nil)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/responses", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai responses API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if onDelta != nil {
		return p.handleStream(resp.Body, onDelta)
	}
	return p.handleSync(resp.Body)
}

// --- Request types ---

type responsesRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input"`
	Tools              []responsesTool `json:"tools,omitempty"`
	Stream             bool            `json:"stream,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	MaxOutputTokens    int             `json:"max_output_tokens,omitempty"`
	Instructions       string          `json:"instructions,omitempty"`
}

type responsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// Input item types for the Responses API
type responsesInputItem struct {
	Type    string                 `json:"type"`
	Role    string                 `json:"role,omitempty"`
	Content []responsesContentPart `json:"content,omitempty"`
	// For function_call items
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// For function_call_output items — pointer so nil is omitted but empty string is kept
	Output *string `json:"output,omitempty"`
}

type responsesContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

func (p *OpenAIResponsesProvider) buildRequest(params StatefulCompletionParams, stream bool) responsesRequest {
	req := responsesRequest{
		Model:  params.Model,
		Stream: stream,
	}

	if params.MaxTokens > 0 {
		req.MaxOutputTokens = params.MaxTokens
	}

	// System prompt goes into instructions
	systemText := params.SystemPrompt
	msgs := params.Messages
	if systemText == "" && len(msgs) > 0 && msgs[0].Role == RoleSystem {
		systemText = msgs[0].Text()
		msgs = msgs[1:]
	}
	if systemText != "" {
		req.Instructions = systemText
	}

	// Previous response ID for stateful continuation
	if params.PreviousResponseID != "" {
		req.PreviousResponseID = params.PreviousResponseID
	}

	// Convert messages to input items.
	// When continuing with previous_response_id, send only new messages
	// (the ones added since the last request) as incremental input.
	inputMsgs := msgs
	if params.PreviousResponseID != "" && params.PreviousMessageCount > 0 && params.PreviousMessageCount <= len(msgs) {
		inputMsgs = msgs[params.PreviousMessageCount:]
	}
	items := convertToResponsesInput(inputMsgs)
	inputJSON, _ := json.Marshal(items)
	req.Input = inputJSON

	// Convert tools
	for _, t := range params.Tools {
		req.Tools = append(req.Tools, responsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}

	return req
}

func convertToResponsesInput(msgs []Message) []responsesInputItem {
	var items []responsesInputItem

	for _, m := range msgs {
		switch m.Role {
		case RoleSystem:
			// System messages handled via instructions field
			continue

		case RoleUser:
			item := responsesInputItem{
				Type: "message",
				Role: "user",
			}
			for _, b := range m.Content {
				switch b.Type {
				case ContentText:
					item.Content = append(item.Content, responsesContentPart{
						Type: "input_text",
						Text: b.Text,
					})
				case ContentImage:
					if b.Source != nil {
						url := b.Source.URL
						if b.Source.Type == "base64" && b.Source.Data != "" {
							url = "data:" + b.Source.MediaType + ";base64," + b.Source.Data
						}
						item.Content = append(item.Content, responsesContentPart{
							Type:     "input_image",
							ImageURL: url,
						})
					}
				default:
					if b.Text != "" {
						item.Content = append(item.Content, responsesContentPart{
							Type: "input_text",
							Text: b.Text,
						})
					}
				}
			}
			items = append(items, item)

		case RoleAssistant:
			// Text content as output message
			if text := m.Text(); text != "" {
				items = append(items, responsesInputItem{
					Type: "message",
					Role: "assistant",
					Content: []responsesContentPart{{
						Type: "output_text",
						Text: text,
					}},
				})
			}
			// Tool calls as function_call items
			for _, tc := range m.ToolCalls {
				items = append(items, responsesInputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				})
			}

		case RoleTool:
			text := m.Text()
			items = append(items, responsesInputItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: &text,
			})
		}
	}

	return items
}

// --- Sync response handling ---

type responsesUsageDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type responsesUsage struct {
	InputTokens        int                    `json:"input_tokens"`
	OutputTokens       int                    `json:"output_tokens"`
	InputTokensDetails *responsesUsageDetails `json:"input_tokens_details,omitempty"`
}

type responsesResponse struct {
	ID     string                `json:"id"`
	Status string                `json:"status"`
	Output []responsesOutputItem `json:"output"`
	Usage  *responsesUsage       `json:"usage,omitempty"`
}

type responsesOutputItem struct {
	Type    string                   `json:"type"`
	ID      string                   `json:"id,omitempty"`
	Role    string                   `json:"role,omitempty"`
	Content []responsesOutputContent `json:"content,omitempty"`
	// For function_call output items
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type responsesOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func (p *OpenAIResponsesProvider) handleSync(body io.Reader) (*StatefulCompletionResponse, error) {
	var resp responsesResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return responsesToCompletion(resp), nil
}

func responsesToCompletion(resp responsesResponse) *StatefulCompletionResponse {
	msg := Message{Role: RoleAssistant}
	hasFunctionCalls := false

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" || c.Type == "text" {
					msg.Content = append(msg.Content, ContentBlock{Type: ContentText, Text: c.Text})
				}
			}
		case "function_call":
			hasFunctionCalls = true
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}

	finishReason := FinishStop
	if hasFunctionCalls {
		finishReason = FinishToolCalls
	}

	result := &StatefulCompletionResponse{
		CompletionResponse: CompletionResponse{
			Message:      msg,
			FinishReason: finishReason,
		},
		ResponseID: resp.ID,
	}
	if resp.Usage != nil {
		result.Usage = Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		}
		if resp.Usage.InputTokensDetails != nil {
			result.Usage.CacheRead = resp.Usage.InputTokensDetails.CachedTokens
		}
	}
	return result
}

// --- Streaming response handling ---

type responsesStreamEvent struct {
	Type       string `json:"type"`
	ResponseID string `json:"response_id,omitempty"`
	// For output_text.delta
	Delta string `json:"delta,omitempty"`
	// For response.completed
	Response *responsesResponse `json:"response,omitempty"`
	// For function_call items
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// For output item tracking
	ItemID      string `json:"item_id,omitempty"`
	OutputIndex int    `json:"output_index,omitempty"`
}

func (p *OpenAIResponsesProvider) handleStream(body io.Reader, onDelta func(StreamDelta)) (*StatefulCompletionResponse, error) {
	reader := sse.NewReader(body)

	msg := Message{Role: RoleAssistant}
	var responseID string
	hasFunctionCalls := false

	// Accumulators
	var textBuf strings.Builder
	type funcCallState struct {
		callID  string
		name    string
		argsBuf strings.Builder
	}
	funcCalls := make(map[int]*funcCallState)

	for {
		event, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read SSE event: %w", err)
		}

		// The event type comes from the SSE "event:" field
		eventType := event.Type

		var ev responsesStreamEvent
		if event.Data != "" && event.Data != "[DONE]" {
			json.Unmarshal([]byte(event.Data), &ev)
		}

		switch eventType {
		case "response.created":
			if ev.Response != nil {
				responseID = ev.Response.ID
			}

		case "response.output_item.added":
			if ev.Type == "function_call" {
				funcCalls[ev.OutputIndex] = &funcCallState{
					callID: ev.CallID,
					name:   ev.Name,
				}
				if ev.Name != "" {
					onDelta(StreamDelta{
						Type:         DeltaToolName,
						ToolCallID:   ev.CallID,
						ToolCallName: ev.Name,
					})
				}
			}

		case "response.output_text.delta":
			textBuf.WriteString(ev.Delta)
			onDelta(StreamDelta{Type: DeltaText, Text: ev.Delta})

		case "response.function_call_arguments.delta":
			if fc := funcCalls[ev.OutputIndex]; fc != nil {
				fc.argsBuf.WriteString(ev.Delta)
				onDelta(StreamDelta{
					Type:         DeltaToolArgs,
					Text:         ev.Delta,
					ToolCallID:   fc.callID,
					ToolCallName: fc.name,
				})
			}

		case "response.function_call_arguments.done":
			// Function call complete

		case "response.output_text.done":
			// Text output complete

		case "response.completed":
			// If we have a full response object, use it
			if ev.Response != nil {
				result := responsesToCompletion(*ev.Response)
				result.ResponseID = responseID
				return result, nil
			}
		}
	}

	// Build from accumulated state if response.completed wasn't received with full response
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
