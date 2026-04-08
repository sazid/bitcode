package main

import (
	"context"

	"github.com/sazid/bitcode/internal/agent"
	"github.com/sazid/bitcode/internal/llm"
)

const defaultMaxAgentTurns = agent.DefaultMaxTurns

// AgentConfig is an alias for agent.Config used throughout the app package.
type AgentConfig = agent.Config

// AgentCallbacks is an alias for agent.Callbacks used throughout the app package.
type AgentCallbacks = agent.Callbacks

// runAgentLoop is a thin wrapper around agent.Runner.Run for backward compatibility.
// It will be removed once all callers use Runner directly.
func runAgentLoop(ctx context.Context, cfg *AgentConfig, messages *[]llm.Message, toolDefs []llm.ToolDef, cb AgentCallbacks) *agent.Result {
	_ = toolDefs // toolDefs are now derived inside the runner

	runner := agent.NewRunner(cfg, cb)
	result, _ := runner.Run(ctx, *messages)
	if result != nil {
		*messages = result.Messages
	}
	return result
}
