package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/conversation"
	"github.com/sazid/bitcode/internal/guard"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/reminder"
	"github.com/sazid/bitcode/internal/tools"
)

// ContextSetter is implemented by tools that need the current context
// (e.g., AgentTool needs it to pass cancellation to subagents).
type ContextSetter interface {
	SetContext(ctx context.Context)
}

// Runner executes an agent loop. Both the leader agent and subagents
// use this same abstraction.
type Runner struct {
	config    *Config
	callbacks Callbacks
}

// NewRunner creates a new agent runner.
func NewRunner(cfg *Config, cb Callbacks) *Runner {
	return &Runner{config: cfg, callbacks: cb}
}

// Run executes the agent loop with the given initial messages.
// Returns when the agent finishes (stop), hits max turns, or ctx is cancelled.
func (r *Runner) Run(ctx context.Context, messages []llm.Message) (*Result, error) {
	cfg := r.config
	cb := r.callbacks

	// Propagate context to AgentTool for subagent cancellation
	if cfg.AgentTool != nil {
		cfg.AgentTool.SetContext(ctx)
	}

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
	var recentToolCallChains []string
	var responseID string    // for StatefulProvider (Responses API)
	var prevMessageCount int // messages already covered by previous_response_id
	var totalUsage llm.Usage

	maxTurns := cfg.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}

	toolDefs := toolDefsFromRegistry(cfg.Tools)

	for turn := 0; turn < maxTurns; turn++ {
		if cfg.TurnCounter != nil {
			cfg.TurnCounter.Set(turn)
		}
		if ctx.Err() != nil {
			return r.buildResult(messages, totalUsage), ctx.Err()
		}

		// Drain any user messages injected mid-flight
		drainInjectedMessages(cfg, &messages)

		// Apply pending compaction: replace history with system prompt + summary
		if cfg.CompactState != nil {
			if summary := cfg.CompactState.TakeSummary(); summary != "" {
				systemMsg := messages[0] // preserve the system prompt
				messages = []llm.Message{
					systemMsg,
					llm.TextMessage(llm.RoleUser, fmt.Sprintf("<context>\nThis is a summary of the conversation so far. The full history has been compacted to free up context space.\n\n%s\n</context>\n\nThe conversation was compacted. Continue assisting based on the summary above.", summary)),
				}
				responseID = "" // reset stateful chain after compaction
				prevMessageCount = 0
				eventsCh <- internal.Event{
					Name:    "Compact",
					Message: fmt.Sprintf("Compacted conversation from %d messages to %d", turn, len(messages)),
				}
			}
		}

		// Evaluate reminders and inject into a copy for the API
		messagesForAPI := messages
		if cfg.Reminders != nil {
			state := &reminder.ConversationState{
				Turn:                 turn,
				Messages:             messages,
				LastToolCalls:        lastToolNames,
				RecentToolCallChains: recentToolCallChains,
				ElapsedTime:          time.Since(startTime),
			}
			if active := cfg.Reminders.Evaluate(state); len(active) > 0 {
				messagesForAPI = reminder.InjectReminders(messages, active)
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

		maxRetries := cfg.MaxRetries
		if maxRetries <= 0 {
			maxRetries = DefaultMaxRetries
		}

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				if cb.OnError != nil {
					cb.OnError(fmt.Errorf("retrying (%d/%d)...", attempt, maxRetries))
				}
				backoff := time.Duration(1<<(attempt-1)) * time.Second
				if backoff > 16*time.Second {
					backoff = 16 * time.Second
				}
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					if cb.OnThinking != nil {
						cb.OnThinking(false)
					}
					return r.buildResult(messages, totalUsage), ctx.Err()
				}
			}

			resp = nil
			err = nil

			// Use StatefulProvider if available (threads response IDs for Responses API)
			if sp, ok := cfg.Provider.(llm.StatefulProvider); ok {
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

			if err == nil {
				break
			}

			if ctx.Err() != nil {
				if cb.OnThinking != nil {
					cb.OnThinking(false)
				}
				return r.buildResult(messages, totalUsage), ctx.Err()
			}

			if cfg.Observer != nil {
				cfg.Observer.RecordError(turn, "llm", err.Error(), "agent_loop")
			}
			if cb.OnError != nil {
				cb.OnError(err)
			}
		}

		if cb.OnThinking != nil {
			cb.OnThinking(false)
		}

		if err != nil {
			return r.buildResult(messages, totalUsage), err
		}

		// Aggregate usage
		totalUsage.InputTokens += resp.Usage.InputTokens
		totalUsage.OutputTokens += resp.Usage.OutputTokens
		totalUsage.CacheRead += resp.Usage.CacheRead
		totalUsage.CacheCreate += resp.Usage.CacheCreate

		// Store the response in the message history
		messages = append(messages, resp.Message)

		// Persist to conversation storage
		persistMessage(cfg.ConvManager, cfg.ConvID, resp.Message)

		if text := resp.Message.Text(); text != "" && cb.OnContent != nil {
			cb.OnContent(text)
		}

		switch resp.FinishReason {
		case llm.FinishToolCalls:
			lastToolNames = make([]string, 0, len(resp.Message.ToolCalls))
			for _, tc := range resp.Message.ToolCalls {
				lastToolNames = append(lastToolNames, tc.Name)
			}
			if len(lastToolNames) > 0 {
				recentToolCallChains = append(recentToolCallChains, strings.Join(lastToolNames, ">"))
				if len(recentToolCallChains) > 8 {
					recentToolCallChains = recentToolCallChains[len(recentToolCallChains)-8:]
				}
			}

			// Separate Agent calls from regular calls for parallel execution
			var agentCalls []llm.ToolCall
			var regularCalls []llm.ToolCall
			for _, tc := range resp.Message.ToolCalls {
				if tc.Name == "Agent" {
					agentCalls = append(agentCalls, tc)
				} else {
					regularCalls = append(regularCalls, tc)
				}
			}

			// Execute regular tools sequentially
			for _, tc := range regularCalls {
				if ctx.Err() != nil {
					return r.buildResult(messages, totalUsage), ctx.Err()
				}
				toolMsg := r.executeToolCall(ctx, tc, eventsCh)
				messages = append(messages, toolMsg)
				persistMessage(cfg.ConvManager, cfg.ConvID, toolMsg)
				drainInjectedMessages(cfg, &messages)
			}

			// Execute Agent tool calls concurrently
			if len(agentCalls) > 0 {
				agentResults := r.executeAgentCallsParallel(ctx, agentCalls, eventsCh)
				for _, toolMsg := range agentResults {
					messages = append(messages, toolMsg)
					persistMessage(cfg.ConvManager, cfg.ConvID, toolMsg)
				}
				drainInjectedMessages(cfg, &messages)
			}

		case llm.FinishStop:
			if cfg.TodoStore != nil && cfg.TodoStore.HasIncomplete() {
				messages = append(messages, llm.Message{
					Role: llm.RoleUser,
					Content: []llm.ContentBlock{{
						Type: llm.ContentText,
						Text: buildIncompleteTodosReminder(cfg.TodoStore.Get()),
					}},
				})
				continue
			}
			return r.buildResult(messages, totalUsage), nil
		default:
			err := fmt.Errorf("unexpected finish reason: %s", resp.FinishReason)
			if cb.OnError != nil {
				cb.OnError(err)
			}
			return r.buildResult(messages, totalUsage), err
		}
	}

	eventsCh <- internal.Event{
		Name:    "System",
		Message: fmt.Sprintf("Max turn (%d) limit reached.", maxTurns),
	}
	return r.buildResult(messages, totalUsage), nil
}

