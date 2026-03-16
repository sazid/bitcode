# Interactive Session Architecture

This document describes the architecture of BitCode's interactive TUI mode — the persistent session model that enables always-visible input, mid-flight message injection, and inline permission prompts.

## Overview

Interactive mode runs a **single persistent bubbletea program** for the entire session. The terminal is logically split into two regions:

```
Terminal (scrollback — grows upward via p.Println):
  > user prompt
  [markdown response]
  [tool events]
  [more response]

Persistent TUI View (bottom — always rendered by View()):
  [2/5] . Working on feature...       <- todo status
  . Thinking...                        <- spinner (when agent active)
  +----------------------------------+
  | [textarea — always enabled]      | <- input
  +----------------------------------+
  ctrl+s send message . ctrl+c interrupt . ctrl+d exit
```

Agent output flows above the TUI view via `p.Println()`, scrolling naturally in the terminal scrollback. The `View()` method renders only the bottom portion: todo status, spinner, textarea, autocomplete suggestions, and context-dependent key hints.

## File Map

| File | Role |
|------|------|
| `app/session.go` | Session TUI model, orchestrator goroutine, programWriter, sessionCallbacks |
| `app/agent.go` | Agent loop, InjectedMessages channel, drainInjectedMessages |
| `app/main.go` | runInteractive entry point, runSingleShot, buildSlashCommands |
| `app/input.go` | Types (SlashCommand, InputResult, inputKeyMap), printWelcomeBanner, printHelp |
| `app/render.go` | renderMarkdown, renderEvent, Spinner (single-shot only), spinnerMessages |
| `app/todo.go` | RenderTodoStatus |
| `internal/guard/guard.go` | Decision, PermissionResult, PermissionHandler types |
| `internal/guard/manager.go` | Guard evaluation chain, handleAsk, session approval cache |
| `internal/guard/prompt.go` | AutoDenyHandler (for non-interactive mode) |

## Goroutine Architecture

Interactive mode runs three concurrent goroutines:

```
Main goroutine                  Orchestrator goroutine          Agent goroutine
─────────────                   ──────────────────────          ───────────────
tea.Program.Run()               runOrchestrator()               runAgentLoop()
  ↕ bubbletea event loop          ↕ select on submitCh,           ↕ LLM calls,
  ↕ processes key events,           agentDoneCh                    tool execution,
  ↕ renders View()                                                 guard evaluation
```

Communication flows through:

- **`submitCh chan InputResult`** — TUI model → orchestrator (user input)
- **`agentDoneCh chan struct{}`** — agent goroutine → orchestrator (completion signal)
- **`config.InjectedMessages chan string`** — orchestrator → agent loop (mid-flight messages)
- **`p.Send(msg)`** — orchestrator/agent → TUI model (state updates)
- **`p.Println(text)`** — agent callbacks → terminal scrollback (output above TUI)
- **`permRequestMsg.responseCh`** — TUI model → agent goroutine (permission response)

### Goroutine Ownership of Shared State

| State | Owner | Others' Access |
|-------|-------|---------------|
| `messages []llm.Message` | orchestrator (idle) / agent goroutine (running) | Exclusive — never concurrent |
| `sessionModel` fields | bubbletea event loop | via `p.Send()` only |
| `config.TaskTitle` | orchestrator | read by callbacks (benign race) |
| `config.MaxTurns`, `config.Reasoning` | orchestrator | read by agent (benign — set between runs) |

## State Machine

The `sessionModel` has three primary states:

```
                 ┌──────────────────────────────────────┐
                 │                                      │
                 ▼                                      │
           ┌──────────┐   user submits text    ┌───────────────┐
           │          │ ─────────────────────> │               │
           │   Idle   │                        │ Agent Running │
           │          │ <───────────────────── │               │
           └──────────┘   agentDoneMsg         └───────────────┘
                                                       │
                                                       │ permRequestMsg
                                                       ▼
                                              ┌────────────────────┐
                                              │    Permission      │
                                              │    Prompt          │
                                              │                    │
                                              │  choosing ←→       │
                                              │  feedback          │
                                              └────────────────────┘
                                                       │
                                                       │ user responds (y/a/n/t+enter)
                                                       ▼
                                              ┌───────────────┐
                                              │ Agent Running │
                                              └───────────────┘
```

### State Behavior

**Idle:**
- Textarea focused, user can type freely
- Ctrl+S submits text → orchestrator starts agent
- Slash commands execute immediately
- Hints: `ctrl+s submit · esc clear · ctrl+d exit`

**Agent Running:**
- Spinner animates in View()
- Textarea stays focused — user can type and submit
- Ctrl+S sends text as mid-flight injection
- Ctrl+C cancels agent via stored `context.CancelFunc`
- Slash commands still work (e.g. `/help`, `/reasoning`)
- Hints: `ctrl+s send message · ctrl+c interrupt · ctrl+d exit`

