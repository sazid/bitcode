package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/notify"
)

// AgentLifecycle manages the lifecycle of a single agent goroutine.
type AgentLifecycle struct {
	config  *AgentConfig
	themes  *ThemeRegistry
	p       *tea.Program
	running bool
	doneCh  chan struct{}
	injectCh chan string
}

func NewAgentLifecycle(config *AgentConfig, themes *ThemeRegistry, p *tea.Program) *AgentLifecycle {
	injectCh := make(chan string, 8)
	config.InjectedMessages = injectCh
	return &AgentLifecycle{
		config:   config,
		themes:   themes,
		p:        p,
		doneCh:   make(chan struct{}, 1),
		injectCh: injectCh,
	}
}

// Start launches the agent loop in a goroutine. Returns true if started.
func (a *AgentLifecycle) Start(ctx context.Context, messages *[]llm.Message, toolDefs []llm.ToolDef) bool {
	if a.running {
		return false
	}
	a.running = true

	cancel := context.CancelFunc(nil)
	ctx, cancel = context.WithCancel(ctx)
	a.p.Send(agentStartMsg{cancel: cancel})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				pt := a.themes.Active()
				a.p.Send(appendOutputMsg(fmt.Sprintf("%sAgent panic: %v%s", pt.ANSI(pt.Error), r, pt.ANSIReset())))
			}
			a.p.Send(agentDoneMsg{})
			a.doneCh <- struct{}{}
			title := "BitCode: " + notify.Truncate(a.config.TaskTitle, 40)
			notify.Send(title, "Done")
		}()
		runAgentLoop(ctx, a.config, messages, toolDefs, sessionCallbacks(a.p, a.config, a.themes))
	}()

	return true
}

// IsRunning returns whether the agent is currently running.
// Also drains the done channel to update state.
func (a *AgentLifecycle) IsRunning() bool {
	select {
	case <-a.doneCh:
		a.running = false
	default:
	}
	return a.running
}

// InjectMessage sends a user message to the running agent.
func (a *AgentLifecycle) InjectMessage(text string) {
	select {
	case a.injectCh <- text:
	default:
	}
}

// Reset prepares for a new conversation.
func (a *AgentLifecycle) Reset() {
	a.running = false
}