// executeToolCall runs a single tool call with guard checks, returning the result message.
func (r *Runner) executeToolCall(ctx context.Context, tc llm.ToolCall, eventsCh chan<- internal.Event) llm.Message {
	cfg := r.config

	// Guard check
	if cfg.Guard != nil {
		decision, guardErr := cfg.Guard.Evaluate(ctx, tc.Name, tc.Arguments, eventsCh)
		if guardErr != nil {
			eventsCh <- internal.Event{
				Name:        "Guard",
				Args:        []string{tc.Name},
				Message:     fmt.Sprintf("Error: %v", guardErr),
				PreviewType: internal.PreviewGuard,
				IsError:     true,
			}
			return llm.Message{
				Role:       llm.RoleTool,
				Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: fmt.Sprintf("Guard error: %v", guardErr)}},
				ToolCallID: tc.ID,
			}
		}
		if decision != nil && decision.Verdict == guard.VerdictDeny {
			if decision.Feedback != "" {
				eventsCh <- internal.Event{
					Name:        "Guard",
					Args:        []string{tc.Name},
					Message:     fmt.Sprintf("User redirected: %s", decision.Feedback),
					PreviewType: internal.PreviewGuard,
				}
				return llm.Message{
					Role:       llm.RoleTool,
					Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: fmt.Sprintf("User chose not to run this tool and provided instructions instead: %s", decision.Feedback)}},
					ToolCallID: tc.ID,
				}
			}
			eventsCh <- internal.Event{
				Name:        "Guard",
				Args:        []string{tc.Name},
				Message:     fmt.Sprintf("Blocked: %s", decision.Reason),
				PreviewType: internal.PreviewGuard,
				IsError:     true,
			}
			return llm.Message{
				Role:       llm.RoleTool,
				Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: fmt.Sprintf("Operation blocked by safety guard: %s", decision.Reason)}},
				ToolCallID: tc.ID,
			}
		}
	}

	result, err := cfg.Tools.ExecuteTool(tc.Name, tc.Arguments, eventsCh)
	content := result.Content
	if err != nil {
		eventsCh <- internal.Event{
			Name:    tc.Name,
			Message: fmt.Sprintf("Error: %v", err),
			IsError: true,
		}
		content = buildToolFailureMessage(tc, err)
	}
	return llm.Message{
		Role:       llm.RoleTool,
		Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: content}},
		ToolCallID: tc.ID,
	}
}