**Permission Prompt:**
- Textarea blurred
- Guard decision displayed with tool name and command preview
- Two sub-states:
  - **Choosing**: Single-key input — y (allow once), a (always allow), n (deny), t (tell what to do)
  - **Feedback**: `textinput.Model` focused for typing instructions, Enter to submit, Esc to go back

## Message Lifecycle

### Normal Message Flow

```
1. User types in textarea, presses Ctrl+S
2. sessionModel.Update() extracts text, sends InputResult to submitCh
3. Orchestrator receives from submitCh
4. Orchestrator prints user message via p.Println()
5. Orchestrator appends llm.TextMessage(RoleUser, text) to messages
6. Orchestrator spawns agent goroutine, sends agentStartMsg to TUI
7. Agent loop calls Provider.Complete() with messages
8. LLM response flows through sessionCallbacks:
   - OnThinking(true)  → p.Send(agentThinkingMsg{true})  → spinner starts
   - OnContent(text)   → renderMarkdown(programWriter)    → p.Println() above TUI
   - OnEvent(event)    → renderEvent(programWriter)       → p.Println() above TUI
   - OnThinking(false) → p.Send(agentThinkingMsg{false}) → spinner stops
9. Agent loop finishes → defer sends agentDoneMsg + agentDoneCh signal
10. TUI transitions to Idle, orchestrator sets agentRunning=false
```

### Mid-Flight Message Injection

```
1. User types while agent is running, presses Ctrl+S
2. sessionModel sends InputResult to submitCh
3. Orchestrator detects agentRunning==true
4. Orchestrator sends text to config.InjectedMessages channel
5. Orchestrator prints "(message will be delivered to the agent)" via p.Println()
6. Agent loop calls drainInjectedMessages() at:
   a. Top of each turn (before LLM call)
   b. After each tool execution
7. drainInjectedMessages() pulls all pending messages from channel
8. Appends each as llm.TextMessage(RoleUser, msg) to conversation
9. Next LLM call includes the injected message(s)
```

### Permission Prompt Flow

```
1. Agent loop calls cfg.GuardMgr.Evaluate() for a tool call
2. Guard rules return Decision{Verdict: VerdictAsk}
3. Manager.handleAsk() checks session approval cache — miss
4. Manager calls permHandler (set by orchestrator)
5. Permission handler (closure in orchestrator):
   a. Sends desktop notification
   b. Creates responseCh (buffered chan, size 1)
   c. Sends permRequestMsg{toolName, decision, responseCh} via p.Send()
   d. Blocks waiting on <-responseCh
6. TUI receives permRequestMsg:
   a. State → sessionPermissionPrompt
   b. Textarea blurred
   c. View() shows guard warning + y/a/n/t options
7. User presses key (e.g. 'y'):
   a. TUI sends PermissionResult{Approved: true} to responseCh
   b. State → sessionAgentRunning
   c. Textarea refocused
8. Permission handler unblocks, returns result to guard manager
9. Guard manager caches if Cache==true, returns VerdictAllow
10. Agent loop executes the tool
```

## programWriter Adapter

The `programWriter` bridges `io.Writer` (used by `renderMarkdown`, `renderEvent`, `printHelp`) with bubbletea's `p.Println()` which prints text above the persistent TUI view:

```go
type programWriter struct{ p *tea.Program }

func (pw *programWriter) Write(b []byte) (int, error) {
    text := strings.TrimRight(string(b), "\n")
    if text != "" {
        pw.p.Println(text)
    }
    return len(b), nil
}
```

This means existing render functions work unchanged — they write to an `io.Writer`, unaware that output appears above a persistent TUI rather than at the cursor position.

## Spinner Implementation

### Interactive Mode: Tick-Based (session.go)

The spinner runs inside the bubbletea event loop — no separate goroutine:

```
agentThinkingMsg{true}
  → set spinnerActive=true, spinnerFrame=0, pick random message
  → return tickSpinner() cmd

spinnerTickMsg (fires every 80ms)
  → increment spinnerFrame
  → swap message every ~45 frames (~3.6s)
  → return tickSpinner() cmd (loop continues)

agentThinkingMsg{false} or agentDoneMsg
  → set spinnerActive=false
  → spinnerTickMsg becomes no-op (returns nil cmd)
```

The spinner renders in `View()` as part of the persistent TUI — no cursor manipulation or escape codes for clearing lines.

### Single-Shot Mode: Goroutine-Based (render.go)

The `Spinner` struct launches a goroutine with `time.NewTicker(80ms)`. It writes directly to stderr with `\r\033[K` to overwrite the current line. Stopped by closing a channel. Used only by `singleShotCallbacks()`.

## Orchestrator Detail

The orchestrator goroutine (`runOrchestrator`) is the central coordinator:

