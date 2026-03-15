package guard

import (
	"encoding/json"

	"github.com/sazid/bitcode/internal"
)

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
	Verdict Verdict
	Reason  string
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

// PermissionHandler is called when a guard verdict is Ask.
// It blocks until the user responds. Returns true if approved.
type PermissionHandler func(toolName string, decision Decision) bool
