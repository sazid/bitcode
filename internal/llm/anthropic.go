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

const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	anthropicAPIVersion     = "2023-06-01"
)

// AnthropicProvider implements Provider for the Anthropic Messages API.
type AnthropicProvider struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(apiKey, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	return &AnthropicProvider{
		httpClient: &http.Client{},
		apiKey:     apiKey,
		baseURL:    strings.TrimRight(baseURL, "/"),
	}
}

func (p *AnthropicProvider) Complete(ctx context.Context, params CompletionParams, onDelta func(StreamDelta)) (*CompletionResponse, error) {
	body := p.buildRequest(params, onDelta != nil)

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	if onDelta != nil {
		return p.handleStream(resp.Body, onDelta)
	}
	return p.handleSync(resp.Body)
}

// --- Request building ---

type anthropicRequest struct {
	Model        string                `json:"model"`
	MaxTokens    int                   `json:"max_tokens"`
	System       json.RawMessage       `json:"system,omitempty"`
	Messages     []anthropicMessage    `json:"messages"`
	Tools        []anthropicTool       `json:"tools,omitempty"`
	Stream       bool                  `json:"stream,omitempty"`
	Thinking     *anthropicThinking    `json:"thinking,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type      string           `json:"type"`
	Text      string           `json:"text,omitempty"`
	Thinking  string           `json:"thinking,omitempty"`
	Source    *anthropicSource `json:"source,omitempty"`
	ID        string           `json:"id,omitempty"`          // tool_use id
	Name      string           `json:"name,omitempty"`        // tool_use name
	Input     json.RawMessage  `json:"input,omitempty"`       // tool_use input
	ToolUseID string           `json:"tool_use_id,omitempty"` // tool_result
	Content   string           `json:"content,omitempty"`     // tool_result content (when string)
	IsError   bool             `json:"is_error,omitempty"`
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

func (p *AnthropicProvider) buildRequest(params CompletionParams, stream bool) anthropicRequest {
	maxTokens := params.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	req := anthropicRequest{
		Model:        params.Model,
		MaxTokens:    maxTokens,
		Stream:       stream,
		CacheControl: &anthropicCacheControl{Type: "ephemeral"},
	}

	// Extract system prompt
	systemText := params.SystemPrompt
	msgs := params.Messages
	if systemText == "" && len(msgs) > 0 && msgs[0].Role == RoleSystem {
		systemText = msgs[0].Text()
		msgs = msgs[1:]
	}
	if systemText != "" {
		raw, _ := json.Marshal(systemText)
		req.System = raw
	}

	// Convert messages, merging consecutive same-role messages
	req.Messages = convertToAnthropicMessages(msgs)

	// Convert tools
	for _, t := range params.Tools {
		req.Tools = append(req.Tools, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.Parameters,
		})
	}

	// Reasoning/thinking support
	if params.ReasoningEffort != "" {
		budget := mapReasoningBudget(params.ReasoningEffort)
		if budget > 0 {
			req.Thinking = &anthropicThinking{
				Type:         "enabled",
				BudgetTokens: budget,
			}
			// Ensure max_tokens accommodates thinking budget
			if req.MaxTokens <= budget {
				req.MaxTokens = budget + 4096
			}
		}
	}

	return req
}

func mapReasoningBudget(effort string) int {
	switch strings.ToLower(effort) {
	case "low":
		return 2048
	case "medium":
		return 8192
	case "high":
		return 32768
	case "xhigh":
		return 65536
	default:
		return 0
	}
}

func convertToAnthropicMessages(msgs []Message) []anthropicMessage {
	var result []anthropicMessage

	for _, m := range msgs {
		role := string(m.Role)
		content := convertContentBlocks(m)

		// Tool results in Anthropic must be user role
		if m.Role == RoleTool {
			role = "user"
			content = []anthropicContent{{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Text(),
			}}
		}

		// Assistant messages with tool calls
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				inputRaw := json.RawMessage(tc.Arguments)
				content = append(content, anthropicContent{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: inputRaw,
				})
			}
		}

		// Skip system messages (already extracted)
		if m.Role == RoleSystem {
			continue
		}

		// Merge consecutive same-role messages
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Content = append(result[len(result)-1].Content, content...)
		} else {
			result = append(result, anthropicMessage{
				Role:    role,
				Content: content,
			})
		}
	}

	return result
}

func convertContentBlocks(m Message) []anthropicContent {
	var blocks []anthropicContent
	for _, b := range m.Content {
		switch b.Type {
		case ContentText:
			if b.Text != "" {
				blocks = append(blocks, anthropicContent{Type: "text", Text: b.Text})
			}
		case ContentThinking:
			if b.Thinking != "" {
				blocks = append(blocks, anthropicContent{Type: "thinking", Thinking: b.Thinking})
			}
		case ContentImage:
			if b.Source != nil {
				src := &anthropicSource{
					Type:      b.Source.Type,
					MediaType: b.Source.MediaType,
				}
				if b.Source.Type == "base64" {
					src.Data = b.Source.Data
				} else {
					src.URL = b.Source.URL
				}
				blocks = append(blocks, anthropicContent{Type: "image", Source: src})
			}
		case ContentDocument:
			if b.Source != nil {
				src := &anthropicSource{
					Type:      b.Source.Type,
					MediaType: b.Source.MediaType,
				}
				if b.Source.Type == "base64" {
					src.Data = b.Source.Data
				} else if b.Source.Type == "url" {
					src.URL = b.Source.URL
				}
				blocks = append(blocks, anthropicContent{Type: "document", Source: src})
			}
		default:
			// Fallback: if there's text, send as text block
			if b.Text != "" {
				blocks = append(blocks, anthropicContent{Type: "text", Text: b.Text})
			}
		}
	}
	return blocks
}

// --- Sync response handling ---

type anthropicResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Content    []anthropicContent `json:"content"`
	StopReason string             `json:"stop_reason"`
}

func (p *AnthropicProvider) handleSync(body io.Reader) (*CompletionResponse, error) {
	var resp anthropicResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return anthropicResponseToCompletion(resp), nil
}

func anthropicResponseToCompletion(resp anthropicResponse) *CompletionResponse {
	msg := Message{Role: RoleAssistant}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			msg.Content = append(msg.Content, ContentBlock{Type: ContentText, Text: block.Text})
		case "thinking":
			msg.Content = append(msg.Content, ContentBlock{Type: ContentThinking, Thinking: block.Thinking})
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(inputJSON),
			})
		}
	}

	finishReason := FinishStop
	if resp.StopReason == "tool_use" {
		finishReason = FinishToolCalls
	}

	return &CompletionResponse{
		Message:      msg,
		FinishReason: finishReason,
	}
}

// --- Streaming response handling ---

// Anthropic SSE event payloads
type anthropicStreamEvent struct {
	Type         string             `json:"type"`
	Index        int                `json:"index"`
	ContentBlock *anthropicContent  `json:"content_block,omitempty"`
	Delta        *anthropicDelta    `json:"delta,omitempty"`
	Message      *anthropicResponse `json:"message,omitempty"`
}

type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

func (p *AnthropicProvider) handleStream(body io.Reader, onDelta func(StreamDelta)) (*CompletionResponse, error) {
	reader := sse.NewReader(body)

	msg := Message{Role: RoleAssistant}
	var stopReason string

	// Track active content blocks by index
	type blockState struct {
		blockType string
		id        string
		name      string
		textBuf   strings.Builder
		inputBuf  strings.Builder
	}
	blocks := make(map[int]*blockState)

	for {
		event, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read SSE event: %w", err)
		}

		var ev anthropicStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &ev); err != nil {
			continue // skip unparseable events
		}

		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock != nil {
				bs := &blockState{blockType: ev.ContentBlock.Type}
				if ev.ContentBlock.Type == "tool_use" {
					bs.id = ev.ContentBlock.ID
					bs.name = ev.ContentBlock.Name
				}
				blocks[ev.Index] = bs
			}

		case "content_block_delta":
			bs := blocks[ev.Index]
			if bs == nil || ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				bs.textBuf.WriteString(ev.Delta.Text)
				onDelta(StreamDelta{Type: DeltaText, Text: ev.Delta.Text})
			case "thinking_delta":
				bs.textBuf.WriteString(ev.Delta.Thinking)
				onDelta(StreamDelta{Type: DeltaThinking, Text: ev.Delta.Thinking})
			case "input_json_delta":
				bs.inputBuf.WriteString(ev.Delta.PartialJSON)
				onDelta(StreamDelta{
					Type:         DeltaToolArgs,
					Text:         ev.Delta.PartialJSON,
					ToolCallID:   bs.id,
					ToolCallName: bs.name,
				})
			}

		case "content_block_stop":
			bs := blocks[ev.Index]
			if bs == nil {
				continue
			}
			switch bs.blockType {
			case "text":
				msg.Content = append(msg.Content, ContentBlock{
					Type: ContentText,
					Text: bs.textBuf.String(),
				})
			case "thinking":
				msg.Content = append(msg.Content, ContentBlock{
					Type:     ContentThinking,
					Thinking: bs.textBuf.String(),
				})
			case "tool_use":
				msg.ToolCalls = append(msg.ToolCalls, ToolCall{
					ID:        bs.id,
					Name:      bs.name,
					Arguments: bs.inputBuf.String(),
				})
			}
			delete(blocks, ev.Index)

		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}

		case "message_stop":
			// End of message
		}
	}

	finishReason := FinishStop
	if stopReason == "tool_use" {
		finishReason = FinishToolCalls
	}

	return &CompletionResponse{
		Message:      msg,
		FinishReason: finishReason,
	}, nil
}
