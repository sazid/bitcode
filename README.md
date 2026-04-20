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

<a href="https://asciinema.org/a/850426" target="_blank"><img src="https://asciinema.org/a/850426.svg" /></a>

Code with agents. Built-in security guards, resumable sessions, and subagents for complex tasks — all in your terminal.

## Features

- **Agentic Coding** — Interactive TUI or single-shot mode (`-p`) with iterative tool calling
- **Subagents** — Spawn specialized agents for complex tasks (explore, plan, general-purpose)
- **Resume Sessions** — Continue any conversation with `/resume` in interactive mode or `-c` flag in single-shot mode; scoped to your working directory
- **Conversation Management** — List, search, fork, rollback, and rename conversations; automatic persistence to `~/.bitcode/conversations/`
- **Context Compaction** — Summarize and compact long conversations to free up context space with the `Compact` tool
- **Security Guards** — Multi-layer validation: rules, user prompts, and LLM-powered guard agent
- **Language-Aware Guards** — Bash, Python, Go, JavaScript, and PowerShell security skills
- **Skills** — User-defined prompt templates from `.agents/`, `.claude/`, or `.bitcode/`
- **System Reminders** — Dynamic context injection via `<system-reminder>` with [plugin support](docs/system-reminders.md)
  - Built-in reminders: skill availability, conversation length warnings, core behavior, todo discipline, doom-loop detection, verification gates
  - Custom plugin reminders via YAML files in `{.agents,.claude,.bitcode}/reminders/`
- **Reasoning Control** — Adjust effort with `--reasoning` flag
- **Multi-Provider** — Anthropic, OpenAI, OpenRouter, or any OpenAI-compatible API
- **Multi-Modal** — Images, audio, documents
- **WebSocket Streaming** — Faster tool-heavy workflows (OpenAI Responses API)

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
| Compact | Summarize and compact conversation history to free context space |
| Agent | Spawn specialized subagents for complex tasks (explore, plan, general-purpose) |

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

### CLI Flags

| Flag | Description |
|---|---|
| `-p "<prompt>"` | Single-shot mode with the given prompt (omit for interactive TUI) |
| `-c <id>` | Resume a conversation by ID (single-shot mode only) |
| `--reasoning [none/low/medium/high/xhigh]` | Set reasoning effort (let model decide if omitted) |
| `--max-turns <n>` | Maximum agent turns per conversation (default: 100) |
| `-q` | Quiet mode: suppress tool usage and spinner output (single-shot only) |
| `--version` | Show version information |

## Environment Variables

### LLM Provider

| Variable | Description | Default |
|---|---|---|
| `BITCODE_API_KEY` | API key for the LLM provider (not required for localhost) | *(required for remote)* |
| `BITCODE_MODEL` | Model to use | auto-detected from provider |
| `BITCODE_BASE_URL` | API endpoint | auto-detected from provider |
| `BITCODE_PROVIDER` | Backend: `openai-chat`, `openai-responses`, `anthropic` | auto-detect from model name |
| `BITCODE_WEBSOCKET` | Use WebSocket transport (only for `openai-responses`) | `false` |
| `BITCODE_CONVERSATIONS` | Enable conversation persistence | `true` |

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

### Telemetry

BitCode collects anonymized telemetry data to help improve the tool. Telemetry is **enabled by default** and can be disabled by setting the environment variable `BITCODE_TELEMETRY=false`.

| Variable | Description | Default |
|---|---|---|
| `BITCODE_TELEMETRY` | Enable/disable telemetry collection | `true` (enabled by default) |

**What is collected:**
- LLM API calls: latency, token usage (input/output), cache hit rates, model/provider info
- Tool executions: success/failure rates, latency, input/output sizes
- Guard evaluations: verdicts, duration
- Session metadata: start/end times, turn counts, mode (interactive/single-shot)
- Errors: component and message (no stack traces or user data)

**Where is data stored?**
Telemetry data is stored as JSONL files in `~/.bitcode/telemetry/`. Files are rotated daily (e.g., `2026-03-28.jsonl`).

**Viewing telemetry:**
In interactive mode, use the `/stats` command to see current session telemetry metrics.

**Privacy:**
Telemetry does not collect:
- Prompt or response content
- File paths or code snippets
- Personal identifiable information
- API keys or secrets

The data is used solely to understand usage patterns, identify bugs, and guide development priorities.

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
| `/history` | List recent conversations |
| `/search <query>` | Search conversations for a query |
| `/resume <id> [safe-count]` | Resume a conversation by ID (with optional safe message count for recovery) |
| `/fork <id> [msg-index]` | Fork a conversation at a specific message index |
| `/rollback <id> <count>` | Trim a conversation to a safe message count |
| `/rename <title>` | Rename the current conversation |
| `/reasoning [none/low/medium/high/xhigh/clear]` | Set or clear reasoning effort |
| `/turns [n]` | Get or set max agent turns |
| `/theme [name]` | Switch theme (dark/light/mono) or show current |
| `/stats` | Show session telemetry |
| `/help` | Show help |
| `/exit`, `/quit` | Exit BitCode |

### Skill Commands

Custom skills defined in `.agents/`, `.claude/`, or `.bitcode/` directories can be invoked as `/<skill-name>`. See the [Skills](#skills) section for details.

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
  session.go        # TUI session model and orchestration
  commands.go       # Slash command dispatcher
  themes.go         # Theme registry (dark/light/mono)
  lifecycle.go      # Session lifecycle management
  setup.go          # Tool manager, reminder manager, guard manager builders
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
  tools/            # Tool implementations (read, write, edit, glob, bash, todo, compact, skill, web_search)
  agent/            # Subagent framework (Agent tool, registry, runner)
  conversation/     # Conversation persistence (save/load, search, fork, rollback)
  telemetry/        # Usage metrics collection and storage
  notify/           # Desktop notifications
  config/           # Instruction file discovery (CLAUDE.md, AGENTS.md)
  version/          # Version information
docs/
  tool-guards.md    # Tool guard architecture and customization
  todo.md           # Todo system usage guide
  system-reminders.md  # System reminders architecture and plugin guide
```

## License

See [LICENSE](LICENSE) for the full license text.
