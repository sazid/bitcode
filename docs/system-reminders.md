# System Reminders

System reminders inject dynamic context into the LLM conversation without modifying the system prompt or polluting the stored conversation history. They use `<system-reminder>` XML tags appended to messages right before each API call.

## Architecture

```
                     Agent Loop (per turn)
                     ─────────────────────
                            │
                            ▼
               ┌────────────────────────┐
               │   Evaluate reminders   │
               │   (Manager.Evaluate)   │
               └───────────┬────────────┘
                           │ []Reminder
                           ▼
               ┌────────────────────────┐
               │   InjectReminders()    │
               │   shallow copy msgs,   │
               │   append <system-      │
               │   reminder> tags to    │
               │   last user/tool msg   │
               └───────────┬────────────┘
                           │ messagesForAPI (copy)
                           ▼
               ┌────────────────────────┐
               │   Provider.Complete()  │
               │   (sends copy to LLM)  │
               └───────────┬────────────┘
                           │
                           ▼
               ┌────────────────────────┐
               │   Append response to   │
               │   *messages (original) │
               │   — no reminders in    │
               │   stored history       │
               └────────────────────────┘
```

### Copy-on-inject pattern

The key design decision: reminders are injected into a **copy** of the message slice, not the original. The LLM sees them, but they are never stored in conversation history. This prevents reminders from accumulating across turns — each turn re-evaluates which reminders should fire based on the current state.

```go
messagesForAPI := *messages
if active := cfg.ReminderMgr.Evaluate(state); len(active) > 0 {
    messagesForAPI = reminder.InjectReminders(*messages, active)
}
// Send messagesForAPI to API, but append response to *messages
```

`InjectReminders` creates a shallow copy of the slice, finds the last `user` or `tool` message, deep-copies its content blocks, and appends the reminder text wrapped in `<system-reminder>` tags to the last text block.

## Core types

### Reminder

```go
type Reminder struct {
    ID       string
    Content  string   // text wrapped in <system-reminder> tags
    Schedule Schedule
    Source   string   // "builtin" or "plugin"
    Priority int      // higher = injected later (more LLM attention)
    Active   bool
}
```

**Priority** controls ordering within the injected text. Since LLMs attend more strongly to text near the end of the context, higher-priority reminders are placed last.

### Schedule

```go
type Schedule struct {
    Kind         ScheduleKind  // "always", "turn", "timer", "oneshot", "condition"
    TurnInterval int           // for turn-based: fire every N turns
    Interval     time.Duration // for timer-based: fire every N duration
    MaxFires     int           // 0 = unlimited
    Condition    ConditionFunc // for condition-based
}
```

### ConversationState

Passed to `Evaluate()` each turn. Provides read-only context for deciding which reminders fire:

```go
type ConversationState struct {
    Turn          int
    Messages      []llm.Message
    LastToolCalls []string
    ElapsedTime   time.Duration
}
```

## Schedule kinds

| Kind | Behavior | Use case |
|------|----------|----------|
| `always` | Fires every turn | Safety guardrails, coding conventions |
| `turn` | Fires every N turns (`TurnInterval`) | Periodic context refresh (e.g. datetime every 10 turns) |
| `timer` | Fires when `Interval` duration has elapsed since last fire | Time-based checks (e.g. CI status every 5 minutes) |
| `oneshot` | Fires once, then auto-deactivates | Initial context injection (e.g. skill availability) |
| `condition` | Fires when `ConditionFunc` returns true | Reactive reminders (e.g. "run tests" after file edits) |

All schedule kinds support `MaxFires` — when set to a non-zero value, the reminder deactivates after firing that many times, regardless of schedule kind.

## Manager

The `Manager` (`internal/reminder/manager.go`) is thread-safe (`sync.RWMutex`) and provides:

- **`Register(r Reminder)`** — Adds a reminder. If a reminder with the same ID already exists, it is replaced.
- **`Remove(id string)`** — Deactivates a reminder by ID.
- **`Evaluate(state *ConversationState) []Reminder`** — Returns reminders that should fire this turn, sorted by priority ascending. Updates fire counts and timestamps internally. Deactivates one-shot reminders after firing.

