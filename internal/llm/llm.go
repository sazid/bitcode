package llm

import (
	"context"
	"strings"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentType string

const (
	ContentText     ContentType = "text"
	ContentThinking ContentType = "thinking"
	ContentImage    ContentType = "image"
	ContentAudio    ContentType = "audio"
	ContentVideo    ContentType = "video"
	ContentDocument ContentType = "document"
)

// MediaSource represents the source data for multi-modal content blocks.
type MediaSource struct {
	Type      string `json:"type"`                 // "base64" or "url"
	MediaType string `json:"media_type,omitempty"` // MIME type, e.g. "image/png", "audio/wav"
	Data      string `json:"data,omitempty"`       // base64-encoded data
	URL       string `json:"url,omitempty"`        // URL source
}

type ContentBlock struct {
	Type     ContentType  `json:"type"`
	Text     string       `json:"text,omitempty"`
	Thinking string       `json:"thinking,omitempty"` // for thinking blocks
	Source   *MediaSource `json:"source,omitempty"`   // for image/audio/video/document blocks
}

type Message struct {
	Role       Role           `json:"role"`
	Content    []ContentBlock `json:"content"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`   // assistant messages only
	ToolCallID string         `json:"tool_call_id,omitempty"` // tool result messages only
}

// Text returns the concatenated text of all text content blocks.
func (m Message) Text() string {
	var sb strings.Builder
	for _, b := range m.Content {
		if b.Type == ContentText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// TextMessage creates a Message with a single text content block.
func TextMessage(role Role, text string) Message {
	return Message{
		Role:    role,
		Content: []ContentBlock{{Type: ContentText, Text: text}},
	}
}

// ImageMessage creates a Message with an image content block and optional text caption.
func ImageMessage(role Role, source MediaSource, caption string) Message {
	blocks := []ContentBlock{{Type: ContentImage, Source: &source}}
	if caption != "" {
		blocks = append(blocks, ContentBlock{Type: ContentText, Text: caption})
	}
	return Message{Role: role, Content: blocks}
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishToolCalls FinishReason = "tool_calls"
)

type CompletionParams struct {
	Model           string
	Messages        []Message
	Tools           []ToolDef
	ReasoningEffort string
	SystemPrompt    string // top-level system prompt (used by Anthropic; OpenAI providers ignore)
	MaxTokens       int    // required by Anthropic; optional for OpenAI
}

// Usage tracks token consumption for a single LLM call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CacheRead    int `json:"cache_read,omitempty"`
	CacheCreate  int `json:"cache_create,omitempty"`
}

type CompletionResponse struct {
	Message      Message
	FinishReason FinishReason
	Usage        Usage
}

// DeltaType identifies the kind of content in a StreamDelta.
type DeltaType string

const (
	DeltaText     DeltaType = "text"
	DeltaThinking DeltaType = "thinking"
	DeltaToolArgs DeltaType = "tool_call_args"
	DeltaToolName DeltaType = "tool_call_name"
)

// StreamDelta represents an incremental piece of content during streaming.
type StreamDelta struct {
	Type         DeltaType
	Text         string
	ToolCallID   string // which tool call this delta belongs to
	ToolCallName string
}

// Provider is the LLM abstraction. If onDelta is non-nil, the provider
// streams tokens via the callback. If nil, it returns the full response.
type Provider interface {
	Complete(ctx context.Context, params CompletionParams, onDelta func(StreamDelta)) (*CompletionResponse, error)
}

// StatefulCompletionParams extends CompletionParams with server-side state tracking.
type StatefulCompletionParams struct {
	CompletionParams
	PreviousResponseID string
	// PreviousMessageCount is the number of messages (excluding system) that were
	// already sent in the previous request. When set with PreviousResponseID,
	// the provider sends only messages[PreviousMessageCount:] as incremental input.
	PreviousMessageCount int
}

// StatefulCompletionResponse extends CompletionResponse with a response ID for chaining.
type StatefulCompletionResponse struct {
	CompletionResponse
	ResponseID string
}

// StatefulProvider maintains server-side conversation state (e.g. OpenAI Responses API).
type StatefulProvider interface {
	Provider
	CompleteStateful(ctx context.Context, params StatefulCompletionParams, onDelta func(StreamDelta)) (*StatefulCompletionResponse, error)
}

// SessionProvider maintains persistent connections (e.g. WebSocket).
type SessionProvider interface {
	StatefulProvider
	Connect(ctx context.Context) error
	Close() error
	IsConnected() bool
}
