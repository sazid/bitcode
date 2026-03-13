package tools

import (
	"encoding/json"
	"fmt"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/skills"
)

type SkillInput struct {
	Skill string `json:"skill"`
	Args  string `json:"args,omitempty"`
}

// SkillLookup is the subset of skill management that SkillTool needs.
type SkillLookup interface {
	Get(name string) (skills.Skill, bool)
}

type SkillTool struct {
	SkillManager SkillLookup
}

var _ Tool = (*SkillTool)(nil)

func (t *SkillTool) Name() string {
	return "Skill"
}

func (t *SkillTool) Description() string {
	return `Execute a skill (user-defined prompt template) by name.

Skills are markdown-based prompt templates that users define to encapsulate
reusable workflows. When a user types "/<skill-name>" (e.g., "/commit", "/review"),
they are referring to a skill. Use this tool to invoke it.

How to invoke:
- Use this tool with the skill name and optional arguments
- Examples:
  - skill: "commit" - invoke the commit skill
  - skill: "commit", args: "-m 'Fix bug'" - invoke with arguments
  - skill: "git:commit" - invoke a namespaced skill (from a subdirectory)

Important:
- Available skills are listed in the system prompt under "Available Skills"
- When a skill matches the user's request, invoke it BEFORE generating other responses
- Do not invoke a skill that does not exist - check the available skills list first
- If a skill has a trigger condition, invoke it automatically when the condition is met`
}

func (t *SkillTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "The skill name (e.g., \"commit\", \"review\", \"git:commit\")",
			},
			"args": map[string]any{
				"type":        "string",
				"description": "Optional arguments to pass to the skill",
			},
		},
		"required": []string{"skill"},
	}
}

func (t *SkillTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params SkillInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	skill, ok := t.SkillManager.Get(params.Skill)
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown skill: %s", params.Skill)
	}

	prompt := skill.FormatPrompt(params.Args)

	eventsCh <- internal.Event{
		Name:    "Skill",
		Args:    []string{skill.Name},
		Message: fmt.Sprintf("Invoked skill: %s", skill.Name),
	}

	return ToolResult{Content: prompt}, nil
}
