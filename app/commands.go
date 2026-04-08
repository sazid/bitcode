package main

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/telemetry"
)

// CommandDispatcher handles built-in slash commands and skill routing.
type CommandDispatcher struct {
	config *AgentConfig
	themes *ThemeRegistry
	p      *tea.Program
}

// DispatchResult describes what happened after dispatching a command.
type DispatchResult struct {
	Handled  bool          // true if the command was fully handled (no agent input needed)
	Text     string        // non-empty text to send to the agent (for skill invocations)
	Quit     bool          // true if the user wants to exit
	Messages []llm.Message // optional: pre-loaded messages for resumed conversations
}

func NewCommandDispatcher(config *AgentConfig, themes *ThemeRegistry, p *tea.Program) *CommandDispatcher {
	return &CommandDispatcher{config: config, themes: themes, p: p}
}

func (d *CommandDispatcher) Dispatch(command string, agentRunning bool, resetConversation func() ([]llm.Message, []llm.ToolDef)) DispatchResult {
	cmdParts := strings.SplitN(command, " ", 2)
	cmdName := cmdParts[0]
	cmdArgs := ""
	if len(cmdParts) > 1 {
		cmdArgs = strings.TrimSpace(cmdParts[1])
	}

	successStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Success) }
	dimStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Dim) }
	errorStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Error) }
	skillStyle := func() lipgloss.Style { return lipgloss.NewStyle().Foreground(d.themes.Active().Secondary) }

	switch cmdName {
	case "/exit", "/quit":
		d.p.Quit()
		return DispatchResult{Handled: true, Quit: true}

	case "/new":
		if agentRunning {
			d.p.Send(appendOutputMsg(errorStyle().Render("\n  Cannot start new conversation while agent is running. Press Ctrl+C first.")))
		} else {
			d.config.TodoStore.Clear()
			resetConversation()
			d.config.ConvID = "" // Clear current conversation ID
			newTaskID := GenerateTaskID()
			if d.config.Observer != nil {
				d.config.Observer.ResetSession(newTaskID)
			}
			d.p.Send(newConversationMsg{taskID: newTaskID})
			d.p.Send(appendOutputMsg(successStyle().Render("\n  \u2713 Started new conversation")))
		}
		return DispatchResult{Handled: true}

	case "/help":
		var buf strings.Builder
		printHelp(&buf, d.themes.Active(), d.config.SkillManager)
		text := strings.TrimRight(buf.String(), "\n")
		if text != "" {
			d.p.Send(appendOutputMsg(text))
		}
		return DispatchResult{Handled: true}

	case "/turns":
		d.handleTurns(cmdArgs, dimStyle, errorStyle, successStyle)
		return DispatchResult{Handled: true}

	case "/reasoning":
		d.handleReasoning(cmdArgs, errorStyle, successStyle, dimStyle)
		return DispatchResult{Handled: true}

	case "/theme":
		d.handleTheme(cmdArgs, dimStyle, errorStyle, successStyle)
		return DispatchResult{Handled: true}

	case "/stats":
		if d.config.Observer != nil {
			stats := d.config.Observer.Stats()
			text := telemetry.FormatStats(stats)
			d.p.Send(appendOutputMsg(dimStyle().Render(text)))
		} else {
			d.p.Send(appendOutputMsg(dimStyle().Render("\n  Telemetry not enabled")))
		}
		return DispatchResult{Handled: true}

	case "/history":
		d.handleHistory(cmdArgs, dimStyle, errorStyle)
		return DispatchResult{Handled: true}

	case "/search":
		d.handleSearch(cmdArgs, dimStyle, errorStyle)
		return DispatchResult{Handled: true}

	case "/resume":
		return d.handleResume(cmdArgs, agentRunning, resetConversation, dimStyle, errorStyle, successStyle)

	case "/fork":
		return d.handleFork(cmdArgs, agentRunning, resetConversation, dimStyle, errorStyle, successStyle)

	case "/rename":
		d.handleRename(cmdArgs, dimStyle, errorStyle, successStyle)
		return DispatchResult{Handled: true}

	default:
		return d.handleSkillOrUnknown(cmdName, cmdArgs, errorStyle, dimStyle, skillStyle)
	}
}

func (d *CommandDispatcher) handleTurns(args string, dimStyle, errorStyle, successStyle func() lipgloss.Style) {
	if args == "" {
		d.p.Send(appendOutputMsg(dimStyle().Render(fmt.Sprintf("\n  Current max turns: %d", d.config.MaxTurns))))
	} else {
		var n int
		if _, err := fmt.Sscan(args, &n); err != nil || n <= 0 {
			d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Invalid value: %s (must be a positive integer)", args))))
		} else {
			d.config.MaxTurns = n
			d.p.Send(appendOutputMsg(successStyle().Render(fmt.Sprintf("\n  \u2713 Max turns set to %d", d.config.MaxTurns))))
		}
	}
}

