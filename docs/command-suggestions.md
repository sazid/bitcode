# Command Suggestions

Command suggestions provide inline autocomplete for slash commands as the user types in the input textarea. When the input starts with `/`, a filtered list of matching commands and skills appears below the input box, updating in real-time with each keystroke.

## Architecture

```
╭──────────────────────────────────╮
│ /he                              │  ← user types here
╰──────────────────────────────────╯
  /help       Show available commands     ← highlighted (selected)
  /reasoning  Set reasoning effort
  ctrl+s submit · esc clear · ctrl+d exit
```

The autocomplete system lives entirely in `app/input.go` within the bubbletea `inputModel`. It uses no external dependencies beyond what the input model already imports.

## How It Works

### Command List

A unified `SlashCommand` type represents both built-in commands and dynamic skills:

```go
type SlashCommand struct {
    Name        string // e.g. "exit", "help", "commit", "git:status"
    Description string // e.g. "Exit BitCode"
    Source      string // "builtin", "project", "user"
}
```

The command list is built once in `runInteractive()` (`app/main.go`) by combining:

1. **Built-in commands**: `/new`, `/reasoning`, `/turns`, `/help`, `/exit`, `/quit`
2. **Dynamic skills**: loaded from `config.SkillManager.List()` — these come from `.agents/skills/`, `.claude/skills/`, and `.bitcode/skills/` directories

The combined list is passed to `readInput()` → `newInputModel()` and stored on the `inputModel`.

### Filtering

On every keystroke, `updateSuggestions()` runs after the textarea processes the key. Suggestions activate when all three conditions are met:

- Input starts with `/`
- Input is a single line (no newlines)
- Input has no spaces (still typing the command name, not arguments)

Matching uses `strings.Contains` (case-insensitive), so typing `/sta` matches `git:status`. Results are sorted with prefix matches first, then alphabetically.

### Key Bindings

When the suggestion popup is visible:

| Key       | Action                                           |
|-----------|--------------------------------------------------|
| Up/Down   | Navigate the suggestion list (wraps around)      |
| Tab       | Accept the selected suggestion                   |
| Escape    | Dismiss suggestions (does not clear input)       |

When suggestions are not visible, all keys behave as normal (Escape clears input, Up/Down navigate textarea lines, etc.).

### Rendering

Suggestions render between the textarea border and the hint text. The selected item gets a subtle background highlight. Non-builtin commands show a source tag (e.g. `[project]`). The list is capped at 8 visible items, with a "... and N more" indicator when truncated.

## Key Files

| File | Role |
|------|------|
| `app/input.go` | All autocomplete logic: state, filtering, key interception, rendering |
| `app/main.go` | Builds the `[]SlashCommand` list from builtins + skills, passes to `readInput()` |
| `internal/skills/skills.go` | `Manager.List()` provides dynamic skill entries |

## Adding a New Built-in Command

When adding a new built-in command to the switch statement in `runInteractive()`, also add a corresponding entry to the `slashCommands` slice in the same function:

```go
slashCommands := []SlashCommand{
    // ... existing entries ...
    {Name: "mycommand", Description: "Does something useful", Source: "builtin"},
}
```

Skills are picked up automatically — no changes needed when adding new skill files.