func buildIncompleteTodosReminder(todos []tools.TodoItem) string {
	if len(todos) == 0 {
		return "<system-reminder>You have incomplete todos. Use TodoRead to inspect them, continue working, and only stop after every todo is complete.</system-reminder>"
	}

	var pending []string
	for _, todo := range todos {
		if todo.Status == "completed" {
			continue
		}
		pending = append(pending, fmt.Sprintf("- [%s] %s", todo.Status, todo.Content))
	}
	if len(pending) == 0 {
		return "<system-reminder>You still have todo state to reconcile. Review it with TodoRead and only stop after every todo is complete.</system-reminder>"
	}

	return fmt.Sprintf("<system-reminder>You tried to stop with incomplete todos. Review the remaining work, continue the task, and only stop after every todo is complete. Remaining todos:\n%s\nIf needed, call TodoRead to inspect the full list before proceeding.</system-reminder>", strings.Join(pending, "\n"))
}

func buildToolFailureMessage(tc llm.ToolCall, err error) string {
	return fmt.Sprintf("Tool call failed for %s.\nArguments: %s\nError: %v\nReflect on why this failed, fix the tool call, and try again if the task still requires it.", tc.Name, tc.Arguments, err)
}

// executeAgentCallsParallel runs multiple Agent tool calls concurrently
// and returns the result messages in the original order.
func (r *Runner) executeAgentCallsParallel(ctx context.Context, calls []llm.ToolCall, eventsCh chan<- internal.Event) []llm.Message {
	results := make([]llm.Message, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))

	for i, tc := range calls {
		go func(idx int, tc llm.ToolCall) {
			defer wg.Done()
			results[idx] = r.executeToolCall(ctx, tc, eventsCh)
		}(i, tc)
	}

	wg.Wait()
	return results
}

func (r *Runner) buildResult(messages []llm.Message, usage llm.Usage) *Result {
	output := ""
	// Find the last assistant text message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llm.RoleAssistant {
			if text := messages[i].Text(); text != "" {
				output = text
				break
			}
		}
	}
	return &Result{
		Output:   output,
		Messages: messages,
		Usage:    usage,
	}
}

// drainInjectedMessages pulls any pending user messages from the injection
// channel and appends them to the conversation.
func drainInjectedMessages(cfg *Config, messages *[]llm.Message) {
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

// persistMessage appends a message to conversation storage if available.
func persistMessage(convManager *conversation.Manager, convID string, msg llm.Message) {
	if convManager != nil && convID != "" {
		if err := convManager.AppendMessage(convID, msg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to persist message: %v\n", err)
		}
	}
}

// toolDefsFromRegistry converts tool definitions from the registry format
// to the LLM format.
func toolDefsFromRegistry(registry tools.ToolRegistry) []llm.ToolDef {
	var defs []llm.ToolDef
	for _, d := range registry.ToolDefinitions() {
		defs = append(defs, llm.ToolDef{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return defs
}
