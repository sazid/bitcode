package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// LLMValidator provides optional LLM-based validation for ambiguous cases.
type LLMValidator interface {
	Validate(ctx context.Context, evalCtx *EvalContext) (*Decision, error)
}

// Manager evaluates tool calls against a chain of rules.
type Manager struct {
	rules           []Rule
	llmValidator    LLMValidator
	permHandler     PermissionHandler
	sessionApproved map[string]bool
	mu              sync.RWMutex
}

// NewManager creates a Manager with no rules.
func NewManager() *Manager {
	return &Manager{
		sessionApproved: make(map[string]bool),
	}
}

// AddRule appends a rule to the evaluation chain.
func (m *Manager) AddRule(r Rule) {
	m.rules = append(m.rules, r)
}

// SetLLMValidator sets the optional LLM validator.
func (m *Manager) SetLLMValidator(v LLMValidator) {
	m.llmValidator = v
}

// SetPermissionHandler sets the handler called on VerdictAsk.
func (m *Manager) SetPermissionHandler(h PermissionHandler) {
	m.permHandler = h
}

// Evaluate runs the tool call through all rules and returns a final decision.
func (m *Manager) Evaluate(ctx context.Context, toolName, input string) (*Decision, error) {
	wd, _ := os.Getwd()

	evalCtx := &EvalContext{
		ToolName:   toolName,
		Input:      json.RawMessage(input),
		WorkingDir: wd,
	}

	// Run rules in order — first non-nil Decision wins
	var decision *Decision
	for _, rule := range m.rules {
		if d := rule.Evaluate(evalCtx); d != nil {
			decision = d
			break
		}
	}

	// No rule fired — allow by default
	if decision == nil {
		return &Decision{Verdict: VerdictAllow, Reason: "no rule matched"}, nil
	}

	// Handle verdict escalation
	switch decision.Verdict {
	case VerdictAllow:
		return decision, nil

	case VerdictDeny:
		return decision, nil

	case VerdictLLM:
		if m.llmValidator != nil {
			llmDecision, err := m.llmValidator.Validate(ctx, evalCtx)
			if err != nil {
				// LLM validation failed — fall back to Ask
				decision = &Decision{
					Verdict: VerdictAsk,
					Reason:  fmt.Sprintf("%s (LLM guard unavailable: %v)", decision.Reason, err),
				}
			} else {
				decision = llmDecision
			}
		} else {
			// No LLM validator — fall back to Ask
			decision.Verdict = VerdictAsk
		}
		// If LLM returned Allow or Deny, return immediately
		if decision.Verdict != VerdictAsk {
			return decision, nil
		}
		// Otherwise fall through to Ask handling
		fallthrough

	case VerdictAsk:
		return m.handleAsk(toolName, decision)
	}

	return decision, nil
}

// handleAsk checks the session cache and prompts the user if needed.
func (m *Manager) handleAsk(toolName string, decision *Decision) (*Decision, error) {
	cacheKey := fmt.Sprintf("%s:%s", toolName, decision.Reason)

	m.mu.RLock()
	approved := m.sessionApproved[cacheKey]
	m.mu.RUnlock()

	if approved {
		return &Decision{Verdict: VerdictAllow, Reason: "previously approved"}, nil
	}

	if m.permHandler == nil {
		// No permission handler — auto-deny
		return &Decision{Verdict: VerdictDeny, Reason: decision.Reason + " (auto-denied, non-interactive)"}, nil
	}

	if m.permHandler(toolName, *decision) {
		// User approved — cache for session
		m.mu.Lock()
		m.sessionApproved[cacheKey] = true
		m.mu.Unlock()
		return &Decision{Verdict: VerdictAllow, Reason: "user approved"}, nil
	}

	return &Decision{Verdict: VerdictDeny, Reason: decision.Reason + " (user denied)"}, nil
}