```go
func runOrchestrator(p *tea.Program, config *AgentConfig, submitCh chan InputResult) {
    messages, toolDefs := newConversation(config)
    agentDoneCh := make(chan struct{}, 1)
    injectedMessages := make(chan string, 8)

    // Main loop: select on submitCh and agentDoneCh
    for {
        select {
        case result, ok := <-submitCh:
            // Handle user input...
        case <-agentDoneCh:
            agentRunning = false
        }
    }
}
```

### Race Avoidance

When the orchestrator receives from `submitCh`, it first does a non-blocking check on `agentDoneCh`:

```go
select {
case <-agentDoneCh:
    agentRunning = false
default:
}
```

This prevents a timing edge case where the agent finishes and the user submits simultaneously — without this check, Go's `select` could pick `submitCh` first while `agentRunning` is stale, causing the message to be injected instead of starting a new agent turn.

### Agent Goroutine Lifecycle

```go
go func() {
    defer func() {
        if r := recover(); r != nil {
            p.Println(fmt.Sprintf("Agent panic: %v", r))
        }
        p.Send(agentDoneMsg{})          // notify TUI
        agentDoneCh <- struct{}{}       // notify orchestrator
        notify.Send(title, "Waiting")   // desktop notification
    }()
    runAgentLoop(ctx, config, &messages, toolDefs, sessionCallbacks(p, config))
}()
```

The `recover()` prevents agent panics from crashing the entire TUI — the user sees the error and can continue.

## Slash Command Handling

Slash commands are routed through the orchestrator, not the TUI model. The TUI only detects the `/` prefix and sends `InputResult{Command: text}` to `submitCh`.

| Command | While Idle | While Agent Running |
|---------|-----------|-------------------|
| `/new` | Clears conversation + todos | Blocked with error message |
| `/help` | Prints help above TUI | Prints help above TUI |
| `/reasoning <effort>` | Updates config | Updates config |
| `/turns [N]` | Gets/sets max turns | Gets/sets max turns |
| `/exit`, `/quit` | Quits program | Quits program |
| `/<skill> [args]` | Formats prompt, starts agent | Formats prompt, injects mid-flight |

Skills fall through from the command handler to the text submission path — the formatted prompt is treated as regular user input.

## Signal Handling

### Interactive Mode

Bubbletea owns the terminal and intercepts signals internally. Ctrl+C is delivered as `tea.KeyCtrlC` to `Update()`:

- **Agent running**: Calls `m.agentCancel()` to cancel the context
- **Textarea has text**: Clears the textarea
- **Textarea empty**: Closes `submitCh`, returns `tea.Quit`

No explicit `signal.Notify` registration is needed.

### Single-Shot Mode

Uses traditional Go signal handling:

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
go func() {
    <-sigCh
    cancel()  // cancels agent context
}()
```

The agent loop checks `ctx.Err()` at the top of each turn and after each tool call.

## Keyboard Shortcuts

| Key | Idle | Agent Running | Permission Prompt |
|-----|------|--------------|------------------|
| Ctrl+S | Submit text / command | Send message to agent mid-flight | — |
| Ctrl+C | Clear text / quit if empty | Cancel agent | — |
| Ctrl+D | Exit immediately | Exit immediately | — |
| Escape | Close suggestions / clear text | Close suggestions / clear text | — |
| Tab | Accept autocomplete suggestion | Accept autocomplete suggestion | — |
| Up/Down | Navigate suggestions | Navigate suggestions | — |
| y | Type 'y' | Type 'y' | Allow once |
| a | Type 'a' | Type 'a' | Always allow |
| n | Type 'n' | Type 'n' | Deny |
| t | Type 't' | Type 't' | Enter feedback mode |
| Enter | New line in textarea | New line in textarea | Submit feedback (in feedback mode) |

## Autocomplete

The autocomplete system triggers when the textarea value starts with `/`, contains no newlines, and no spaces:

1. `updateSuggestions()` runs after every keystroke
2. Filters `commands` list by substring match on command name
3. Sorts: prefix matches first, then alphabetical
4. Renders up to 8 suggestions between textarea and hints
5. Tab accepts the selected suggestion
6. Up/Down navigates, Escape dismisses

The command list includes builtins (`/new`, `/help`, etc.) and discovered skills from `.bitcode/`, `.claude/`, `.agents/` directories.

## Differences from Single-Shot Mode

| Aspect | Interactive | Single-Shot |
|--------|------------|-------------|
| TUI | Persistent bubbletea program | None |
| Spinner | Tick-based in View() | Goroutine-based on stderr |
| Output | programWriter → p.Println() | Direct stderr writes |
| Permission | Inline in TUI | AutoDenyHandler (always deny) |
| Signals | Bubbletea handles Ctrl+C | signal.Notify goroutine |
| Callbacks | sessionCallbacks() | singleShotCallbacks() |
| Message injection | Supported via channel | Not applicable |
| User input | Always available | Single prompt via -p flag |
