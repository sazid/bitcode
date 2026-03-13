# Tool Guard System

## Context

BitCode's agent loop executes tool calls immediately — there's no safety layer between the LLM deciding to call a tool and actual execution. The Bash tool allows arbitrary shell commands including system modification and operations outside the working directory. File tools (Write, Edit) can overwrite sensitive files. The user wants a guard system that:

1. Enforces working directory boundaries — operations outside cwd require explicit permission
2. Catches dangerous/destructive commands before they run
3. Optionally calls a fast/cheap LLM to validate ambiguous cases
4. Prompts the user for approval when a guard flags something

## Architecture

```
                    Agent Loop (tool call dispatch)
                    ──────────────────────────────
                           │ tc.Name, tc.Arguments
                           ▼
                ┌──────────────────────┐
                │   Guard Manager      │
                │   (Evaluate)         │
                │                      │
                │   ┌───────────────┐  │
                │   │ Rule 1: Deny  │──┤── VerdictDeny → return error to LLM
                │   │ Rule 2: Ask   │──┤── VerdictAsk  → prompt user
                │   │ Rule 3: LLM   │──┤── VerdictLLM  → call LLM guard
                │   │ ...           │  │── VerdictAllow → proceed
                │   └───────────────┘  │
                └──────────┬───────────┘
                           │ Decision
                           ▼
                ┌──────────────────────┐
                │  Denied? → error msg │
                │  Ask? → user prompt  │
                │  Allow? → execute    │
                └──────────────────────┘
```

The guard sits at a single point in `app/agent.go:108`, before `cfg.ToolManager.ExecuteTool()`. The `Tool` interface is untouched.

---

## Phase 1: Core Types — `internal/guard/guard.go`

```go
type Verdict string
const (
    VerdictAllow Verdict = "allow"  // safe, proceed
    VerdictDeny  Verdict = "deny"   // blocked, return error to LLM
    VerdictAsk   Verdict = "ask"    // ask user for approval
    VerdictLLM   Verdict = "llm"    // escalate to LLM guard
)

type Decision struct {
    Verdict Verdict
    Reason  string // human-readable explanation
}

type EvalContext struct {
    ToolName   string
    Input      json.RawMessage
    WorkingDir string
}

type Rule interface {
    Evaluate(ctx *EvalContext) *Decision // nil = abstain
}

// Called when verdict is Ask — blocks until user responds
type PermissionHandler func(toolName string, decision Decision) bool

// Optional LLM-based validation
type LLMValidator interface {
    Validate(ctx context.Context, evalCtx *EvalContext) (*Decision, error)
}
```

## Phase 2: Manager — `internal/guard/manager.go`

```go
type Manager struct {
    rules           []Rule
    llmValidator    LLMValidator       // nil = disabled
    permHandler     PermissionHandler  // nil = auto-deny
    sessionApproved map[string]bool    // "Bash:git push" → true
    mu              sync.RWMutex
}
```

`Evaluate(ctx context.Context, toolName, input string) (*Decision, error)`:

1. Parse input JSON, build `EvalContext` with `os.Getwd()`
2. Run rules in order — first non-nil `Decision` wins
3. If no rule fires: `VerdictAllow` for read-only tools (Read, Glob, Skill), `VerdictAsk` for write tools (Bash, Write, Edit)
4. `VerdictLLM` → call `llmValidator` if set, else fall back to `VerdictAsk`
5. `VerdictAsk` → check `sessionApproved` cache; if miss, call `permHandler`; user approval gets cached
6. `VerdictDeny` → return immediately

## Phase 3: Built-in Rules — `internal/guard/rules.go`

### WorkingDirRule

**File tools (Read, Write, Edit, Glob):** Parse `file_path`/`path` from JSON, resolve to absolute path via `filepath.Abs` + `filepath.Clean`, check if it has cwd as prefix. Inside cwd → `nil` (abstain). Outside cwd → `VerdictAsk` with reason.

**Bash:** Extract absolute paths from command string using regex `/[^\s;|&>"']+`. For each path outside cwd:
- With write-oriented commands (`rm`, `mv`, `cp`, `chmod`, `mkdir`, `rmdir`, `tee`, `dd`) → `VerdictAsk`
- With read-only commands (`cat`, `ls`, `grep`, `head`, `stat`) → `nil` (reading outside cwd is usually fine)

### DangerousCommandRule (Bash only)

