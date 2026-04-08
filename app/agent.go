package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/conversation"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/reminder"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/telemetry"
	"github.com/sazid/bitcode/internal/tools"
)

const defaultMaxAgentTurns = 200

type AgentConfig struct {
	Provider         llm.Provider
	Model            string
	Reasoning        string
	MaxTurns         int
	ToolManager      tools.ToolRegistry
	SkillManager     skills.SkillProvider
	ReminderMgr      reminder.ReminderEvaluator
	GuardMgr         guard.GuardEvaluator
	TodoStore        tools.TodoStore
	CompactState     *tools.CompactState
	TaskTitle        string      // Current task title for notifications
	InstructionFiles []string    // Discovered CLAUDE.md/AGENTS.md relative paths
	InjectedMessages chan string // optional; user messages injected mid-flight
	Observer         telemetry.Observer
	TurnCounter      *telemetry.TurnCounter
	ConvManager      *conversation.Manager // optional; conversation persistence
	ConvID           string                // current conversation ID
}

type AgentCallbacks struct {
	OnContent  func(content string)
	OnThinking func(thinking bool)
	OnEvent    func(event internal.Event)
	OnError    func(err error)
}

// drainInjectedMessages pulls any pending user messages from the injection
// channel and appends them to the conversation.
func drainInjectedMessages(cfg *AgentConfig, messages *[]llm.Message) {
	if cfg.InjectedMessages == nil {
		return
	}
	for {
		select {
		case msg := <-cfg.InjectedMessages:
			*messages = append(*messages, llm.TextMessage(llm.RoleUser, msg))
		default:
			return
		}
	}
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

	// If provider supports persistent connections (WebSocket), manage lifecycle
	if sp, ok := cfg.Provider.(llm.SessionProvider); ok {
		if err := sp.Connect(ctx); err == nil {
			defer sp.Close()
		}
	}

	startTime := time.Now()
	var lastToolNames []string
	var responseID string    // for StatefulProvider (Responses API)
	var prevMessageCount int // messages already covered by previous_response_id

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxAgentTurns
	}
	for turn := 0; turn < maxTurns; turn++ {
		if cfg.TurnCounter != nil {
			cfg.TurnCounter.Set(turn)
		}
		if ctx.Err() != nil {
			return
		}

		// Drain any user messages injected mid-flight
		drainInjectedMessages(cfg, messages)

		// Apply pending compaction: replace history with system prompt + summary
		if cfg.CompactState != nil {
			if summary := cfg.CompactState.TakeSummary(); summary != "" {
				systemMsg := (*messages)[0] // preserve the system prompt
				*messages = []llm.Message{
					systemMsg,
					llm.TextMessage(llm.RoleUser, fmt.Sprintf("<context>\nThis is a summary of the conversation so far. The full history has been compacted to free up context space.\n\n%s\n</context>\n\nThe conversation was compacted. Continue assisting based on the summary above.", summary)),
				}
				responseID = "" // reset stateful chain after compaction
				prevMessageCount = 0
				eventsCh <- internal.Event{
					Name:    "Compact",
					Message: fmt.Sprintf("Compacted conversation from %d messages to %d", turn, len(*messages)),
				}
			}
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

		// Build streaming callback
		var onDelta func(llm.StreamDelta)
		if cb.OnContent != nil {
			onDelta = func(d llm.StreamDelta) {
				switch d.Type {
				case llm.DeltaText:
					// Streaming text will be delivered via OnContent at the end
					// (keeping current behavior of full-message delivery)
				case llm.DeltaThinking:
					// Could be wired to UI in the future
				}
			}
		}

		params := llm.CompletionParams{
			Model:           cfg.Model,
			Messages:        messagesForAPI,
			Tools:           toolDefs,
			ReasoningEffort: cfg.Reasoning,
		}

		var resp *llm.CompletionResponse
		var err error

		// Use StatefulProvider if available (threads response IDs for Responses API)
		if sp, ok := cfg.Provider.(llm.StatefulProvider); ok {
			// Count non-system messages for incremental input tracking
			msgCount := len(messagesForAPI)
			if msgCount > 0 && messagesForAPI[0].Role == llm.RoleSystem {
				msgCount--
			}

			statefulResp, statefulErr := sp.CompleteStateful(ctx, llm.StatefulCompletionParams{
				CompletionParams:     params,
				PreviousResponseID:   responseID,
				PreviousMessageCount: prevMessageCount,
			}, onDelta)
			if statefulErr != nil {
				err = statefulErr
			} else {
				resp = &statefulResp.CompletionResponse
				responseID = statefulResp.ResponseID
				prevMessageCount = msgCount
			}
		} else {
			resp, err = cfg.Provider.Complete(ctx, params, onDelta)
		}

		if cb.OnThinking != nil {
			cb.OnThinking(false)
		}

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if cfg.Observer != nil {
				cfg.Observer.RecordError(turn, "llm", err.Error(), "agent_loop")
			}
			if cb.OnError != nil {
				cb.OnError(err)
			}
			return
		}

		// Store the original response in the real message history
		*messages = append(*messages, resp.Message)

		// Persist to conversation storage
		if cfg.ConvManager != nil && cfg.ConvID != "" {
			if err := cfg.ConvManager.AppendMessage(cfg.ConvID, resp.Message); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to persist message: %v\n", err)
			}
		}

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
				toolMsg := llm.Message{
					Role:       llm.RoleTool,
					Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: content}},
					ToolCallID: tc.ID,
				}
				*messages = append(*messages, toolMsg)

				// Persist tool result to conversation storage
				if cfg.ConvManager != nil && cfg.ConvID != "" {
					if err := cfg.ConvManager.AppendMessage(cfg.ConvID, toolMsg); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: failed to persist tool result: %v\n", err)
					}
				}

				// Drain injected messages after each tool execution
				drainInjectedMessages(cfg, messages)
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
