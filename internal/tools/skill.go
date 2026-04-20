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
	return `Execute a named skill by loading its reusable prompt instructions.

When to use this:
- Use Skill when the user's request matches a skill listed in the system prompt.
- Invoke the skill before doing manual work when the skill is designed for that workflow.
- Use args when the skill expects additional user input.

Important:
- Check the available skills list first and do not invoke missing skills.
- Skills may be namespaced, such as "git:commit".
- If a skill has a trigger condition, invoke it automatically when the condition is met.

Parameters:
- skill (required): Skill name.
- args (optional): Extra input passed to the skill template.`
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