func (d *CommandDispatcher) handleReasoning(args string, errorStyle, successStyle, dimStyle func() lipgloss.Style) {
	validEfforts := []string{"none", "low", "medium", "high", "xhigh"}
	a := strings.ToLower(args)
	if a == "" || a == "default" || a == "clear" {
		d.config.Reasoning = ""
		d.p.Send(appendOutputMsg(successStyle().Render("\n  \u2713 Reasoning effort reset to default")))
	} else {
		valid := false
		for _, e := range validEfforts {
			if a == e {
				valid = true
				break
			}
		}
		if !valid {
			d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Invalid reasoning effort: %s", args))))
			d.p.Send(appendOutputMsg(dimStyle().Render("  Valid options: none, low, medium, high, xhigh, default")))
		} else {
			d.config.Reasoning = a
			d.p.Send(appendOutputMsg(successStyle().Render("\n  \u2713 Reasoning effort set to " + d.config.Reasoning)))
		}
	}
}

func (d *CommandDispatcher) handleTheme(args string, dimStyle, errorStyle, successStyle func() lipgloss.Style) {
	if args == "" {
		activeName := d.themes.Active().Name
		var listing strings.Builder
		listing.WriteString("\n  Themes:\n")
		for _, name := range d.themes.Names() {
			marker := "  "
			if name == activeName {
				marker = "* "
			}
			listing.WriteString(fmt.Sprintf("    %s%s\n", marker, name))
		}
		d.p.Send(appendOutputMsg(dimStyle().Render(listing.String())))
	} else {
		name := strings.ToLower(args)
		if d.themes.Set(name) {
			d.p.Send(appendOutputMsg(successStyle().Render(fmt.Sprintf("\n  \u2713 Theme set to %s", name))))
		} else {
			d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Unknown theme: %s", args))))
			d.p.Send(appendOutputMsg(dimStyle().Render("  Available: " + strings.Join(d.themes.Names(), ", "))))
		}
	}
}

func (d *CommandDispatcher) handleSkillOrUnknown(cmdName, cmdArgs string, errorStyle, dimStyle, skillStyle func() lipgloss.Style) DispatchResult {
	skillName := strings.TrimPrefix(cmdName, "/")
	if skill, ok := d.config.SkillManager.Get(skillName); ok {
		d.p.Send(appendOutputMsg(fmt.Sprintf("\n%s %s", skillStyle().Render("\u26a1"), skill.Name)))
		return DispatchResult{Handled: false, Text: skill.FormatPrompt(cmdArgs)}
	}

	d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Unknown command: %s", cmdName))))
	d.p.Send(appendOutputMsg(dimStyle().Render("  Type /help for available commands")))
	return DispatchResult{Handled: true}
}

// handleHistory lists recent conversations.
func (d *CommandDispatcher) handleHistory(args string, dimStyle, errorStyle func() lipgloss.Style) {
	if d.config.ConvManager == nil {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  Conversation storage not available")))
		return
	}

	convs, err := d.config.ConvManager.List(strings.Contains(args, "--all"), 20)
	if err != nil {
		d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Error loading history: %v", err))))
		return
	}

	if len(convs) == 0 {
		d.p.Send(appendOutputMsg(dimStyle().Render("\n  No conversations found")))
		return
	}

	var buf strings.Builder
	buf.WriteString("\n  Recent conversations:\n")
	for _, conv := range convs {
		buf.WriteString(fmt.Sprintf("    %s  %s  (%d messages, %s)\n",
			conv.ID,
			conv.Title,
			conv.MessageCount,
			conv.UpdatedAt.Format("Jan 2 15:04")))
	}
	d.p.Send(appendOutputMsg(dimStyle().Render(buf.String())))
}

// handleSearch searches conversations for a query.
func (d *CommandDispatcher) handleSearch(query string, dimStyle, errorStyle func() lipgloss.Style) {
	if d.config.ConvManager == nil {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  Conversation storage not available")))
		return
	}

	showAll := strings.Contains(query, "--all")
	if showAll {
		query = strings.TrimSpace(strings.ReplaceAll(query, "--all", ""))
	}

	if query == "" {
		d.p.Send(appendOutputMsg(dimStyle().Render("\n  Usage: /search <query> [--all]")))
		return
	}

	results, err := d.config.ConvManager.Search(query, showAll, 20)
	if err != nil {
		d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Error searching: %v", err))))
		return
	}

	if len(results) == 0 {
		d.p.Send(appendOutputMsg(dimStyle().Render("\n  No matches found")))
		return
	}

	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("\n  Found %d conversation(s) matching %q:\n", len(results), query))
	for _, res := range results {
		buf.WriteString(fmt.Sprintf("    %s  %s  (%d matches)\n",
			res.ID,
			res.Title,
			len(res.Matches)))
	}
	d.p.Send(appendOutputMsg(dimStyle().Render(buf.String())))
}

