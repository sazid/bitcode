package tools

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/sazid/bitcode/internal"
)

// CompactState holds a pending compaction summary, shared between the
// CompactTool (which sets the summary) and the agent loop (which applies it).
type CompactState struct {
	mu      sync.Mutex
	summary string
}

func NewCompactState() *CompactState {
	return &CompactState{}
}

// SetSummary stores a compaction summary to be applied by the agent loop.
func (s *CompactState) SetSummary(summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summary = summary
}

// TakeSummary returns and clears the pending summary.
// Returns "" if no compaction is pending.
func (s *CompactState) TakeSummary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	summary := s.summary
	s.summary = ""
	return summary
}

type compactInput struct {
	Summary string `json:"summary"`
}

// CompactTool lets the agent compact the conversation history by providing
// a summary that replaces older messages, freeing up context space.
type CompactTool struct {
	State *CompactState
}

var _ Tool = (*CompactTool)(nil)

func (t *CompactTool) Name() string { return "Compact" }

func (t *CompactTool) Description() string {
	return `Compact the conversation history by replacing it with a summary.

Use this when the conversation is getting long and you need to free up context space.
Provide a comprehensive summary that captures everything needed to continue working.

The compaction is applied at the start of the next turn — the full message history
will be replaced with the system prompt plus your summary.

Your summary should include:
- The user's original request and goals
- What work has been completed (files read, created, modified)
- Key decisions, trade-offs, or constraints discovered
- Current state and what remains to be done
- Any errors encountered and how they were resolved`
}

func (t *CompactTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "Comprehensive summary of the conversation so far",
			},
		},
		"required": []string{"summary"},
	}
}

func (t *CompactTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params compactInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	if params.Summary == "" {
		return ToolResult{}, fmt.Errorf("summary cannot be empty")
	}

	t.State.SetSummary(params.Summary)

	eventsCh <- internal.Event{
		Name:    "Compact",
		Message: "Conversation history will be compacted at the start of the next turn",
	}

	return ToolResult{
		Content: "Compaction scheduled. At the start of the next turn, the conversation history will be replaced with the system prompt and your summary. Continue working normally.",
	}, nil
}
