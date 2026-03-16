package main

import (
	"context"
	"fmt"
	"time"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/reminder"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/tools"
)

const defaultMaxAgentTurns = 200

type AgentConfig struct {
	Provider     llm.Provider
	Model        string
	Reasoning    string
	MaxTurns     int
	ToolManager  *tools.Manager
	SkillManager *skills.Manager
	ReminderMgr  *reminder.Manager
	GuardMgr     *guard.Manager
	TodoStore    tools.TodoStore
	TaskTitle    string // Current task title for notifications
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

	startTime := time.Now()
	var lastToolNames []string

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxAgentTurns
	}
	for turn := 0; turn < maxTurns; turn++ {
		if ctx.Err() != nil {
			return
		}

		// Evaluate reminders and inject into a copy for the API
		messagesForAPI := *messages
		if cfg.ReminderMgr != nil {
			state := &reminder.ConversationState{
				Turn:          turn,
				Messages:      *messages,
				LastToolCalls: lastToolNames,
				ElapsedTime:   time.Since(startTime),
			}
			if active := cfg.ReminderMgr.Evaluate(state); len(active) > 0 {
				messagesForAPI = reminder.InjectReminders(*messages, active)
			}
		}

		if cb.OnThinking != nil {
			cb.OnThinking(true)
		}

		resp, err := cfg.Provider.Complete(ctx, llm.CompletionParams{
			Model:           cfg.Model,
			Messages:        messagesForAPI,
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

		// Store the original response in the real message history
		*messages = append(*messages, resp.Message)

		if text := resp.Message.Text(); text != "" && cb.OnContent != nil {
			cb.OnContent(text)
		}

		switch resp.FinishReason {
		case llm.FinishToolCalls:
			lastToolNames = make([]string, 0, len(resp.Message.ToolCalls))
			for _, tc := range resp.Message.ToolCalls {
				if ctx.Err() != nil {
					return
				}
				lastToolNames = append(lastToolNames, tc.Name)

				// Guard check
				if cfg.GuardMgr != nil {
					decision, guardErr := cfg.GuardMgr.Evaluate(ctx, tc.Name, tc.Arguments, eventsCh)
					if guardErr != nil {
						eventsCh <- internal.Event{
							Name:        "Guard",
							Args:        []string{tc.Name},
							Message:     fmt.Sprintf("Error: %v", guardErr),
							PreviewType: internal.PreviewGuard,
							IsError:     true,
						}
						*messages = append(*messages, llm.Message{
							Role:       llm.RoleTool,
							Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: fmt.Sprintf("Guard error: %v", guardErr)}},
							ToolCallID: tc.ID,
						})
						continue
					}
					if decision != nil && decision.Verdict == guard.VerdictDeny {
						if decision.Feedback != "" {
							eventsCh <- internal.Event{
								Name:        "Guard",
								Args:        []string{tc.Name},
								Message:     fmt.Sprintf("User redirected: %s", decision.Feedback),
								PreviewType: internal.PreviewGuard,
							}
							*messages = append(*messages, llm.Message{
								Role:       llm.RoleTool,
								Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: fmt.Sprintf("User chose not to run this tool and provided instructions instead: %s", decision.Feedback)}},
								ToolCallID: tc.ID,
							})
						} else {
							eventsCh <- internal.Event{
								Name:        "Guard",
								Args:        []string{tc.Name},
								Message:     fmt.Sprintf("Blocked: %s", decision.Reason),
								PreviewType: internal.PreviewGuard,
								IsError:     true,
							}
							*messages = append(*messages, llm.Message{
								Role:       llm.RoleTool,
								Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: fmt.Sprintf("Operation blocked by safety guard: %s", decision.Reason)}},
								ToolCallID: tc.ID,
							})
						}
						continue
					}
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
			if cfg.TodoStore != nil && cfg.TodoStore.HasIncomplete() {
				*messages = append(*messages, llm.Message{
					Role: llm.RoleUser,
					Content: []llm.ContentBlock{{
						Type: llm.ContentText,
						Text: "<system-reminder>You have incomplete todos. You must complete all todos before stopping. Use TodoRead to check your current todos and continue working.</system-reminder>",
					}},
				})
				continue
			}
			return
		default:
			if cb.OnError != nil {
				cb.OnError(fmt.Errorf("unexpected finish reason: %s", resp.FinishReason))
			}
			return
		}
	}

	eventsCh <- internal.Event{
		Name:    "System",
		Message: fmt.Sprintf("Max turn (%d) limit reached.", maxTurns),
	}
}
