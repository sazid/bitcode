# BitCode

<div align="center">

```
 ▄▀▀▄▄▀▀▄
 █▄▄██▄▄█
  ▀▀  ▀▀
```


**[Download](https://github.com/sazid/bitcode/releases/latest)** — Linux and macOS · Windows: use WSL

</div>

```sh
curl -fsSL https://raw.githubusercontent.com/sazid/bitcode/main/install.sh | sh
```

---

[![asciicast](https://asciinema.org/a/OEbplbhzYB1xU4bl.svg)](https://asciinema.org/a/OEbplbhzYB1xU4bl)

An agentic AI coding assistant in your terminal — with interactive TUI, smart security guards, extensible skills, built-in todo tracking with planning, and full control over reasoning effort.

## Features

- **Interactive Mode** — Full TUI with multiline input editor, bordered prompt, and keyboard shortcuts
- **Single-Shot Mode** — Run a single prompt from the command line with `-p`
- **Agent Loop** — Iterative LLM conversation with automatic tool calling (up to 50 turns)
- **Tool Guards** — Safety layer that validates tool calls before execution (rules-based, user prompts, or LLM-powered)
- **Guard Agent** — Expert multi-turn LLM agent for security-aware tool validation with language-specific skills
- **System Reminders** — Dynamic context injection via `<system-reminder>` tags with [plugin support](docs/system-reminders.md)
- **Skills** — User-defined prompt templates loaded from `.agents/`, `.claude/`, or `.bitcode/` directories
- **Markdown Rendering** — Rich terminal output with syntax-highlighted code blocks
- **Reasoning Control** — Adjustable reasoning effort (`--reasoning` flag)
- **Multi-Provider Support** — Anthropic, OpenAI (Chat Completions + Responses API), OpenRouter, and any OpenAI-compatible API
- **Multi-Modal** — Images, audio, and document content in conversations
- **WebSocket Streaming** — Optional WebSocket transport for faster tool-heavy workflows (OpenAI Responses API)

### Tools

| Tool | Description |
|------|-------------|
| Read | Read files with optional line offset/limit |
| Write | Create or overwrite files |
| Edit | Surgical find-and-replace edits |
| Glob | Fast file pattern matching |
| Bash | Execute shell commands |
| LineCount | Count lines in a file (SIMD-optimized) |
| FileSize | Get file size in bytes |
| WebSearch | Search the web for current information |
| TodoRead | Read current todo list |
| TodoWrite | Create or update todo list |
| Skill | Invoke user-defined prompt templates |

## Requirements

- Go 1.26+
- An API key for your LLM provider (not required for localhost)

## Getting Started

### 1. Clone and configure

Create a `.env` file in the project root:

```sh
# Anthropic (auto-detected from model name)
BITCODE_API_KEY=sk-ant-xxxxxxxxxxxx
BITCODE_MODEL=claude-sonnet-4-6

# Or OpenRouter / OpenAI-compatible
BITCODE_API_KEY=sk-or-v1-xxxxxxxxxxxx
BITCODE_MODEL=openrouter/free
BITCODE_BASE_URL=https://openrouter.ai/api/v1
```

### 2. Build

```sh
go build -o bitcode ./app
```

### 3. Run

**Interactive mode** (default):

```sh
./bitcode
```

This launches a TUI with a multiline input editor. Use `Ctrl+S` to submit, `Enter` for newlines, `Esc` to clear, and `Ctrl+D` to exit.

**Single-shot mode:**

```sh
./bitcode -p "Your prompt here"
```

**With reasoning effort:**

```sh
./bitcode --reasoning high -p "Refactor the agent loop"
```

**With a different model:**

```sh
BITCODE_MODEL=claude-opus-4-6 ./bitcode -p "Explain main.go"
```

## Environment Variables

### LLM Provider

| Variable | Description | Default |
|---|---|---|
| `BITCODE_API_KEY` | API key for the LLM provider (not required for localhost) | *(required for remote)* |
| `BITCODE_MODEL` | Model to use | auto-detected from provider |
| `BITCODE_BASE_URL` | API endpoint | auto-detected from provider |
| `BITCODE_PROVIDER` | Backend: `openai-chat`, `openai-responses`, `anthropic` | auto-detect from model name |
| `BITCODE_WEBSOCKET` | Use WebSocket transport (only for `openai-responses`) | `false` |

The provider is auto-detected: if no base URL is set and the model starts with `claude-`, it connects to Anthropic's API directly. If a custom base URL is set (OpenRouter, Bedrock, local proxy, etc.), it always uses OpenAI Chat Completions format — the universal compatibility protocol these services expose.

**Examples:**

```sh
# Direct Anthropic (auto-detected, no base URL needed)
BITCODE_API_KEY=sk-ant-xxx BITCODE_MODEL=claude-sonnet-4-6

# Claude via OpenRouter (base URL forces openai-chat format)
BITCODE_API_KEY=sk-or-xxx BITCODE_MODEL=anthropic/claude-sonnet-4-6 BITCODE_BASE_URL=https://openrouter.ai/api/v1

# Claude via AWS Bedrock (base URL forces openai-chat format)
BITCODE_API_KEY=xxx BITCODE_MODEL=anthropic.claude-v2 BITCODE_BASE_URL=https://bedrock-runtime.us-east-1.amazonaws.com

# Local server (no API key needed)
BITCODE_BASE_URL=http://localhost:1234/v1 BITCODE_MODEL=local-model
```

### Guard Agent

| Variable | Description | Default |
|---|---|---|
| `BITCODE_GUARD` | Enable the LLM-powered guard agent | `true` |
| `BITCODE_GUARD_MODEL` | Model for guard agent | *(same as main)* |
| `BITCODE_GUARD_API_KEY` | API key for guard agent | *(same as main)* |
| `BITCODE_GUARD_BASE_URL` | Base URL for guard agent | *(same as main)* |
| `BITCODE_GUARD_PROVIDER` | Backend for guard agent | *(same as main)* |
| `BITCODE_GUARD_MAX_TURNS` | Max turns for guard agent | unlimited |

## Interactive Mode Keys

| Key | Action |
|---|---|
| `Ctrl+S` | Submit input |
| `Enter` | New line |
| `Escape` | Clear input |
| `Ctrl+C` | Clear input (exit if empty) |
| `Ctrl+D` | Exit |

## Commands

Type these in the interactive prompt:

| Command | Description |
|---|---|
| `/new` | Start a new conversation |
| `/help` | Show help |
| `/exit` | Exit BitCode |

## System Reminders

BitCode can inject dynamic context into the LLM conversation each turn using `<system-reminder>` tags. Reminders are evaluated before every API call and injected into a copy of the messages — the stored conversation history stays clean.

Built-in reminders handle skill availability and conversation length warnings. You can add your own by dropping plugin files into a `reminders/` subdirectory:

```yaml
# .bitcode/reminders/run-tests.yaml
id: run-tests
content: |
  Files were just edited. Consider running tests to verify the changes.
schedule:
  kind: condition
  condition: "after_tool:Edit,Write"
  max_fires: 5
priority: 3
```

Plugins are loaded from `{.agents,.claude,.bitcode}/reminders/` at both the user home and project level. See [docs/system-reminders.md](docs/system-reminders.md) for the full architecture, schedule kinds, condition expressions, and more examples.

## Tool Guards

BitCode includes a safety layer that validates tool calls before execution. The guard system can:
- **Allow** — Proceed with tool execution
- **Deny** — Block the tool call with an error
- **Ask** — Prompt the user for approval
- **LLM Guard** — Use a secondary LLM to evaluate the tool call

The **Guard Agent** is an expert multi-turn LLM agent specifically designed for security-aware tool validation. It:
- Uses a security/sysadmin persona system prompt
- Automatically injects language-specific security skills (Bash, Python, Go, JavaScript) based on detected environment
- Supports on-demand "simulate" skill for step-by-step code tracing
- Can escalate to user prompts when uncertain

The guard agent is enabled by default. Set `BITCODE_GUARD=false` to use only rule-based guards.

See [docs/tool-guards.md](docs/tool-guards.md) for the full architecture, rule definitions, and customization options.

## Todo System

BitCode includes a built-in task management system accessible via tools:

- **TodoWrite** — Create and manage a structured task list for the current session
- **TodoRead** — Read the current todo list

Each task has an id, content, status (`pending`, `in_progress`, `completed`), and priority (`high`, `medium`, `low`). The agent automatically tracks progress and updates the todo list as tasks are completed.

See [docs/todo.md](docs/todo.md) for detailed usage patterns and best practices.

## Project Structure

```
app/
  main.go           # Entry point, CLI flags, interactive REPL loop
  agent.go          # Agent loop (LLM ↔ tool call cycle)
  input.go          # TUI input editor (bubbletea textarea)
  render.go         # Terminal rendering (markdown, spinner, events)
  system_prompt.go  # System prompt construction
internal/
  event.go          # Event types for tool output
  llm/
    llm.go              # Provider interface, message types, multi-modal content blocks
    openai.go           # OpenAI Chat Completions provider
    anthropic.go        # Anthropic Messages API provider
    openai_responses.go # OpenAI Responses API provider (HTTP SSE)
    openai_responses_ws.go # OpenAI Responses API provider (WebSocket)
    provider.go         # Provider factory and auto-detection
    sse/                # Reusable SSE stream parser
  guard/            # Tool guard system (rules, LLM validator, guard agent)
    skills/         # Guard agent security skills (bash, python, go, js, simulate)
  reminder/         # System reminder framework (evaluation, injection, plugins)
  skills/           # Skill discovery and management
  tools/            # Tool implementations (read, write, edit, glob, bash, todo)
docs/
  tool-guards.md    # Tool guard architecture and customization
  todo.md           # Todo system usage guide
  system-reminders.md  # System reminders architecture and plugin guide
```

## License

See [LICENSE](LICENSE) for the full license text.
