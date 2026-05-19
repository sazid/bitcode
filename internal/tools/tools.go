package tools

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/sazid/bitcode/internal"
)

// ToolRegistry is the interface for executing tools and retrieving definitions.
type ToolRegistry interface {
	ExecuteTool(toolName string, input string, eventsCh chan<- internal.Event) (ToolResult, error)
	ToolDefinitions() []ToolDefinition
}

// ToolInputNormalizer is implemented by registries that can validate and repair
// raw model-generated tool arguments before guard checks or execution.
type ToolInputNormalizer interface {
	NormalizeToolInput(toolName string, input string) (string, []InputRepair, error)
}

type Manager struct {
	tools map[string]Tool
}

type ToolResult struct {
	Content string
}

type Tool interface {
	Name() string
	Description() string
	ParametersSchema() map[string]any
	Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error)
}

func IsParallelReadOnlyTool(name string) bool {
	switch name {
	case "Read", "Glob", "LineCount", "FileSize", "TodoRead", "Skill", "WebSearch":
		return true
	default:
		return false
	}
}

func NewManager() *Manager {
	return &Manager{
		tools: make(map[string]Tool),
	}
}

func (m *Manager) Register(tool Tool) {
	m.tools[tool.Name()] = tool
}

func (m *Manager) Get(name string) (Tool, bool) {
	tool, ok := m.tools[name]
	return tool, ok
}

func (m *Manager) List() []Tool {
	result := make([]Tool, 0, len(m.tools))
	for _, tool := range m.tools {
		result = append(result, tool)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})

	return result
}

func (m *Manager) ExecuteTool(toolName string, input string, eventsCh chan<- internal.Event) (ToolResult, error) {
	tool, ok := m.Get(toolName)
	if !ok {
		return ToolResult{}, fmt.Errorf("unknown tool: %s", toolName)
	}

	normalizedInput, _, err := normalizeInputForTool(toolName, input, tool.ParametersSchema())
	if err != nil {
		return ToolResult{}, err
	}

	return tool.Execute(json.RawMessage(normalizedInput), eventsCh)
}

func (m *Manager) NormalizeToolInput(toolName string, input string) (string, []InputRepair, error) {
	tool, ok := m.Get(toolName)
	if !ok {
		return "", nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	return normalizeInputForTool(toolName, input, tool.ParametersSchema())
}

type ToolDefinition struct {
	Type        string
	Name        string
	Description string
	Parameters  map[string]any
}

func (m *Manager) ToolDefinitions() []ToolDefinition {
	result := make([]ToolDefinition, 0, len(m.tools))
	for _, tool := range m.List() {
		result = append(result, ToolDefinition{
			Type:        "function",
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.ParametersSchema(),
		})
	}
	return result
}
