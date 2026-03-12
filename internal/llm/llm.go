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
)

type ContentBlock struct {
	Type ContentType
	Text string
}

type Message struct {
	Role       Role
	Content    []ContentBlock
	ToolCalls  []ToolCall // assistant messages only
	ToolCallID string     // tool result messages only
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

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
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
}

type CompletionResponse struct {
	Message      Message
	FinishReason FinishReason
}

// StreamDelta represents an incremental piece of content during streaming.
type StreamDelta struct {
	Text string
}

// Provider is the LLM abstraction. If onDelta is non-nil, the provider
// streams tokens via the callback. If nil, it returns the full response.
type Provider interface {
	Complete(ctx context.Context, params CompletionParams, onDelta func(StreamDelta)) (*CompletionResponse, error)
}
