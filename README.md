# BitCode

An AI coding assistant built in Go that uses LLMs to understand code and perform actions through tool calls. BitCode implements an agentic loop with multiple integrated tools.

## Features

- **Agent Loop** — Iterative LLM conversation with automatic tool calling (up to 50 turns)
- **Read Tool** — Read files with optional line offset/limit
- **Write Tool** — Create or overwrite files
- **Edit Tool** — Surgical find-and-replace edits
- **Glob Tool** — Fast file pattern matching
- **Bash Tool** — Execute shell commands
- **Markdown Rendering** — Rich terminal output with syntax-highlighted code snippets
- **OpenRouter Integration** — Works with any OpenAI-compatible API

## Requirements

- Go 1.26+
- An OpenRouter API key (or any OpenAI-compatible endpoint)

## Usage

```sh
# Set up environment
cp .env.example .env
# Edit .env with your API key

# Build and run
go build -o bitcode ./app
./bitcode -p "Your prompt here"
```

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `OPENROUTER_API_KEY` | API key for OpenRouter | *(required)* |
| `OPENROUTER_BASE_URL` | Base URL for the API | `https://openrouter.ai/api/v1` |
| `OPENROUTER_MODEL` | Model to use | `anthropic/claude-haiku-4.5` |

## Project Structure

```
app/
  main.go           # Entry point and agent loop
  conversation.go   # Conversation/message management
  render.go         # Terminal rendering (markdown, events)
  system_prompt.go  # System prompt construction
internal/
  event.go          # Event types for tool output
  tools/            # Tool implementations (read, write, edit, glob, bash)
```

## License

MIT
