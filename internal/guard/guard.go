package guard

import (
	"context"
	"encoding/json"

	"github.com/sazid/bitcode/internal"
)

// GuardEvaluator is the interface for evaluating tool calls against guard rules.
type GuardEvaluator interface {
	Evaluate(ctx context.Context, toolName, input string, eventsCh chan<- internal.Event) (*Decision, error)
}

// Verdict represents the outcome of a guard rule evaluation.
type Verdict string

const (
	VerdictAllow Verdict = "allow" // safe, proceed
	VerdictDeny  Verdict = "deny"  // blocked, return error to LLM
	VerdictAsk   Verdict = "ask"   // ask user for approval
	VerdictLLM   Verdict = "llm"   // escalate to LLM guard
)

// Decision contains a verdict and a human-readable reason.
type Decision struct {
	Verdict  Verdict
	Reason   string
	Command  string // short preview of the tool input (populated by manager for the UI)
	Feedback string // user instructions returned when they choose "tell what to do"
}

// EvalContext provides the context needed to evaluate a tool call.
type EvalContext struct {
	ToolName   string
	Input      json.RawMessage
	WorkingDir string
	EventsCh   chan<- internal.Event // optional; used to publish guard progress to the UI
}

// Rule evaluates a tool call and returns a Decision.
// Returning nil means the rule abstains (no opinion).
type Rule interface {
	Evaluate(ctx *EvalContext) *Decision
}

// PermissionResult is the outcome returned by a PermissionHandler.
type PermissionResult struct {
	Approved bool
	Cache    bool   // if true, cache the approval for the rest of the session (Always allow)
	Feedback string // non-empty when the user chose "tell what to do"
}

// PermissionHandler is called when a guard verdict is Ask.
// It blocks until the user responds.
type PermissionHandler func(toolName string, decision Decision) PermissionResult