// handleResume resumes a conversation by ID.
func (d *CommandDispatcher) handleResume(args string, agentRunning bool, resetConversation func() ([]llm.Message, []llm.ToolDef), dimStyle, errorStyle, successStyle func() lipgloss.Style) DispatchResult {
	if d.config.ConvManager == nil {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  Conversation storage not available")))
		return DispatchResult{Handled: true}
	}

	if args == "" {
		d.p.Send(appendOutputMsg(dimStyle().Render("\n  Usage: /resume <conversation-id>")))
		return DispatchResult{Handled: true}
	}

	if agentRunning {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  Cannot resume while agent is running. Press Ctrl+C first.")))
		return DispatchResult{Handled: true}
	}

	conv, err := d.config.ConvManager.Load(args)
	if err != nil {
		d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Error loading conversation: %v", err))))
		return DispatchResult{Handled: true}
	}

	// Reset conversation and load messages
	d.config.TodoStore.Clear()
	newMessages, _ := resetConversation()

	// Set the conversation ID and messages (merge system prompt with loaded messages)
	d.config.ConvID = conv.ID

	d.p.Send(newConversationMsg{taskID: conv.ID})
	d.p.Send(appendOutputMsg(successStyle().Render(fmt.Sprintf("\n  \u2713 Resumed conversation: %s (%d messages)", conv.Title, len(conv.Messages)))))

	// Return the loaded messages to be used by the orchestrator
	// Keep the system prompt from newMessages[0] and append loaded messages
	return DispatchResult{
		Handled:  true,
		Messages: append([]llm.Message{newMessages[0]}, conv.Messages...),
	}
}

// handleFork forks a conversation at a specific message index.
func (d *CommandDispatcher) handleFork(args string, agentRunning bool, resetConversation func() ([]llm.Message, []llm.ToolDef), dimStyle, errorStyle, successStyle func() lipgloss.Style) DispatchResult {
	if d.config.ConvManager == nil {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  Conversation storage not available")))
		return DispatchResult{Handled: true}
	}

	if args == "" {
		d.p.Send(appendOutputMsg(dimStyle().Render("\n  Usage: /fork <conversation-id> [message-index]")))
		return DispatchResult{Handled: true}
	}

	if agentRunning {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  Cannot fork while agent is running. Press Ctrl+C first.")))
		return DispatchResult{Handled: true}
	}

	// Parse args: "conv-id" or "conv-id 5"
	parts := strings.Fields(args)
	convID := parts[0]
	msgIdx := -1
	if len(parts) > 1 {
		if n, err := strconv.Atoi(parts[1]); err == nil {
			msgIdx = n
		}
	}

	source, err := d.config.ConvManager.Load(convID)
	if err != nil {
		d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Error loading conversation: %v", err))))
		return DispatchResult{Handled: true}
	}

	newTitle := "Fork of " + source.Title
	forked, err := d.config.ConvManager.Fork(convID, newTitle, msgIdx)
	if err != nil {
		d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Error forking conversation: %v", err))))
		return DispatchResult{Handled: true}
	}

	// Reset and switch to forked conversation
	d.config.TodoStore.Clear()
	resetConversation()
	d.config.ConvID = forked.ID

	d.p.Send(newConversationMsg{taskID: forked.ID})
	d.p.Send(appendOutputMsg(successStyle().Render(fmt.Sprintf("\n  \u2713 Created fork: %s -> %s (%d messages)", source.ID, forked.ID, len(forked.Messages)))))

	return DispatchResult{Handled: true}
}

// handleRename renames the current conversation.
func (d *CommandDispatcher) handleRename(args string, dimStyle, errorStyle, successStyle func() lipgloss.Style) {
	if d.config.ConvManager == nil {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  Conversation storage not available")))
		return
	}

	if d.config.ConvID == "" {
		d.p.Send(appendOutputMsg(errorStyle().Render("\n  No active conversation to rename")))
		return
	}

	if args == "" {
		d.p.Send(appendOutputMsg(dimStyle().Render("\n  Usage: /rename <new-title>")))
		return
	}

	if err := d.config.ConvManager.Rename(d.config.ConvID, args); err != nil {
		d.p.Send(appendOutputMsg(errorStyle().Render(fmt.Sprintf("\n  Error renaming conversation: %v", err))))
		return
	}

	d.p.Send(appendOutputMsg(successStyle().Render(fmt.Sprintf("\n  \u2713 Renamed conversation to: %s", args))))
}
