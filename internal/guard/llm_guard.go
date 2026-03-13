package guard

import (
	"context"
	"fmt"
	"strings"

	"github.com/sazid/bitcode/internal/llm"
)

// LLMGuard validates ambiguous tool calls using a fast LLM.
type LLMGuard struct {
	provider llm.Provider
	model    string
}

// NewLLMGuard creates an LLM-based guard validator.
func NewLLMGuard(provider llm.Provider, model string) *LLMGuard {
	return &LLMGuard{provider: provider, model: model}
}

func (g *LLMGuard) Validate(ctx context.Context, evalCtx *EvalContext) (*Decision, error) {
	systemPrompt := fmt.Sprintf(`You are a security evaluator for a CLI coding agent working in: %s

Evaluate this tool call:
Tool: %s
Input: %s

Respond with exactly one line:
ALLOW
DENY: <reason>
ASK: <reason>

Consider: working directory boundaries, system damage risk, data exfiltration, common dev operations.`,
		evalCtx.WorkingDir,
		evalCtx.ToolName,
		truncateBytes(evalCtx.Input, 500),
	)

	resp, err := g.provider.Complete(ctx, llm.CompletionParams{
		Model: g.model,
		Messages: []llm.Message{
			llm.TextMessage(llm.RoleUser, systemPrompt),
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("LLM guard completion failed: %w", err)
	}

	text := strings.TrimSpace(resp.Message.Text())
	return parseLLMResponse(text), nil
}

func parseLLMResponse(text string) *Decision {
	line := strings.TrimSpace(text)
	// Take only the first line
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}

	upper := strings.ToUpper(line)

	if upper == "ALLOW" {
		return &Decision{Verdict: VerdictAllow, Reason: "LLM guard approved"}
	}
	if strings.HasPrefix(upper, "DENY:") {
		reason := strings.TrimSpace(line[5:])
		return &Decision{Verdict: VerdictDeny, Reason: reason}
	}
	if strings.HasPrefix(upper, "ASK:") {
		reason := strings.TrimSpace(line[4:])
		return &Decision{Verdict: VerdictAsk, Reason: reason}
	}

	// Unparseable response — ask to be safe
	return &Decision{Verdict: VerdictAsk, Reason: fmt.Sprintf("LLM guard returned ambiguous response: %s", truncate(line, 80))}
}

func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