**Deny list** (always blocked):
- `rm -rf /`, `rm -rf ~`, `rm -rf $HOME`
- `mkfs`, `dd if=... of=/dev/...`
- Fork bombs
- `chmod -R 777 /`

**Ask list** (user must approve):
- `sudo` anything
- `curl|sh`, `wget|sh` (pipe-to-shell)
- `git push --force`, `git reset --hard`
- `npm publish`, `cargo publish`, `pip upload`
- Network access commands (`curl`, `wget`, `ssh`, `scp`) to external hosts
- `docker run`, `docker exec`

### SensitiveFileRule (Write, Edit only)

Files that require approval before modification:
- `.env`, `.env.*`
- `*credentials*`, `*secret*`, `*.pem`, `*.key`
- `.git/config`, `.ssh/*`

### DefaultPolicyRule

Provides baseline when no other rule fires:
- **Skill**: always `VerdictAllow`
- **Read, Glob**: `VerdictAllow`
- **Write, Edit**: `VerdictAllow` (WorkingDirRule and SensitiveFileRule already handle risky cases)
- **Bash**: check against an allowlist of known-safe patterns. If matched → `VerdictAllow`. Otherwise → `VerdictLLM` (or `VerdictAsk` if LLM guard is disabled)

Known-safe Bash patterns (skip guard):
- `echo`, `pwd`, `which`, `env`, `printenv`
- `ls`, `cat`, `head`, `tail`, `wc`, `sort`, `uniq`, `diff` (in cwd)
- `git status`, `git log`, `git diff`, `git branch`, `git show`, `git stash`
- `go build`, `go test`, `go run`, `go vet`, `go fmt`, `go mod tidy`
- `npm test`, `npm run`, `npm ci`, `npm install`
- `cargo build`, `cargo test`, `cargo check`
- `make`, `cmake`
- `grep`, `rg`, `ag`, `fd`, `find` (in cwd)

## Phase 4: LLM Guard — `internal/guard/llm_guard.go`

```go
type LLMGuard struct {
    provider llm.Provider
    model    string
}
```

Sends a single completion with a short system prompt:
```
You are a security evaluator for a CLI coding agent working in: {cwd}

Evaluate this tool call:
Tool: {toolName}
Input: {sanitized input}

Respond with exactly one line:
ALLOW
DENY: <reason>
ASK: <reason>

Consider: working directory boundaries, system damage risk, data exfiltration, common dev operations.
```

Configuration via env vars:
- `BITCODE_GUARD_LLM=true` — enable
- `BITCODE_GUARD_LLM_MODEL` — model (default: main model)
- `BITCODE_GUARD_LLM_BASE_URL` — endpoint (default: main endpoint)
- `BITCODE_GUARD_LLM_API_KEY` — API key (default: main key)

## Phase 5: User Permission Prompt — `internal/guard/prompt.go`

A simple confirmation that works within the agent loop. Since the agent loop blocks `runInteractive()`/`runSingleShot()` synchronously, we can:

1. Stop the spinner (via the `OnThinking(false)` callback)
2. Print the guard warning to stderr
3. Read a single keypress (y/n/a) via a minimal bubbletea program (same pattern as `readInput()` in `app/input.go`)
4. Resume spinner

Render:
```
⚠ Guard: Bash command accesses path outside working directory
  $ rm -rf /tmp/old-builds
  Reason: /tmp/old-builds is outside /Users/sazid/workspace/personal/bitcode

  [y] Allow once  [a] Always allow  [n] Deny
```

"Always allow" caches the pattern in `sessionApproved` for the process lifetime.

For non-interactive (`-p` flag): auto-deny and return an error message to the LLM.

The `PermissionHandler` needs to pause the spinner before prompting. Pass a `pauseThinking`/`resumeThinking` pair of callbacks from `app/main.go` into the handler, or add `OnGuardPrompt` to `AgentCallbacks` that the agent loop calls to pause/resume around the prompt.

## Phase 6: Plugin Rules — `internal/guard/plugins.go`

Files in `{.agents,.claude,.bitcode}/guards/` directories. Same precedence as reminders/skills.

```yaml
# .bitcode/guards/block-docker.yaml
id: block-docker
tool: Bash
patterns:
  - match: "docker"
    verdict: ask
    reason: "Docker commands require approval"
```

```markdown
---
id: protect-env
tool: Write,Edit
---
patterns:
  - file_match: ".env*"
    verdict: ask
    reason: "Modifying environment configuration"
```

