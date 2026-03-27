package telemetry

import (
	"time"
)

// EventType identifies the kind of telemetry event.
type EventType string

const (
	EventLLMCall      EventType = "llm_call"
	EventToolCall     EventType = "tool_call"
	EventGuardEval    EventType = "guard_eval"
	EventSessionStart EventType = "session_start"
	EventSessionEnd   EventType = "session_end"
	EventTurn         EventType = "turn"
	EventError        EventType = "error"
)

// Event is the envelope written to JSONL storage.
type Event struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"ts"`
	SessionID string    `json:"session_id"`
	Turn      int       `json:"turn,omitempty"`

	// Payloads — only one is populated per event.
	LLM     *LLMEvent     `json:"llm,omitempty"`
	Tool    *ToolEvent    `json:"tool,omitempty"`
	Guard   *GuardEvent   `json:"guard,omitempty"`
	Session *SessionEvent `json:"session,omitempty"`
	Error   *ErrorEvent   `json:"error,omitempty"`
}

// LLMEvent records a single LLM API call.
type LLMEvent struct {
	Model        string        `json:"model"`
	Provider     string        `json:"provider"`
	Duration     time.Duration `json:"duration_ns"`
	InputTokens  int           `json:"input_tokens"`
	OutputTokens int           `json:"output_tokens"`
	CacheRead    int           `json:"cache_read,omitempty"`
	CacheCreate  int           `json:"cache_create,omitempty"`
	FinishReason string        `json:"finish_reason"`
	IsGuard      bool          `json:"is_guard,omitempty"`
	Error        string        `json:"error,omitempty"`
}

// ToolEvent records a single tool execution.
type ToolEvent struct {
	Name      string        `json:"name"`
	Duration  time.Duration `json:"duration_ns"`
	Success   bool          `json:"success"`
	InputLen  int           `json:"input_len"`
	OutputLen int           `json:"output_len"`
	Error     string        `json:"error,omitempty"`
}

// GuardEvent records a guard evaluation.
type GuardEvent struct {
	ToolName string        `json:"tool_name"`
	Verdict  string        `json:"verdict"`
	Duration time.Duration `json:"duration_ns"`
	Error    string        `json:"error,omitempty"`
}

// SessionEvent records session start/end.
type SessionEvent struct {
	Mode     string        `json:"mode"` // "interactive" or "single-shot"
	Duration time.Duration `json:"duration_ns,omitempty"`
}

// ErrorEvent records an error.
type ErrorEvent struct {
	Component string `json:"component"`
	Message   string `json:"message"`
	Context   string `json:"context,omitempty"`
}

// Observer is the interface for recording telemetry events.
type Observer interface {
	RecordLLM(turn int, ev LLMEvent)
	RecordTool(turn int, ev ToolEvent)
	RecordGuard(turn int, ev GuardEvent)
	RecordSessionStart(mode string)
	RecordSessionEnd(duration time.Duration)
	RecordError(turn int, component, message, context string)
	Stats() *SessionStats
	ResetSession(newID string)
	Close()
}

// SessionStats holds aggregated metrics for the current session.
type SessionStats struct {
	SessionID     string
	StartTime     time.Time
	Turns         int
	LLMCalls      int
	TotalLatency  time.Duration
	InputTokens   int
	OutputTokens  int
	CacheRead     int
	CacheCreate   int
	ToolCalls     map[string]int
	ToolErrors    int
	GuardEvals    int
	GuardVerdicts map[string]int
	Errors        int
}

// NoopObserver discards all events.
type NoopObserver struct{}

func (NoopObserver) RecordLLM(int, LLMEvent)                 {}
func (NoopObserver) RecordTool(int, ToolEvent)               {}
func (NoopObserver) RecordGuard(int, GuardEvent)             {}
func (NoopObserver) RecordSessionStart(string)               {}
func (NoopObserver) RecordSessionEnd(time.Duration)          {}
func (NoopObserver) RecordError(int, string, string, string) {}
func (NoopObserver) Stats() *SessionStats {
	return &SessionStats{ToolCalls: map[string]int{}, GuardVerdicts: map[string]int{}}
}
func (NoopObserver) ResetSession(string) {}
func (NoopObserver) Close()              {}
