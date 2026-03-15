# BitCode

[![asciicast](https://asciinema.org/a/OEbplbhzYB1xU4bl.svg)](https://asciinema.org/a/OEbplbhzYB1xU4bl)

An AI coding assistant built in Go that uses LLMs to understand code and perform actions through tool calls. BitCode implements an agentic loop with multiple integrated tools.

## Features

- **Interactive Mode** — Full TUI with multiline input editor, bordered prompt, and keyboard shortcuts
- **Single-Shot Mode** — Run a single prompt from the command line with `-p`
- **Agent Loop** — Iterative LLM conversation with automatic tool calling (up to 50 turns)

### Tools

| Tool | Description |
|------|-------------|
| Read | Read files with optional line offset/limit |
| Write | Create or overwrite files |
| Edit | Surgical find-and-replace edits |
| Glob | Fast file pattern matching |
| Bash | Execute shell commands |
| TodoRead | Read current todo list |
| TodoWrite | Create or update todo list |
| Skill | Invoke user-defined prompt templates |

- **Tool Guards** — Safety layer that validates tool calls before execution (rules-based, user prompts, or LLM-powered)
- **Guard Agent** — Expert multi-turn LLM agent for security-aware tool validation with language-specific skills
- **System Reminders** — Dynamic context injection via `<system-reminder>` tags with [plugin support](docs/system-reminders.md)
- **Skills** — User-defined prompt templates loaded from `.agents/`, `.claude/`, or `.bitcode/` directories
- **Markdown Rendering** — Rich terminal output with syntax-highlighted code blocks
- **Reasoning Control** — Adjustable reasoning effort (`--reasoning` flag)
- **OpenRouter Integration** — Works with any OpenAI-compatible API (including local servers)

## Requirements

- Go 1.26+
- An OpenRouter API key (or any OpenAI-compatible endpoint; not required for localhost)

## Getting Started

### 1. Clone and configure

Create a `.env` file in the project root with your API key:

```
OPENROUTER_API_KEY=sk-or-v1-xxxxxxxxxxxx
```

Optionally set the model and base URL:

```
OPENROUTER_MODEL=anthropic/claude-sonnet-4-20250514
OPENROUTER_BASE_URL=https://openrouter.ai/api/v1
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
OPENROUTER_MODEL=anthropic/claude-sonnet-4-20250514 ./bitcode -p "Explain main.go"
```

**With a local server (no API key needed):**

```sh
OPENROUTER_BASE_URL=http://localhost:1234/v1 OPENROUTER_MODEL=local-model ./bitcode
```

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `OPENROUTER_API_KEY` | API key for OpenRouter (not required for localhost) | *(required for remote)* |
| `OPENROUTER_BASE_URL` | Base URL for the API | `https://openrouter.ai/api/v1` |
| `OPENROUTER_MODEL` | Model to use | `openrouter/free` |
| `BITCODE_GUARD_LLM` | Enable the LLM-powered guard agent | `true` |
| `BITCODE_GUARD_MODEL` | Model to use for guard agent | *(uses main model)* |

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

The guard agent is enabled by default. Set `BITCODE_GUARD_LLM=false` to use only rule-based guards.

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
    llm.go          # Provider interface, message types, content blocks
    openai.go       # OpenAI-compatible provider (sync + streaming)
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
