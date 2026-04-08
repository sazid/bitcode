package agent

import (
	"fmt"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/tools"
)

// FilteredRegistry wraps a ToolRegistry to expose only named tools.
type FilteredRegistry struct {
	inner   tools.ToolRegistry
	allowed map[string]bool
}

// NewFilteredRegistry creates a registry that only exposes the named tools.
// If allowedTools is empty, all tools from inner are exposed.
func NewFilteredRegistry(inner tools.ToolRegistry, allowedTools []string) *FilteredRegistry {
	allowed := make(map[string]bool, len(allowedTools))
	for _, name := range allowedTools {
		allowed[name] = true
	}
	return &FilteredRegistry{inner: inner, allowed: allowed}
}

func (f *FilteredRegistry) ExecuteTool(toolName string, input string, eventsCh chan<- internal.Event) (tools.ToolResult, error) {
	if len(f.allowed) > 0 && !f.allowed[toolName] {
		return tools.ToolResult{}, fmt.Errorf("tool %q not available to this agent", toolName)
	}
	return f.inner.ExecuteTool(toolName, input, eventsCh)
}

func (f *FilteredRegistry) ToolDefinitions() []tools.ToolDefinition {
	all := f.inner.ToolDefinitions()
	if len(f.allowed) == 0 {
		return all
	}
	var filtered []tools.ToolDefinition
	for _, d := range all {
		if f.allowed[d.Name] {
			filtered = append(filtered, d)
		}
	}
	return filtered
}
