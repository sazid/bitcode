package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/telemetry"
	"github.com/sazid/bitcode/internal/tools"
)

// AgentTool is the LLM-facing tool that spawns subagents.
type AgentTool struct {
	Registry     *Registry
	ParentConfig *Config

	mu  sync.Mutex
	ctx context.Context
}

// SetContext sets the context for subagent execution.
// Called by the runner before each tool execution batch.
func (t *AgentTool) SetContext(ctx context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ctx = ctx
}

func (t *AgentTool) getContext() context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ctx != nil {
		return t.ctx
	}
	return context.Background()
}

func (t *AgentTool) Name() string { return "Agent" }

func (t *AgentTool) Description() string {
	var sb strings.Builder
	sb.WriteString("Spawn a specialized subagent to handle a bounded task. Use this when the work is complex, cross-file, ambiguous, or easy to isolate from the main thread. The subagent runs autonomously with its own context and returns its final output. Explore and plan subagents return structured results so the parent can reuse findings and plans directly.\n\nAvailable agent types:\n")
	for _, def := range t.Registry.List() {
		fmt.Fprintf(&sb, "- %s: %s\n", def.Name, def.Description)
	}
	return sb.String()
}

func (t *AgentTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_type": map[string]any{
				"type":        "string",
				"description": "Which agent type to use (for example: explore for read-only investigation, plan for implementation design, general-purpose for isolated execution)",
			},
			"prompt": map[string]any{
				"type":        "string",
				"description": "The task for the subagent to perform",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "Optional additional context from the current conversation",
			},
		},
		"required": []string{"agent_type", "prompt"},
	}
}

type agentToolInput struct {
	AgentType string `json:"agent_type"`
	Prompt    string `json:"prompt"`
	Context   string `json:"context"`
}

func (t *AgentTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (tools.ToolResult, error) {
	var params agentToolInput
	if err := json.Unmarshal(input, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	if params.AgentType == "" {
		return tools.ToolResult{}, fmt.Errorf("agent_type is required")
	}
	if params.Prompt == "" {
		return tools.ToolResult{}, fmt.Errorf("prompt is required")
	}

	def, ok := t.Registry.Get(params.AgentType)
	if !ok {
		available := make([]string, 0)
		for _, d := range t.Registry.List() {
			available = append(available, d.Name)
		}
		return tools.ToolResult{}, fmt.Errorf("unknown agent type %q, available: %s", params.AgentType, strings.Join(available, ", "))
	}

	eventsCh <- internal.Event{
		Name:    fmt.Sprintf("[%s]", def.Name),
		Message: "Starting subagent",
	}

	// Build subagent config
	subConfig, err := t.buildSubagentConfig(def, eventsCh)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create subagent: %w", err)
	}

	// Build initial messages
	userPrompt := params.Prompt
	if params.Context != "" {
		userPrompt = params.Context + "\n\n" + params.Prompt
	}
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, def.Prompt),
		llm.TextMessage(llm.RoleUser, userPrompt),
	}

	// Run subagent with event forwarding to parent
	ctx := t.getContext()
	prefix := fmt.Sprintf("[%s] ", def.Name)
	cb := Callbacks{
		OnEvent: func(e internal.Event) {
			e.Name = prefix + e.Name
			eventsCh <- e
		},
	}
	runner := NewRunner(subConfig, cb)
	result, err := runner.Run(ctx, messages)
	if err != nil && err != context.Canceled {
		return tools.ToolResult{}, fmt.Errorf("subagent %q failed: %w", def.Name, err)
	}

	eventsCh <- internal.Event{
		Name:    fmt.Sprintf("[%s]", def.Name),
		Message: "Subagent finished",
	}

	output := ""
	if result != nil {
		output = result.Output
	}
	if output == "" {
		if err == context.Canceled {
			output = "(subagent was cancelled)"
		} else {
			output = "(subagent produced no output)"
		}
	}

	return tools.ToolResult{Content: normalizeSubagentOutput(params.AgentType, params.Prompt, output)}, nil
}

func normalizeSubagentOutput(agentType, task, output string) string {
	switch agentType {
	case "explore":
		return normalizeStructuredSubagentOutput("explore_result", task, output, []string{"Summary", "Findings", "Relevant Files", "Next Steps"})
	case "plan":
		return normalizeStructuredSubagentOutput("plan_result", task, output, []string{"Summary", "Steps", "Risks", "Verification"})
	default:
		return output
	}
}

