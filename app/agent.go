package main

import (
	"context"
	"fmt"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/tools"
)

const maxAgentTurns = 50

type AgentConfig struct {
	Provider    llm.Provider
	Model       string
	Reasoning   string
	ToolManager *tools.Manager
}

type AgentCallbacks struct {
	OnContent  func(content string)
	OnThinking func(thinking bool)
	OnEvent    func(event internal.Event)
	OnError    func(err error)
}

func runAgentLoop(ctx context.Context, cfg *AgentConfig, messages *[]llm.Message, toolDefs []llm.ToolDef, cb AgentCallbacks) {
	eventsCh := make(chan internal.Event, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range eventsCh {
			if cb.OnEvent != nil {
				cb.OnEvent(e)
			}
		}
	}()
	defer func() { close(eventsCh); <-done }()

	for turn := 0; turn < maxAgentTurns; turn++ {
		if ctx.Err() != nil {
			return
		}

		if cb.OnThinking != nil {
			cb.OnThinking(true)
		}

		resp, err := cfg.Provider.Complete(ctx, llm.CompletionParams{
			Model:           cfg.Model,
			Messages:        *messages,
			Tools:           toolDefs,
			ReasoningEffort: cfg.Reasoning,
		}, nil)

		if cb.OnThinking != nil {
			cb.OnThinking(false)
		}

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if cb.OnError != nil {
				cb.OnError(err)
			}
			return
		}

		*messages = append(*messages, resp.Message)

		if text := resp.Message.Text(); text != "" && cb.OnContent != nil {
			cb.OnContent(text)
		}

		switch resp.FinishReason {
		case llm.FinishToolCalls:
			for _, tc := range resp.Message.ToolCalls {
				if ctx.Err() != nil {
					return
				}
				result, err := cfg.ToolManager.ExecuteTool(tc.Name, tc.Arguments, eventsCh)
				content := result.Content
				if err != nil {
					eventsCh <- internal.Event{
						Name:    tc.Name,
						Message: fmt.Sprintf("Error: %v", err),
						IsError: true,
					}
					content = fmt.Sprintf("Error: %v", err)
				}
				*messages = append(*messages, llm.Message{
					Role:       llm.RoleTool,
					Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: content}},
					ToolCallID: tc.ID,
				})
			}
		case llm.FinishStop:
			return
		default:
			return
		}
	}

	eventsCh <- internal.Event{
		Name:    "System",
		Message: fmt.Sprintf("Max turn (%d) limit reached.", maxAgentTurns),
	}
}
