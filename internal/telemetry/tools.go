package telemetry

import (
	"time"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/tools"
)

// toolRegistryWrapper instruments a tools.ToolRegistry with telemetry.
type toolRegistryWrapper struct {
	inner    tools.ToolRegistry
	observer Observer
	turn     *TurnCounter
}

// WrapToolRegistry wraps a ToolRegistry with telemetry instrumentation.
func WrapToolRegistry(r tools.ToolRegistry, obs Observer, turn *TurnCounter) tools.ToolRegistry {
	return &toolRegistryWrapper{inner: r, observer: obs, turn: turn}
}

func (w *toolRegistryWrapper) ExecuteTool(toolName string, input string, eventsCh chan<- internal.Event) (tools.ToolResult, error) {
	start := time.Now()
	result, err := w.inner.ExecuteTool(toolName, input, eventsCh)
	duration := time.Since(start)

	ev := ToolEvent{
		Name:      toolName,
		Duration:  duration,
		Success:   err == nil,
		InputLen:  len(input),
		OutputLen: len(result.Content),
	}
	if err != nil {
		ev.Error = err.Error()
	}
	w.observer.RecordTool(w.turn.Get(), ev)
	return result, err
}

func (w *toolRegistryWrapper) ToolDefinitions() []tools.ToolDefinition {
	return w.inner.ToolDefinitions()
}
