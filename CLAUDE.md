# CLAUDE.md

This file provides guidance to AI Agents when working with code in this repository.

## Build & Run

```bash
go build -o bitcode ./app        # Build
go test ./...                     # Run all tests
go test ./internal/tools/...      # Run tests for a single package
go test -run TestEditTool ./internal/tools/  # Run a single test
go vet ./...                      # Lint
```

Run interactively: `./bitcode`
Single-shot: `./bitcode -p "prompt"`
With reasoning: `./bitcode --reasoning high -p "prompt"`

## Environment

Configured via `.env` file or environment variables:
- `OPENROUTER_API_KEY` — API key (not needed for localhost)
- `OPENROUTER_MODEL` — model name (default: `openrouter/free`)
- `OPENROUTER_BASE_URL` — API endpoint (default: `https://openrouter.ai/api/v1`)
- `BITCODE_GUARD_LLM` — enable LLM guard agent (default: `true`)
- `BITCODE_GUARD_MODEL` — separate model for guard agent (optional)

## Architecture

BitCode is an agentic AI coding assistant CLI built in Go. It uses an OpenAI-compatible API via OpenRouter to power an iterative agent loop with tool calling.

### Core Loop

`app/agent.go:runAgentLoop` drives the main cycle: send messages to LLM → receive tool calls → evaluate guards → execute tools → append results → repeat (up to 200 turns). Two modes: interactive TUI (`runInteractive`) and single-shot (`runSingleShot`), both dispatched from `app/main.go`.

### Key Packages

- **`app/`** — Entry point, agent loop, TUI input (bubbletea), markdown rendering (glamour), system prompt construction. All files are in package `main`.
- **`internal/llm/`** — `Provider` interface and OpenAI-compatible implementation (sync + streaming). All LLM communication flows through this abstraction.
- **`internal/tools/`** — `Tool` interface + `Manager` registry. Each tool (Read, Write, Edit, Glob, Bash, Skill, TodoRead, TodoWrite) is a separate file. Tools return `ToolResult` and emit `Event`s via a channel.
- **`internal/guard/`** — Safety layer between LLM decisions and tool execution. Evaluates rules in order (first non-nil `Decision` wins), with verdict escalation: Allow → Deny → Ask user → LLM guard agent. Session approval caching prevents re-prompting. Built-in rules in `rules.go`; LLM-powered guard agent in `guard_agent.go` with language-specific security skills in `guard/skills/`.
- **`internal/reminder/`** — Injects `<system-reminder>` tags into messages before API calls using copy-on-inject (stored history stays clean). Schedule kinds: always, turn, timer, oneshot, condition. Supports plugin loading from `reminders/` subdirectories.
- **`internal/skills/`** — Discovers and loads user-defined prompt templates (YAML frontmatter + Markdown) from `.agents/`, `.claude/`, `.bitcode/` in both home and project directories. Project-level takes precedence.
- **`internal/notify/`** — Cross-platform desktop notifications.
- **`internal/event.go`** — Event types for tool output previews flowing from tools → agent loop → UI.

### Extension Points

Skills, reminders, and guard rules can all be extended via plugin files dropped into `.bitcode/`, `.claude/`, or `.agents/` directories (both `~/` and project root). See `docs/` for plugin formats.

## Release

CI builds static binaries (`CGO_ENABLED=0`) for linux/darwin × amd64/arm64 on version tag pushes. Version info injected via ldflags (`internal/version/`).
