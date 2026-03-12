package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/tools"
)

type AppConfig struct {
	Client          *openai.Client
	Model           string
	ReasoningEffort string
	ToolManager     *tools.Manager
}

func main() {
	_ = godotenv.Load()

	var prompt string
	var reasoningEffort string
	flag.StringVar(&prompt, "p", "", "Prompt to send to LLM (omit for interactive mode)")
	flag.StringVar(&reasoningEffort, "reasoning", "medium", "Reasoning effort: none, minimal, low, medium, high, xhigh")
	flag.Parse()

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	baseUrl := os.Getenv("OPENROUTER_BASE_URL")
	model := os.Getenv("OPENROUTER_MODEL")
	if baseUrl == "" {
		baseUrl = "https://openrouter.ai/api/v1"
	}
	if model == "" {
		model = "openrouter/free"
	}

	isLocalhost := strings.HasPrefix(baseUrl, "http://localhost") || strings.HasPrefix(baseUrl, "http://127.0.0.1")
	if apiKey == "" && !isLocalhost {
		fmt.Fprintln(os.Stderr, "Error: Env variable OPENROUTER_API_KEY not found (not required when base URL points to localhost)")
		os.Exit(1)
	}

	toolManager := tools.NewManager()
	toolManager.Register(&tools.ReadTool{})
	toolManager.Register(&tools.WriteTool{})
	toolManager.Register(&tools.EditTool{})
	toolManager.Register(&tools.GlobTool{})
	toolManager.Register(&tools.BashTool{})

	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseUrl))

	config := &AppConfig{
		Client:          &client,
		Model:           model,
		ReasoningEffort: reasoningEffort,
		ToolManager:     toolManager,
	}

	if prompt != "" {
		runSingleShot(config, prompt)
	} else {
		runInteractive(config)
	}
}

// runSingleShot runs a single prompt through the agent loop and exits.
func runSingleShot(config *AppConfig, prompt string) {
	conversation := newConversationWithTools(config)
	conversation.AddMessage(openai.UserMessage(prompt))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	eventsCh := make(chan internal.Event, 16)
	go func() {
		for e := range eventsCh {
			renderEvent(os.Stderr, e)
		}
	}()

	runAgentLoop(ctx, config, conversation, eventsCh)
	close(eventsCh)
}

// runInteractive runs the interactive REPL mode.
func runInteractive(config *AppConfig) {
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))  // green
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))  // yellow
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))    // red

	printWelcomeBanner(config.Model)

	conversation := newConversationWithTools(config)

	for {
		result := readInput()

		if result.EOF {
			fmt.Fprintln(os.Stderr, dimStyle.Render("\nGoodbye!"))
			break
		}

		if result.Command != "" {
			switch result.Command {
			case "/exit", "/quit":
				fmt.Fprintln(os.Stderr, dimStyle.Render("\nGoodbye!"))
				return
			case "/new":
				conversation = newConversationWithTools(config)
				fmt.Fprintln(os.Stderr, successStyle.Render("\n  ✓ Started new conversation"))
				continue
			case "/help":
				printHelp()
				continue
			default:
				fmt.Fprintln(os.Stderr, errorStyle.Render(
					fmt.Sprintf("\n  Unknown command: %s", result.Command),
				))
				fmt.Fprintln(os.Stderr, dimStyle.Render("  Type /help for available commands"))
				continue
			}
		}

		if result.Text == "" {
			continue
		}

		conversation.AddMessage(openai.UserMessage(result.Text))

		// Create a cancellable context for this turn.
		// Ctrl+C interrupts the current response, not the whole program.
		ctx, cancel := context.WithCancel(context.Background())

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT)

		go func() {
			select {
			case <-sigCh:
				fmt.Fprintln(os.Stderr, warningStyle.Render("\n⏹ Interrupted"))
				cancel()
			case <-ctx.Done():
			}
		}()

		eventsCh := make(chan internal.Event, 16)
		go func() {
			for e := range eventsCh {
				renderEvent(os.Stderr, e)
			}
		}()

		runAgentLoop(ctx, config, conversation, eventsCh)
		close(eventsCh)

		signal.Stop(sigCh)
		cancel()
	}
}

// newConversationWithTools creates a new conversation pre-loaded with the system prompt and tool definitions.
func newConversationWithTools(config *AppConfig) *Conversation {
	conversation := NewConversation()
	conversation.AddMessage(openai.SystemMessage(buildSystemPrompt()))
	for _, td := range tools.ToOpenAITools(config.ToolManager.ToolDefinitions()) {
		conversation.AddTool(td)
	}
	return conversation
}

// runAgentLoop runs the agent loop: sending messages to the LLM, processing tool calls,
// and iterating until the model stops or the context is cancelled.
func runAgentLoop(ctx context.Context, config *AppConfig, conversation *Conversation, eventsCh chan<- internal.Event) {
	maxTurns := 50

	for turn := 0; turn < maxTurns; turn++ {
		if ctx.Err() != nil {
			return
		}

		params := openai.ChatCompletionNewParams{
			ReasoningEffort: openai.ReasoningEffort(config.ReasoningEffort),
			Model:           config.Model,
			Messages:        conversation.Messages,
			Tools:           conversation.Tools,
			N:               openai.Int(1),
		}

		resp, err := config.Client.Chat.Completions.New(ctx, params)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
			return
		}
		if len(resp.Choices) == 0 {
			fmt.Fprintln(os.Stderr, "\033[31mError: No choices in response\033[0m")
			return
		}

		choice := resp.Choices[0]
		param := choice.Message.ToParam()
		conversation.AddMessage(param)

		if choice.Message.Content != "" {
			renderMarkdown(os.Stderr, choice.Message.Content)
		}

		switch choice.FinishReason {
		case "tool_calls":
			for _, toolCall := range choice.Message.ToolCalls {
				if ctx.Err() != nil {
					return
				}

				toolName := toolCall.Function.Name
				toolInput := toolCall.Function.Arguments

				result, err := config.ToolManager.ExecuteTool(toolName, toolInput, eventsCh)
				content := result.Content
				if err != nil {
					eventsCh <- internal.Event{
						Name:    toolName,
						Args:    []string{},
						Message: fmt.Sprintf("Error: %v", err),
						IsError: true,
					}
					content = fmt.Sprintf("Error: %v", err)
				}
				conversation.AddMessage(openai.ToolMessage(content, toolCall.ID))
			}
		case "stop":
			return
		default:
			return
		}
	}

	eventsCh <- internal.Event{
		Name:    "System",
		Args:    []string{},
		Message: fmt.Sprintf("Max turn (%d) limit reached.", maxTurns),
	}
}