## Built-in reminders

Registered in `app/main.go`:

| ID | Schedule | Purpose |
|----|----------|---------|
| `skill-availability` | oneshot | Lists available skills on the first turn |
| `conversation-length` | condition (>80 messages), max 2 fires | Nudges the user to start a new conversation when the context grows long |

## Plugin system

Reminder plugins are loaded from disk at startup. Drop `.md`, `.yaml`, or `.yml` files into a `reminders/` subdirectory and BitCode picks them up automatically.

### Directory precedence

Directories are scanned in order of increasing precedence. Later entries with the same `id` overwrite earlier ones:

1. `~/.agents/reminders/` (lowest)
2. `~/.claude/reminders/`
3. `~/.bitcode/reminders/`
4. `.agents/reminders/` (project-level)
5. `.claude/reminders/`
6. `.bitcode/reminders/` (highest)

### File formats

**Markdown with YAML frontmatter:**

```markdown
---
id: testing-reminder
schedule:
  kind: condition
  condition: "after_tool:Edit,Write"
  max_fires: 3
priority: 2
---
After editing files, consider running the relevant tests to verify changes.
```

The body after the frontmatter becomes the reminder content.

**Pure YAML:**

```yaml
id: commit-nudge
content: |
  If significant changes have been made, suggest committing.
schedule:
  kind: turn
  turn_interval: 15
priority: 1
```

### Frontmatter fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | derived from filename | Unique identifier |
| `content` | string | (markdown body) | Reminder text (YAML files only; markdown files use the body) |
| `schedule.kind` | string | `oneshot` | Schedule kind: `always`, `turn`, `timer`, `oneshot`, `condition` |
| `schedule.turn_interval` | int | 1 | For `turn` kind: fire every N turns |
| `schedule.interval` | string | `5m` | For `timer` kind: Go duration string (e.g. `30s`, `5m`) |
| `schedule.max_fires` | int | 0 (unlimited) | Deactivate after N fires |
| `schedule.condition` | string | — | For `condition` kind: condition expression |
| `priority` | int | 0 | Higher = injected later (more LLM attention) |

### Condition expressions

Simple string-based conditions for plugin files (programmatic reminders can use arbitrary Go functions):

| Expression | Behavior |
|------------|----------|
| `always` | Always true |
| `""` (empty) | Always true |
| `after_tool:Edit` | True when `Edit` was used in the previous turn |
| `after_tool:Edit,Write` | True when `Edit` OR `Write` was used |
| `turn_gt:20` | True when the turn count exceeds 20 |

Unknown condition strings never fire (safe default).

## Example plugins

### Project conventions (oneshot)

```markdown
---
id: project-conventions
schedule:
  kind: oneshot
priority: 5
---
This project uses:
- Conventional commits (feat:, fix:, chore:)
- Table-driven tests in Go
- No global state
```

### Safety guardrails (always, high priority)

```markdown
---
id: destructive-ops
schedule:
  kind: always
priority: 10
---
Never run destructive commands (rm -rf, DROP TABLE, git push --force)
without explicit user confirmation.
```

### Test nudge (condition-based)

```yaml
id: run-tests
content: |
  Files were just edited. Consider running tests to verify the changes.
schedule:
  kind: condition
  condition: "after_tool:Edit,Write"
  max_fires: 5
priority: 3
```

### Periodic datetime refresh

```yaml
id: datetime
content: "Current date and time: {{now}}"
schedule:
  kind: turn
  turn_interval: 10
priority: 0
```

## Code layout

```
internal/reminder/
  reminder.go      # Core types (Reminder, Schedule, ConversationState)
  manager.go       # Manager — register, evaluate, fire tracking
  inject.go        # InjectReminders() — copy-on-inject into messages
  plugins.go       # Plugin loading from disk, condition string parser
  reminder_test.go # Tests (schedule evaluation, injection, plugins, conditions)
```

Integration points in `app/`:
- `agent.go` — Evaluates reminders and injects into message copy before each LLM call
- `main.go` — Creates the Manager, registers built-in reminders, loads plugins
- `system_prompt.go` — Instructs the LLM to treat `<system-reminder>` tags as system-level context