func normalizeStructuredSubagentOutput(rootTag, task, output string, orderedSections []string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return output
	}
	if strings.HasPrefix(trimmed, "<"+rootTag+">") {
		return trimmed
	}

	sections, orderedSectionsByAppearance, remainder := extractMarkdownSections(trimmed)
	knownSections := make(map[string]bool, len(orderedSections))
	for _, section := range orderedSections {
		knownSections[strings.ToLower(section)] = true
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "<%s>\n", rootTag)
	if strings.TrimSpace(task) != "" {
		fmt.Fprintf(&sb, "<task>%s</task>\n", xmlEscape(strings.TrimSpace(task)))
	}

	wroteStructured := false
	for _, section := range orderedSections {
		content := strings.TrimSpace(sections[strings.ToLower(section)])
		if content == "" {
			continue
		}
		tag := strings.ToLower(strings.ReplaceAll(section, " ", "_"))
		fmt.Fprintf(&sb, "<%s>%s</%s>\n", tag, xmlEscape(content), tag)
		wroteStructured = true
	}

	var notes []string
	if extra := strings.TrimSpace(remainder); extra != "" {
		notes = append(notes, extra)
	}
	for _, section := range orderedSectionsByAppearance {
		if knownSections[section.key] {
			continue
		}
		if text := strings.TrimSpace(section.content); text != "" {
			notes = append(notes, fmt.Sprintf("## %s\n%s", section.title, text))
		}
	}
	if len(notes) > 0 && wroteStructured {
		fmt.Fprintf(&sb, "<notes>%s</notes>\n", xmlEscape(strings.Join(notes, "\n\n")))
	}

	if !wroteStructured {
		fmt.Fprintf(&sb, "<report>%s</report>\n", xmlEscape(trimmed))
	}
	fmt.Fprintf(&sb, "</%s>", rootTag)
	return sb.String()
}

type markdownSection struct {
	title   string
	key     string
	content string
}

func extractMarkdownSections(output string) (map[string]string, []markdownSection, string) {
	sections := make(map[string]string)
	var ordered []markdownSection
	var remainder strings.Builder
	var currentTitle string
	var currentKey string
	var currentContent strings.Builder
	flush := func() {
		text := strings.TrimSpace(currentContent.String())
		if text == "" {
			currentContent.Reset()
			return
		}
		if currentKey == "" {
			if remainder.Len() > 0 {
				remainder.WriteString("\n")
			}
			remainder.WriteString(text)
		} else {
			sections[currentKey] = text
			ordered = append(ordered, markdownSection{
				title:   currentTitle,
				key:     currentKey,
				content: text,
			})
		}
		currentContent.Reset()
	}

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			flush()
			currentTitle = strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			currentKey = strings.ToLower(currentTitle)
			continue
		}
		currentContent.WriteString(line)
		currentContent.WriteString("\n")
	}
	flush()
	return sections, ordered, strings.TrimSpace(remainder.String())
}

func xmlEscape(text string) string {
	text = strings.ReplaceAll(text, "&", "&amp;")
	text = strings.ReplaceAll(text, "<", "&lt;")
	text = strings.ReplaceAll(text, ">", "&gt;")
	return text
}

func (t *AgentTool) buildSubagentConfig(def Definition, parentEventsCh chan<- internal.Event) (*Config, error) {
	// Resolve provider
	provider := t.ParentConfig.Provider
	model := t.ParentConfig.Model

	if def.Model != "" {
		model = def.Model
	}

	// Create new provider if definition overrides provider settings
	if def.Provider != "" || def.BaseURL != "" || def.APIKey != "" {
		// Start from parent's config and apply overrides
		providerCfg := t.ParentConfig.ProviderConfig
		providerCfg.Model = model
		if def.Provider != "" {
			providerCfg.Backend = def.Provider
		}
		if def.BaseURL != "" {
			providerCfg.BaseURL = def.BaseURL
		}
		if def.APIKey != "" {
			providerCfg.APIKey = def.APIKey
		}
		newProvider, err := llm.NewProvider(providerCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider for agent %q: %w", def.Name, err)
		}
		provider = newProvider
	}

	// Filter tools — always exclude the Agent tool to prevent nesting
	var filteredTools tools.ToolRegistry
	if len(def.Tools) > 0 {
		// Use only the tools specified in the definition, minus "Agent"
		allowed := make([]string, 0, len(def.Tools))
		for _, name := range def.Tools {
			if name != "Agent" {
				allowed = append(allowed, name)
			}
		}
		filteredTools = NewFilteredRegistry(t.ParentConfig.Tools, allowed)
	} else {
		// All tools except Agent
		allDefs := t.ParentConfig.Tools.ToolDefinitions()
		allowed := make([]string, 0, len(allDefs))
		for _, d := range allDefs {
			if d.Name != "Agent" {
				allowed = append(allowed, d.Name)
			}
		}
		filteredTools = NewFilteredRegistry(t.ParentConfig.Tools, allowed)
	}

	// Create fresh per-instance state
	todoStore := tools.NewTodoStore()
	compactState := tools.NewCompactState()

	return &Config{
		Name:         def.Name,
		SystemPrompt: def.Prompt,
		Provider:     provider,
		Model:        model,
		MaxTurns:     def.MaxTurns,
		Tools:        filteredTools,
		Guard:        t.ParentConfig.Guard,
		TodoStore:    todoStore,
		CompactState: compactState,
		Observer:     t.ParentConfig.Observer,
		TurnCounter:  telemetry.NewTurnCounter(),
	}, nil
}
