package reminder

import (
	"time"

	"github.com/sazid/bitcode/internal/llm"
)

// ScheduleKind determines when a reminder fires.
type ScheduleKind string

const (
	ScheduleAlways    ScheduleKind = "always"    // Every turn
	ScheduleTurn      ScheduleKind = "turn"      // Every N turns
	ScheduleTimer     ScheduleKind = "timer"     // Every N duration
	ScheduleOneShot   ScheduleKind = "oneshot"   // Fire once then deactivate
	ScheduleCondition ScheduleKind = "condition" // Fire when condition returns true
)

// ConditionFunc evaluates whether a reminder should fire.
type ConditionFunc func(state *ConversationState) bool

// Schedule controls when a reminder fires.
type Schedule struct {
	Kind         ScheduleKind
	TurnInterval int           // for turn-based: fire every N turns
	Interval     time.Duration // for timer-based: fire every N duration
	MaxFires     int           // 0 = unlimited
	Condition    ConditionFunc // for condition-based
}

// Reminder represents a piece of context to inject into the conversation.
type Reminder struct {
	ID       string
	Content  string // text to wrap in <system-reminder> tags
	Schedule Schedule
	Source   string // "builtin", "plugin"
	Priority int    // higher = injected later (more LLM attention)
	Active   bool
}

// ConversationState provides read-only context for evaluating reminder conditions.
type ConversationState struct {
	Turn                 int
	Messages             []llm.Message
	LastToolCalls        []string
	RecentToolCallChains []string
	ElapsedTime          time.Duration
	AssistantText        string
	UserText             string
}
