package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/tools"
)

type HarnessData struct {
	Conversation *Conversation
}

func main() {
	_ = godotenv.Load()

	var prompt string
	var reasoningEffort string
	flag.StringVar(&prompt, "p", "", "Prompt to send to LLM")
	flag.StringVar(&reasoningEffort, "reasoning", "medium", "Reasoning effort: none, minimal, low, medium, high, xhigh")
	flag.Parse()

	if prompt == "" {
		panic("Prompt must not be empty")
	}

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
		panic("Env variable OPENROUTER_API_KEY not found (not required when base URL points to localhost)")
	}

	toolManager := tools.NewManager()
	toolManager.Register(&tools.ReadTool{})
	toolManager.Register(&tools.WriteTool{})
	toolManager.Register(&tools.EditTool{})
	toolManager.Register(&tools.GlobTool{})
	toolManager.Register(&tools.BashTool{})

	conversation := NewConversation()
	conversation.AddMessage(openai.SystemMessage(buildSystemPrompt()))
	conversation.AddMessage(openai.UserMessage(prompt))
	for _, td := range tools.ToOpenAITools(toolManager.ToolDefinitions()) {
		conversation.AddTool(td)
	}

	eventsCh := make(chan internal.Event)
	go func() {
		for e := range eventsCh {
			renderEvent(os.Stderr, e)
		}
	}()

	harnessData := HarnessData{
		Conversation: conversation,
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseUrl))

	continueLoop := true
	maxTurns := 50
	turns := 0
	for ; turns < maxTurns; turns++ {
		if !continueLoop {
			break
		}
		params := openai.ChatCompletionNewParams{
			ReasoningEffort: openai.ReasoningEffort(reasoningEffort),
			Model:           model,
			Messages:        conversation.Messages,
			Tools:           conversation.Tools,
			N:               openai.Int(1),
		}

		resp, err := client.Chat.Completions.New(context.Background(), params)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if len(resp.Choices) == 0 {
			panic("No choices in response")
			// fmt.Fprintf(os.Stderr, "no choices in response: %v\n", err)
			// break
		}

		// We've set N=1 so we'll always get one choice for now
		choice := resp.Choices[0]

		param := choice.Message.ToParam()
		harnessData.Conversation.AddMessage(param)

		// Render the assistant's text content (reasoning/status messages)
		// before processing tool calls — this is how Claude Code shows
		// self-explanatory messages as it works through a task.
		if choice.Message.Content != "" {
			renderMarkdown(os.Stderr, choice.Message.Content)
		}

		switch choice.FinishReason {
		case "tool_calls":
			for _, toolCall := range choice.Message.ToolCalls {
				toolName := toolCall.Function.Name
				toolInput := toolCall.Function.Arguments

				result, err := toolManager.ExecuteTool(toolName, toolInput, eventsCh)
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
				harnessData.Conversation.AddMessage(openai.ToolMessage(content, toolCall.ID))
			}
		case "stop":
			continueLoop = false
		default:
			continueLoop = false
		}
	}

	if turns >= maxTurns {
		eventsCh <- internal.Event{
			Name:    "System",
			Args:    []string{},
			Message: fmt.Sprintf("Max turn (%d) limit reached. Exiting...\n", maxTurns),
		}
	}
}
