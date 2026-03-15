# Todo System

BitCode includes a built-in task management system that helps the agent track progress on multi-step tasks. It's accessible via two tools: `TodoWrite` and `TodoRead`.

## Overview

The todo system allows the agent to:

- **Plan tasks** — Create a structured task list before starting work
- **Track progress** — Mark tasks as in-progress or completed
- **Prioritize** — Assign priority levels (high, medium, low)
- **Resume work** — Use the todo list to continue from where left off

This is particularly useful for:
- Complex refactoring tasks with multiple steps
- Bug fixes that require understanding several files first
- Adding new features that touch multiple components

## Tools

### TodoWrite

Creates or replaces the entire todo list for the current session.

**Parameters:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `todos` | array | Yes | Full replacement list of todo items |

**Todo Item Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique identifier for the todo item |
| `content` | string | Yes | Description of the task |
| `status` | string | Yes | Current status: `pending`, `in_progress`, or `completed` |
| `priority` | string | Yes | Priority level: `high`, `medium`, or `low` |

**Example:**

```json
{
  "todos": [
    {
      "id": "1",
      "content": "Write implementation plan to .bitcode/PLAN.md",
      "status": "in_progress",
      "priority": "high"
    },
    {
      "id": "2",
      "content": "Implement the feature",
      "status": "pending",
      "priority": "high"
    },
    {
      "id": "3",
      "content": "Write tests",
      "status": "pending",
      "priority": "medium"
    }
  ]
}
```

**Important:** Each call to `TodoWrite` **replaces** the entire todo list. To update a single item, include all items in your write.

### TodoRead

Reads the current todo list.

**Parameters:** None (empty object)

**Returns:** JSON array of all todo items, or `"No todos"` if the list is empty.

**Example Response:**

```json
[
  {
    "id": "1",
    "content": "Write implementation plan to .bitcode/PLAN.md",
    "status": "completed",
    "priority": "high"
  },
  {
    "id": "2",
    "content": "Implement the feature",
    "status": "in_progress",
    "priority": "high"
  }
]
```

## Usage Patterns

### Planning a Multi-Step Task

For any non-trivial task, the first step should be creating a todo list:

```
User: Refactor the authentication system to use JWT tokens

Agent calls TodoWrite:
{
  "todos": [
    {"id": "1", "content": "Analyze current auth implementation", "status": "in_progress", "priority": "high"},
    {"id": "2", "content": "Design JWT token structure", "status": "pending", "priority": "high"},
    {"id": "3", "content": "Implement token generation", "status": "pending", "priority": "high"},
    {"id": "4", "content": "Implement token validation middleware", "status": "pending", "priority": "high"},
    {"id": "5", "content": "Update existing endpoints", "status": "pending", "priority": "medium"},
    {"id": "6", "content": "Run tests and verify", "status": "pending", "priority": "high"}
  ]
}
```

### Tracking Progress

As the agent completes each step, it updates the todo list:

```
Agent completes step 1, calls TodoWrite:
{
  "todos": [
    {"id": "1", "content": "Analyze current auth implementation", "status": "completed", "priority": "high"},
    {"id": "2", "content": "Design JWT token structure", "status": "in_progress", "priority": "high"},
    ...
  ]
}
```

### Adding New Tasks Mid-Work

When the agent discovers additional work needed:

```
Agent discovers the password hashing needs updating, calls TodoWrite:
{
  "todos": [
    {"id": "1", "content": "Analyze current auth implementation", "status": "completed", "priority": "high"},
    {"id": "2", "content": "Design JWT token structure", "status": "completed", "priority": "high"},
    {"id": "3a", "content": "Update password hashing to bcrypt", "status": "in_progress", "priority": "high"},
    {"id": "4", "content": "Implement token generation", "status": "pending", "priority": "high"},
    ...
  ]
}
```

## Best Practices

1. **Always start with a plan** — For non-trivial tasks, create a todo list first. This helps the agent stay focused and ensures nothing is missed.

2. **Use descriptive content** — Write clear, actionable task descriptions.

3. **Set priorities** — Use `high` for critical path items, `medium` for important but not blocking, `low` for nice-to-have.

4. **Only one in_progress** — Mark only one item as `in_progress` at a time to maintain clarity.

5. **Update frequently** — Mark items as completed immediately after finishing them.

6. **Check the list** — Use `TodoRead` before continuing work to ensure you're on track.

## Persistence

The todo list is stored in memory for the current session only. It does not persist between restarts. This is by design — each session should start fresh with a clear plan.

If you need to persist tasks across sessions, create a file (e.g., `.bitcode/PLAN.md`) as your first todo item suggests:

> For any non-trivial task, the FIRST todo should be:
> "Write implementation plan to .bitcode/PLAN.md"
> This allows resuming work across sessions.

## Terminal Display

The todo list is displayed in the terminal with visual indicators:

| Icon | Meaning |
|------|---------|
| `[✓]` | Completed task |
| `[~]` | In-progress task |
| `[ ]` | Pending task |

Each item also shows its priority in parentheses: `(high)`, `(medium)`, or `(low)`.

Example:
```
[✓] Write implementation plan (high)
[~] Implement the feature (high)
[ ] Write tests (medium)
```

## Code Reference

The todo system is implemented in `internal/tools/todo.go`:

- `TodoItem` — Individual task struct
- `TodoStore` — Interface for persistence (default: `MemoryTodoStore`)
- `TodoWriteTool` — Tool implementation for writing
- `TodoReadTool` — Tool implementation for reading
