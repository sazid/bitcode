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
	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/skills"
	"github.com/sazid/bitcode/internal/tools"
)

func main() {
	_ = godotenv.Load()

	var prompt string
	var reasoningEffort string
	flag.StringVar(&prompt, "p", "", "Prompt to send to LLM (omit for interactive mode)")
	flag.StringVar(&reasoningEffort, "reasoning", "", "Reasoning effort: low, medium, high (omit to let the model decide)")
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

	skillManager := skills.NewManager()
	toolManager.Register(&tools.SkillTool{SkillManager: skillManager})

	config := &AgentConfig{
		Provider:     llm.NewOpenAIProvider(apiKey, baseUrl),
		Model:        model,
		Reasoning:    reasoningEffort,
		ToolManager:  toolManager,
		SkillManager: skillManager,
	}

	if prompt != "" {
		runSingleShot(config, prompt)
	} else {
		runInteractive(config)
	}
}

func newConversation(config *AgentConfig) ([]llm.Message, []llm.ToolDef) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, buildSystemPrompt(config.SkillManager)),
	}
	return messages, toolDefsFromManager(config.ToolManager)
}

func toolDefsFromManager(m *tools.Manager) []llm.ToolDef {
	var defs []llm.ToolDef
	for _, d := range m.ToolDefinitions() {
		defs = append(defs, llm.ToolDef{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		})
	}
	return defs
}

func defaultCallbacks() AgentCallbacks {
	var spin *Spinner
	return AgentCallbacks{
		OnContent: func(content string) {
			renderMarkdown(os.Stderr, content)
		},
		OnThinking: func(active bool) {
			if active {
				spin = StartSpinner(os.Stderr)
			} else if spin != nil {
				spin.Stop()
				spin = nil
			}
		},
		OnEvent: func(e internal.Event) {
			renderEvent(os.Stderr, e)
		},
		OnError: func(err error) {
			fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
		},
	}
}

// runSingleShot runs a single prompt through the agent loop and exits.
func runSingleShot(config *AgentConfig, prompt string) {
	messages, toolDefs := newConversation(config)
	messages = append(messages, llm.TextMessage(llm.RoleUser, prompt))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	runAgentLoop(ctx, config, &messages, toolDefs, defaultCallbacks())
}

// runInteractive runs the interactive REPL mode.
func runInteractive(config *AgentConfig) {
	successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	printWelcomeBanner(config.Model, config.Reasoning)

	messages, toolDefs := newConversation(config)

	for {
		result := readInput()

		if result.EOF {
			fmt.Fprintln(os.Stderr, dimStyle.Render("\nGoodbye!"))
			break
		}

		if result.Command != "" {
			// Extract command name and optional arguments
			cmdParts := strings.SplitN(result.Command, " ", 2)
			cmdName := cmdParts[0]
			cmdArgs := ""
			if len(cmdParts) > 1 {
				cmdArgs = strings.TrimSpace(cmdParts[1])
			}

			switch cmdName {
			case "/exit", "/quit":
				fmt.Fprintln(os.Stderr, dimStyle.Render("\nGoodbye!"))
				return
			case "/new":
				messages, toolDefs = newConversation(config)
				fmt.Fprintln(os.Stderr, successStyle.Render("\n  ✓ Started new conversation"))
				continue
			case "/help":
				printHelp(config.SkillManager)
				continue
			default:
				// Check if it's a skill
				skillName := strings.TrimPrefix(cmdName, "/")
				if skill, ok := config.SkillManager.Get(skillName); ok {
					prompt := skill.FormatPrompt(cmdArgs)

					skillStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
					fmt.Fprintf(os.Stderr, "\n%s %s\n", skillStyle.Render("⚡"), skill.Name)

					result.Text = prompt
				} else {
					fmt.Fprintln(os.Stderr, errorStyle.Render(
						fmt.Sprintf("\n  Unknown command: %s", cmdName),
					))
					fmt.Fprintln(os.Stderr, dimStyle.Render("  Type /help for available commands"))
					continue
				}
			}
		}

		if result.Text == "" {
			continue
		}

		// Show the user's message
		userStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
		fmt.Fprintf(os.Stderr, "\n%s %s\n", userStyle.Render(">"), result.Text)

		messages = append(messages, llm.TextMessage(llm.RoleUser, result.Text))

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

		runAgentLoop(ctx, config, &messages, toolDefs, defaultCallbacks())

		signal.Stop(sigCh)
		cancel()
	}
}