`LoadPlugins() []Rule` scans directories, parses files, returns `PluginRule` instances following the same pattern as `internal/reminder/plugins.go`.

## Integration Points

### `app/agent.go` — line 108

Add `GuardMgr *guard.Manager` to `AgentConfig`. Before `ExecuteTool`:

```go
// Guard check
if cfg.GuardMgr != nil {
    decision, err := cfg.GuardMgr.Evaluate(ctx, tc.Name, tc.Arguments)
    if err != nil {
        content = fmt.Sprintf("Guard error: %v", err)
        // append tool result, continue
    }
    if decision != nil && decision.Verdict == guard.VerdictDeny {
        eventsCh <- internal.Event{
            Name:    "Guard",
            Args:    []string{tc.Name},
            Message: fmt.Sprintf("Blocked: %s", decision.Reason),
            IsError: true,
        }
        content = fmt.Sprintf("Operation blocked by safety guard: %s", decision.Reason)
        // append tool result, continue
    }
}
result, err := cfg.ToolManager.ExecuteTool(tc.Name, tc.Arguments, eventsCh)
```

### `app/main.go`

After tool registration, before building config:
- Create `guard.NewManager()`
- Register built-in rules: `DangerousCommandRule`, `WorkingDirRule`, `SensitiveFileRule`, `DefaultPolicyRule`
- Load plugin rules via `guard.LoadPlugins()`
- Optionally configure `LLMGuard` from env vars
- Set `PermissionHandler` (terminal prompt for interactive, auto-deny for `-p`)
- Add `GuardMgr` to `AgentConfig`

### `app/render.go`

Guard event rendering — yellow/amber warning bullet with tool name and reason.

### `app/system_prompt.go`

Add section telling the LLM about guards:
```
# Safety Guards
Tool calls are subject to safety guards. If a tool call is blocked, you will receive
an error explaining why. Do not retry blocked operations. Instead, explain to the user
what you wanted to do and suggest alternatives.
```

### `internal/event.go`

Add `PreviewGuard PreviewType = "guard"`.

## Files Summary

| File | Action | Purpose |
|------|--------|---------|
| `internal/guard/guard.go` | Create | Core types |
| `internal/guard/manager.go` | Create | Rule evaluation, approval caching, escalation |
| `internal/guard/rules.go` | Create | 4 built-in rules |
| `internal/guard/llm_guard.go` | Create | Optional LLM validator |
| `internal/guard/prompt.go` | Create | `TerminalPermissionHandler`, `AutoDenyHandler` |
| `internal/guard/plugins.go` | Create | Plugin loader from `guards/` directories |
| `internal/guard/guard_test.go` | Create | Tests for rules, manager, plugins |
| `app/agent.go` | Modify | Add `GuardMgr` to config, guard check before `ExecuteTool` |
| `app/main.go` | Modify | Wire guard manager, register rules, load plugins |
| `app/render.go` | Modify | Guard event rendering |
| `app/system_prompt.go` | Modify | Add safety guard instructions |
| `internal/event.go` | Modify | Add `PreviewGuard` constant |

## Implementation Order

1. `internal/guard/guard.go` — types
2. `internal/guard/manager.go` — evaluation logic
3. `internal/guard/rules.go` — built-in rules (most code)
4. `internal/guard/prompt.go` — permission handlers
5. `app/agent.go` + `app/main.go` — integration (system becomes functional)
6. `app/render.go` + `internal/event.go` — rendering
7. `app/system_prompt.go` — LLM instructions
8. `internal/guard/llm_guard.go` — optional LLM guard
9. `internal/guard/plugins.go` — plugin loading
10. `internal/guard/guard_test.go` — tests

## Verification

1. **Unit tests**: All rule types with known-safe and known-dangerous inputs, manager evaluation flow, plugin loading
2. **Manual — working dir enforcement**: Run BitCode, ask it to `rm /tmp/something` — should prompt
3. **Manual — dangerous command**: Ask it to `rm -rf /` — should auto-deny
4. **Manual — safe commands**: Ask it to `go test ./...` — should proceed without prompting
5. **Manual — sensitive files**: Ask it to edit `.env` — should prompt
6. **Manual — non-interactive**: Run with `-p "delete /tmp/foo"` — auto-deny
7. **Build**: `go build ./...` && `go test ./...` pass
