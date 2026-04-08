package agent

import (
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/conversation"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/reminder"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/telemetry"
	"github.com/sazid/bitcode/internal/tools"
)

const DefaultMaxTurns = 200

type Result struct {
	Output   string        // final assistant text (last message)
	Messages []llm.Message // full conversation transcript
	Usage    llm.Usage     // aggregated token usage across all turns
}

type Config struct {
	// Identity
	Name         string
	SystemPrompt string

	// LLM
	Provider       llm.Provider
	ProviderConfig llm.ProviderConfig // for subagent inheritance of partial overrides
	Model          string
	Reasoning      string
	MaxTurns       int

	// Capabilities
	Tools     tools.ToolRegistry
	Guard     guard.GuardEvaluator
	Reminders reminder.ReminderEvaluator

	// Per-instance state
	TodoStore    tools.TodoStore
	CompactState *tools.CompactState

	// Communication
	InjectedMessages chan string

	// Agent delegation
	AgentRegistry *Registry
	AgentTool     *AgentTool // reference for context propagation

	// App-level (used by the leader agent's UI layer)
	SkillManager     skills.SkillProvider
	TaskTitle        string
	InstructionFiles []string

	// Optional
	ConvManager *conversation.Manager
	ConvID      string
	Observer    telemetry.Observer
	TurnCounter *telemetry.TurnCounter
}

type Callbacks struct {
	OnContent  func(content string)
	OnThinking func(thinking bool)
	OnEvent    func(event internal.Event)
	OnError    func(err error)
}
