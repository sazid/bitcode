package guard

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/tools"
)

//go:embed skills/simulate.md
var simulateMD []byte

//go:embed skills/bash.md
var bashMD []byte

//go:embed skills/python.md
var pythonMD []byte

//go:embed skills/go.md
var goMD []byte

//go:embed skills/js.md
var jsMD []byte

// guardSkillsFS is a minimal in-memory FS built from the embedded skill files.
// We use individual embed directives because Go's embed.FS requires directory
// paths to be known at compile time, and we want them addressable as a package.
var embeddedGuardSkills = map[string][]byte{
	"simulate.md": simulateMD,
	"bash.md":     bashMD,
	"python.md":   pythonMD,
	"go.md":       goMD,
	"js.md":       jsMD,
}

// NewGuardSkillManager creates a skills.Manager pre-loaded with the built-in
// guard skills embedded in the binary. It scans "guard-skills/" directories
// on disk (same precedence model as the main skill system) so users can add or
// override guard skills by dropping files into those directories.
func NewGuardSkillManager() *skills.Manager {
	// Build an in-memory FS from the embedded skill byte slices.
	memFS := newMemFS(embeddedGuardSkills)

	return skills.NewManager(skills.Config{
		SubDir:         "guard-skills",
		Embedded:       memFS,
		EmbeddedSource: "builtin",
	})
}

// GuardAgent is a multi-turn LLM agent that implements LLMValidator.
// It uses an expert security persona and can invoke guard skills via SkillTool.
type GuardAgent struct {
	provider llm.Provider
	model    string
	skillMgr *skills.Manager
	maxTurns int
}

// NewGuardAgent creates a GuardAgent. maxTurns controls how many LLM turns the
// agent may take before returning a safe fallback of VerdictAsk.
// If maxTurns is 0, it defaults to 5.
func NewGuardAgent(provider llm.Provider, model string, skillMgr *skills.Manager, maxTurns int) *GuardAgent {
	if maxTurns <= 0 {
		maxTurns = 5
	}
	return &GuardAgent{
		provider: provider,
		model:    model,
		skillMgr: skillMgr,
		maxTurns: maxTurns,
	}
}

// Validate implements LLMValidator. It runs the guard agent loop and parses
// an ALLOW/DENY:/ASK: verdict from the agent's final text response.
func (g *GuardAgent) Validate(ctx context.Context, evalCtx *EvalContext) (*Decision, error) {
	// Set up a minimal tools.Manager with only the SkillTool
	toolMgr := tools.NewManager()
	toolMgr.Register(&tools.SkillTool{SkillManager: g.skillMgr})

	toolDefs := guardToolDefs(toolMgr)

	// Detect language and collect auto-invoke skills for pre-injection
	lang := DetectLanguage(evalCtx)
	autoSkills := SkillsForLanguage(g.skillMgr, lang)

	// Build the initial user message
	userMsg := buildInitialMessage(evalCtx, autoSkills)

	// Build the system prompt (lists on-demand skills)
	systemPrompt := BuildGuardSystemPrompt(
		evalCtx.WorkingDir,
		evalCtx.ToolName,
		string(evalCtx.Input),
		g.skillMgr,
	)

	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, systemPrompt),
		llm.TextMessage(llm.RoleUser, userMsg),
	}

	// Local discard channel for SkillTool events — guard internals stay off the main UI
	skillEventsCh := make(chan internal.Event, 32)
	go func() {
		for range skillEventsCh {
		}
	}()
	defer close(skillEventsCh)

	sendProgress(evalCtx.EventsCh, evalCtx.ToolName, "evaluating...", false)

	// Agent loop
	for turn := 0; turn < g.maxTurns; turn++ {
		if ctx.Err() != nil {
			return &Decision{Verdict: VerdictAsk, Reason: "guard evaluation cancelled"}, nil
		}

		resp, err := g.provider.Complete(ctx, llm.CompletionParams{
			Model:    g.model,
			Messages: messages,
			Tools:    toolDefs,
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("guard agent completion failed: %w", err)
		}

		messages = append(messages, resp.Message)

		if text := strings.TrimSpace(resp.Message.Text()); text != "" {
			sendProgress(evalCtx.EventsCh, evalCtx.ToolName, briefLine(text), false)
		}

		switch resp.FinishReason {
		case llm.FinishToolCalls:
			for _, tc := range resp.Message.ToolCalls {
				result, execErr := toolMgr.ExecuteTool(tc.Name, tc.Arguments, skillEventsCh)
				content := result.Content
				if execErr != nil {
					content = fmt.Sprintf("Error: %v", execErr)
				}
				messages = append(messages, llm.Message{
					Role:       llm.RoleTool,
					Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: content}},
					ToolCallID: tc.ID,
				})
			}

		case llm.FinishStop:
			text := strings.TrimSpace(resp.Message.Text())
			return parseLLMResponse(text), nil

		default:
			return &Decision{Verdict: VerdictAsk, Reason: "guard agent ended unexpectedly"}, nil
		}
	}

	return &Decision{Verdict: VerdictAsk, Reason: fmt.Sprintf("guard agent max turns (%d) exceeded", g.maxTurns)}, nil
}

// sendProgress emits a guard progress event if ch is non-nil; non-blocking.
func sendProgress(ch chan<- internal.Event, toolName, msg string, isError bool) {
	if ch == nil {
		return
	}
	select {
	case ch <- internal.Event{
		Name:        "Guard",
		Args:        []string{toolName},
		Message:     msg,
		PreviewType: internal.PreviewGuard,
		IsError:     isError,
	}:
	default:
	}
}

// briefLine returns the first non-empty line of text, truncated to 100 chars.
func briefLine(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 100 {
				return line[:100] + "..."
			}
			return line
		}
	}
	return text
}

// buildInitialMessage constructs the first user message for the guard agent.
// Auto-invoke skill bodies are pre-injected so the agent sees them immediately.
func buildInitialMessage(evalCtx *EvalContext, autoSkills []skills.Skill) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Evaluate this tool call:\nTool: %s\nInput: %s",
		evalCtx.ToolName,
		truncateBytes(evalCtx.Input, 2000),
	))

	if len(autoSkills) > 0 {
		sb.WriteString("\n\n--- Auto-injected security context ---\n")
		for _, s := range autoSkills {
			sb.WriteString("\n")
			sb.WriteString(s.Prompt)
			sb.WriteString("\n")
		}
		sb.WriteString("--- End security context ---\n")
	}

	return sb.String()
}

// guardToolDefs converts the tools.Manager definitions into llm.ToolDef slice.
func guardToolDefs(m *tools.Manager) []llm.ToolDef {
	defs := m.ToolDefinitions()
	result := make([]llm.ToolDef, 0, len(defs))
	for _, d := range defs {
		result = append(result, llm.ToolDef{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return result
}
