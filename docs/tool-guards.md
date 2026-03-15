# Tool Guards

Tool guards are a safety layer between the LLM deciding to call a tool and the actual execution. Every tool call passes through the guard system before reaching the tool implementation. Guards can allow, deny, escalate to an LLM validator, or prompt the user for approval.

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

The guard sits at a single point in `app/agent.go`, inside the tool call loop, **before** `cfg.ToolManager.ExecuteTool()`. The `Tool` interface is untouched — guards are completely transparent to tool implementations.

### Intercept point

```go
// app/agent.go — inside the tool call loop
if cfg.GuardMgr != nil {
    decision, guardErr := cfg.GuardMgr.Evaluate(ctx, tc.Name, tc.Arguments)
    if guardErr != nil {
        // send error as tool result, continue to next tool call
    }
    if decision != nil && decision.Verdict == guard.VerdictDeny {
        // send blocked message as tool result, continue to next tool call
    }
}
// Only reaches here if guard allowed (or no guard configured)
result, err := cfg.ToolManager.ExecuteTool(tc.Name, tc.Arguments, eventsCh)
```

When a tool call is blocked, the LLM receives a tool result explaining the block (e.g. `"Operation blocked by safety guard: dangerous command blocked: rm -rf /"`). This allows the LLM to explain the situation to the user and suggest alternatives, rather than silently failing.

## Core types

All types live in `internal/guard/guard.go`.

### Verdict

```go
type Verdict string

const (
    VerdictAllow Verdict = "allow"  // safe, proceed with execution
    VerdictDeny  Verdict = "deny"   // blocked, return error to LLM
    VerdictAsk   Verdict = "ask"    // prompt the user for approval
    VerdictLLM   Verdict = "llm"    // escalate to LLM-based validator
)
```

### Decision

```go
type Decision struct {
    Verdict Verdict
    Reason  string // human-readable explanation shown to user/LLM
}
```

### EvalContext

```go
type EvalContext struct {
    ToolName   string          // e.g. "Bash", "Write", "Read"
    Input      json.RawMessage // raw JSON arguments from the LLM
    WorkingDir string          // current working directory (os.Getwd())
}
```

### Rule

```go
type Rule interface {
    Evaluate(ctx *EvalContext) *Decision // nil = abstain (no opinion)
}
```

Rules return `nil` to abstain — indicating they have no opinion on this tool call. This is distinct from returning `VerdictAllow`, which is a positive assertion that the call is safe.

### PermissionHandler

```go
type PermissionHandler func(toolName string, decision Decision) bool
```

Called when the final verdict is `VerdictAsk`. Blocks until the user responds. Returns `true` if approved.

## Manager

The `Manager` (`internal/guard/manager.go`) orchestrates rule evaluation, verdict escalation, session caching, and user prompting.

```go
type Manager struct {
    rules           []Rule
    llmValidator    LLMValidator       // nil = disabled
    permHandler     PermissionHandler  // nil = auto-deny
    sessionApproved map[string]bool    // "Bash:reason" → true
    mu              sync.RWMutex
}
```

### Evaluation flow

`Evaluate(ctx context.Context, toolName, input string) (*Decision, error)`:

1. **Build context** — Parse `input` as `json.RawMessage`, get cwd via `os.Getwd()`, construct `EvalContext`.

2. **Run rules in order** — First non-nil `Decision` wins. If no rule fires, return `VerdictAllow`.

3. **Handle verdict**:
   - `VerdictAllow` → return immediately (tool executes)
   - `VerdictDeny` → return immediately (tool blocked)
   - `VerdictLLM` → call `llmValidator.Validate()` if configured; on error or if unconfigured, fall back to `VerdictAsk`
   - `VerdictAsk` → check session cache → if miss, call `permHandler` → if no handler, auto-deny

4. **Session caching** — When a user approves a prompt, the approval is cached using the key `"toolName:reason"` for the process lifetime. Subsequent identical prompts are auto-approved without re-prompting.

### Rule ordering matters

Rules are evaluated in registration order. The first non-nil decision wins. This means:

- **DangerousCommandRule** runs first to catch catastrophic commands before any other logic
- **WorkingDirRule** runs second to enforce directory boundaries
- **SensitiveFileRule** runs third to protect sensitive files
- **Plugin rules** run next (project-specific overrides)
- **DefaultPolicyRule** runs last as the catch-all baseline

```go
// app/main.go — registration order
guardMgr.AddRule(&guard.DangerousCommandRule{})  // highest priority
guardMgr.AddRule(&guard.WorkingDirRule{})
guardMgr.AddRule(&guard.SensitiveFileRule{})
for _, r := range guard.LoadPlugins() {           // project-specific
    guardMgr.AddRule(r)
}
guardMgr.AddRule(&guard.DefaultPolicyRule{})       // catch-all (lowest)
```

## Built-in rules

All built-in rules live in `internal/guard/rules.go`.

### DangerousCommandRule

**Applies to:** `Bash` tool only.

Matches command strings against two sets of patterns:

**Deny list** (always blocked, no user override):

| Pattern | What it catches |
|---------|----------------|
| `rm -rf /`, `rm -rf ~`, `rm -rf $HOME` | Recursive deletion of root/home |
| `mkfs` | Filesystem formatting |
| `dd if=... of=/dev/...` | Raw device writes |
| `:(){ ...\|... };:` | Fork bombs |
| `chmod -R 777 /` | Recursive permission change on root |

**Ask list** (user must approve):

| Pattern | Reason |
|---------|--------|
| `sudo` | Privilege escalation |
| `curl\|sh`, `wget\|sh` | Pipe-to-shell |
| `git push --force`, `git push -f` | Force push |
| `git reset --hard` | Hard reset |
| `npm publish`, `cargo publish` | Package publishing |
| `docker run`, `docker exec` | Container execution |

Commands not matching either list cause the rule to abstain (`nil`), deferring to later rules.

### WorkingDirRule

**Applies to:** `Read`, `Write`, `Edit`, `Glob` (file path check), `Bash` (path extraction).

**File tools:** Parses `file_path` or `path` from the JSON input, resolves to an absolute path via `filepath.Abs` + `filepath.Clean`, and checks whether it falls inside the working directory. Files inside cwd → abstain. Files outside cwd → `VerdictAsk`.

**Bash:** Extracts absolute paths from the command string using the regex `/[^\s;|&>"']+`. For each path outside cwd, checks if the command is write-oriented (`rm`, `mv`, `cp`, `chmod`, `mkdir`, `rmdir`, `tee`, `dd`, `chown`, `touch`). Write commands outside cwd → `VerdictAsk`. Read-only commands outside cwd → abstain (reading outside cwd is usually harmless).

### SensitiveFileRule

**Applies to:** `Write` and `Edit` only (reading sensitive files is allowed).

Checks the file basename against glob patterns:

| Pattern | Files matched |
|---------|--------------|
| `.env`, `.env.*` | Environment configuration |
| `*credentials*` | Credential files |
| `*secret*` | Secret files |
| `*.pem`, `*.key` | Cryptographic keys |
| `.git/config` | Git configuration |
| `.ssh/*` | SSH configuration |

Match → `VerdictAsk`. No match → abstain.

### DefaultPolicyRule

**Applies to:** All tools. Always returns a non-nil decision (never abstains). This is the catch-all rule and must be registered last.

| Tool | Verdict | Rationale |
|------|---------|-----------|
| `Skill`, `Read`, `Glob` | `VerdictAllow` | Read-only, no side effects |
| `Write`, `Edit` | `VerdictAllow` | Earlier rules already catch sensitive files and out-of-cwd writes |
| `Bash` (known-safe) | `VerdictAllow` | Matches against a safe-list |
| `Bash` (unknown) | `VerdictLLM` | Escalates to LLM guard (falls back to `VerdictAsk` if LLM guard is disabled) |
| Other tools | `VerdictAllow` | Unknown tools default to allow |

**Known-safe Bash patterns** (commands that skip the guard):

- Shell builtins: `echo`, `pwd`, `which`, `env`, `printenv`
- Read-only: `ls`, `cat`, `head`, `tail`, `wc`, `sort`, `uniq`, `diff`
- Git read-only: `git status`, `git log`, `git diff`, `git branch`, `git show`, `git stash`
- Build/test: `go build/test/run/vet/fmt`, `npm test/run/ci/install`, `cargo build/test/check`, `make`, `cmake`
- Search: `grep`, `rg`, `ag`, `fd`, `find`

Matching is prefix-based — `go test ./...` matches because it starts with `go test`.

## LLM Guard

Optional LLM-based validator (`internal/guard/llm_guard.go`) for ambiguous commands that don't match any safe-list pattern. When enabled, unknown Bash commands are sent to a fast/cheap LLM for evaluation instead of immediately prompting the user.

### How it works

Sends a single completion request with this prompt:

```
You are a security evaluator for a CLI coding agent working in: {cwd}

Evaluate this tool call:
Tool: {toolName}
Input: {sanitized input, truncated to 500 bytes}

Respond with exactly one line:
ALLOW
DENY: <reason>
ASK: <reason>

Consider: working directory boundaries, system damage risk, data exfiltration,
common dev operations.
```

The response is parsed line-by-line. Only the first line is considered:
- `ALLOW` → `VerdictAllow`
- `DENY: <reason>` → `VerdictDeny`
- `ASK: <reason>` → `VerdictAsk`
- Anything else → `VerdictAsk` (ambiguous response defaults to prompting)

### Configuration

Controlled via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `BITCODE_GUARD_LLM` | (unset) | Set to `true` to enable |
| `BITCODE_GUARD_LLM_MODEL` | Main model | Model to use for evaluation |
| `BITCODE_GUARD_LLM_BASE_URL` | Main base URL | API endpoint |
| `BITCODE_GUARD_LLM_API_KEY` | Main API key | API key |

Typically you'd point this at a fast, cheap model (e.g. a small local model or a low-cost API tier) since it only needs to classify commands as safe/unsafe.

### Fallback behavior

- If the LLM guard is enabled but the API call fails → falls back to `VerdictAsk` (prompt user)
- If the LLM guard is not enabled → `VerdictLLM` automatically falls back to `VerdictAsk`

## Guard Agent

The **Guard Agent** is an expert multi-turn LLM agent designed for security-aware tool validation. It replaces the simple single-turn LLM guard with a sophisticated agent that uses domain-specific skills and structured reasoning.

### Architecture

```
                         Tool Call (Name + JSON args)
                                    │
                                    ▼
                         ┌──────────────────────┐
                         │   Guard Manager       │
                         │   Rule Chain          │
                         └──────────┬───────────┘
                                    │ VerdictLLM
                                    ▼
                         ┌──────────────────────────────────────┐
                         │   Guard Agent                         │
                         │   ──────────────────────────────────  │
                         │   Expert persona system prompt        │
                         │                                       │
                         │   tools.Manager                       │
                         │     └─ SkillTool ◄─ GuardSkillMgr   │◄── guard-skills/ dirs
                         │          (embedded built-ins)        │
                         │                                       │
                         │   Standard tool-call agent loop:      │
                         │     Complete() → FinishToolCalls      │
                         │       → SkillTool.Execute()           │
                         │       → inject skill body as result   │
                         │     → Continue until FinishStop       │
                         │       → parse ALLOW/DENY/ASK          │
                         └──────────────────────────────────────┘
```

### Features

1. **Expert Persona** — A senior security engineer and sysadmin with extensive cloud deployment experience (AWS, GCP, Azure, Kubernetes).

2. **Multi-turn Reasoning** — Uses a standard tool-call loop (same pattern as the main agent) to reason about complex tool calls. The agent can invoke skills, analyze code, and make informed decisions.

3. **Language-Specific Skills** — Automatically detects the programming language/runtime and injects relevant security context:
   - **Bash** — Command substitution, eval, redirection attacks, pipe-to-shell
   - **Python** — `eval()`, `exec()`, `subprocess` with `shell=True`, pickle deserialization
   - **Go** — `exec.Command`, shell invocation, `unsafe` package, path traversal
   - **JavaScript/TypeScript** — `eval()`, `child_process`, prototype pollution, SSRF

4. **Code Simulation** — On-demand skill for step-by-step code tracing to predict execution behavior before allowing the call.

### Guard Skills

Guard skills work the same as the main agent's skills system but are loaded from `guard-skills/` directories:

```
~/.agents/guard-skills/       ← lowest precedence (disk)
~/.claude/guard-skills/
~/.bitcode/guard-skills/
.agents/guard-skills/         ← project-level
.claude/guard-skills/
.bitcode/guard-skills/        ← highest precedence (disk)
internal/guard/skills/        ← embedded built-ins (lowest of all)
```

### Built-in Guard Skills

The following skills are embedded in the binary:

| Skill | Language | Auto-invoke | Description |
|-------|----------|-------------|-------------|
| `bash.md` | `bash` | Yes | Bash security patterns and dangerous constructs |
| `python.md` | `python` | Yes | Python security patterns (subprocess, eval, pickle) |
| `go.md` | `go` | Yes | Go security patterns (exec.Command, unsafe) |
| `js.md` | `js` | Yes | JS/TS security patterns (eval, child_process) |
| `simulate.md` | — | No | Code simulation protocol (on-demand) |

### Skill Frontmatter

Guard skills support additional frontmatter fields:

```markdown
---
name: Bash Security Expert
description: Pattern library for dangerous Bash constructs
language: bash
auto_invoke: true
---
# Bash Security Patterns
...
```

| Field | Type | Description |
|-------|------|-------------|
| `language` | string | The language this skill applies to |
| `auto_invoke` | bool | If true, automatically inject skill body into guard context |

### How Auto-Injection Works

When the guard agent evaluates a tool call:

1. **Language Detection** — The guard detects the language/runtime from the tool call:
   - `Bash` tool → always `bash`
   - `Bash` with `python`/`python3`/`uv run` → `python`
   - `Bash` with `go run`/`go build`/`go test` → `go`
   - `Bash` with `node`/`deno`/`bun`/`npx` → `js`
   - File tools with `.py`/`.go`/`.js`/`.ts` extensions

2. **Auto-Inject Skills** — Skills with `auto_invoke: true` matching the detected language have their bodies pre-injected into the first user message. The guard LLM sees them immediately without needing to make a tool call.

3. **On-Demand Skills** — All skills are listed in the system prompt. The guard can invoke them via `SkillTool` when it needs deeper analysis.

### Example Guard Agent Evaluation

```
[User message to guard]
  Tool: Bash
  Input: python3 -c "import subprocess; subprocess.run('rm -rf /tmp/old', shell=True)"
  Auto-context: [python.md body pre-injected]

[Assistant — FinishToolCalls]
  {"tool": "Skill", "arguments": {"skill": "simulate"}}

[Tool result]
  # Code Simulation Protocol
  ... (simulate.md body)

[Assistant — FinishStop]
  "Tracing the code: subprocess.run with shell=True executing 'rm -rf /tmp/old'.
   /tmp/old is outside working directory. Shell=True with a literal string is acceptable
   but the path is fixed (/tmp) not cwd-relative.
   ASK: subprocess.run with shell=True deletes outside working directory"

→ Decision{Verdict: VerdictAsk, Reason: "subprocess.run with shell=True deletes outside working directory"}
```

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `BITCODE_GUARD_LLM` | `true` | Enable the Guard Agent (set to `false` to disable) |
| `BITCODE_GUARD_MODEL` | main model | Model to use for guard agent |
| `BITCODE_GUARD_MAX_TURNS` | `5` | Max turns for guard agent reasoning loop |

### Disabling the Guard Agent

To use only rule-based guards without LLM evaluation:

```bash
BITCODE_GUARD_LLM=false ./bitcode
```

This will cause all `VerdictLLM` verdicts to fall back to `VerdictAsk` (prompt user) instead of calling the Guard Agent.

## User Permission Prompt

When the final verdict is `VerdictAsk`, the guard system prompts the user for approval using a minimal bubbletea program (`internal/guard/prompt.go`).

### Rendering

```
⚠ Guard: Bash command modifies /tmp/old-builds which is outside working directory /home/user/project
  Tool: Bash

  [y] Allow once  [a] Always allow  [n] Deny
```

### Keybindings

| Key | Action |
|-----|--------|
| `y` | Allow this one tool call |
| `a` | Always allow (caches for the session) |
| `n`, `q`, `Esc`, `Ctrl+C` | Deny |

### Spinner integration

The permission prompt needs to pause the thinking spinner before displaying. The `TerminalPermissionHandler` accepts `pauseThinking` and `resumeThinking` callbacks. In practice, only `pauseThinking` is needed — the spinner restarts automatically on the next `OnThinking(true)` call when the LLM begins its next turn.

```go
// app/main.go — wiring
guardMgr.SetPermissionHandler(guard.TerminalPermissionHandler(
    func() {  // pauseThinking
        if spin != nil {
            spin.Stop()
            spin = nil
        }
    },
    nil, // resumeThinking not needed
))
```

### Non-interactive mode

When BitCode runs with `-p` (single-shot mode), the `AutoDenyHandler` is used instead. All `VerdictAsk` decisions are automatically denied, and the LLM receives an error explaining why the operation was blocked.

```go
if isNonInteractive {
    guardMgr.SetPermissionHandler(guard.AutoDenyHandler())
}
```

### Session caching

When a user chooses "Always allow" (`a`), the approval is cached in `sessionApproved` using the key `"toolName:reason"`. This means:

- The same type of operation (same tool, same reason string) won't prompt again
- The cache lives only for the current process — restarting BitCode resets it
- There is no persistent allow-list on disk

## Plugin System

Guard plugins allow projects to define custom rules via configuration files (`internal/guard/plugins.go`).

### Directory precedence

Same precedence model as reminders and skills. Later entries with the same `id` overwrite earlier ones:

1. `~/.agents/guards/` (lowest)
2. `~/.claude/guards/`
3. `~/.bitcode/guards/`
4. `.agents/guards/` (project-level)
5. `.claude/guards/`
6. `.bitcode/guards/` (highest)

### File formats

**YAML:**

```yaml
# .bitcode/guards/block-docker.yaml
id: block-docker
tool: Bash
patterns:
  - match: "docker"
    verdict: ask
    reason: "Docker commands require approval"
```

**Markdown with frontmatter:**

```markdown
---
id: protect-env
tool: Write,Edit
patterns:
  - file_match: ".env*"
    verdict: ask
    reason: "Modifying environment configuration"
---
```

### Plugin fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | No (derived from filename) | Unique identifier for deduplication |
| `tool` | string | No | Comma-separated tool names to match (e.g. `Bash`, `Write,Edit`). Empty = all tools. |
| `patterns` | list | Yes | Pattern rules to evaluate |

### Pattern fields

| Field | Type | Description |
|-------|------|-------------|
| `match` | string (regex) | Regex matched against the Bash command string |
| `file_match` | string (glob) | Glob matched against the file basename for file tools (Read, Write, Edit) |
| `verdict` | string | `allow`, `deny`, or `ask` |
| `reason` | string | Human-readable reason shown to the user |

Each pattern can have either `match` (for Bash) or `file_match` (for file tools), or both. Patterns are checked in order; the first matching pattern determines the verdict.

### Example plugins

**Block all network access:**

```yaml
id: no-network
tool: Bash
patterns:
  - match: "\\b(curl|wget|ssh|scp|nc|netcat)\\b"
    verdict: ask
    reason: "Network access requires approval"
```

**Protect database migrations:**

```yaml
id: protect-migrations
tool: Write,Edit
patterns:
  - file_match: "*.sql"
    verdict: ask
    reason: "Modifying SQL migration files requires approval"
```

**Block package installation:**

```yaml
id: no-install
tool: Bash
patterns:
  - match: "\\b(apt|yum|brew|pip|gem)\\s+install\\b"
    verdict: ask
    reason: "Package installation requires approval"
```

## Event rendering

Guard events are rendered in yellow/amber to visually distinguish them from normal tool events (green) and errors (red).

```
⚠ Guard(Bash)
⎿  Blocked: dangerous command blocked: rm -rf /
```

The event uses `PreviewType: PreviewGuard` (defined in `internal/event.go`) and is rendered by `renderGuardEvent()` in `app/render.go`.

## System prompt

The system prompt (`app/system_prompt.go`) includes instructions telling the LLM about guards:

```
# Safety Guards
Tool calls are subject to safety guards. If a tool call is blocked, you will receive
an error explaining why. Do not retry blocked operations. Instead, explain to the user
what you wanted to do and suggest alternatives.
```

This prevents the LLM from entering retry loops when a tool call is blocked.

## Code layout

```
internal/guard/
  guard.go       # Core types (Verdict, Decision, EvalContext, Rule, PermissionHandler)
  manager.go     # Manager — rule chain evaluation, session caching, verdict escalation
  rules.go       # 4 built-in rules (DangerousCommand, WorkingDir, SensitiveFile, DefaultPolicy)
  llm_guard.go   # Deprecated — replaced by GuardAgent
  guard_agent.go # GuardAgent — multi-turn LLM agent with SkillTool support
  guard_prompt.go # BuildGuardSystemPrompt() — expert persona prompt
  langdetect.go  # DetectLanguage() + SkillsForLanguage() helpers
  prompt.go      # Terminal permission prompt (TerminalPermissionHandler, AutoDenyHandler)
  plugins.go     # Plugin loading from guards/ directories
  guard_test.go  # Tests for rules, manager, LLM parsing, plugin parsing
  skills/        # Embedded built-in guard skills
    bash.md      #   Bash security patterns (auto_invoke: true)
    python.md    #   Python security patterns (auto_invoke: true)
    go.md        #   Go security patterns (auto_invoke: true)
    js.md        #   JS/TS security patterns (auto_invoke: true)
    simulate.md  #   Code simulation protocol (on-demand)

internal/skills/
  skills.go      # Skill manager with Config support (SubDir, Embedded, Metadata)
```

Integration points in `app/`:
- `agent.go` — Guard check before `ExecuteTool` in the tool call loop; `GuardMgr` field on `AgentConfig`
- `main.go` — Creates the Manager, registers built-in rules in priority order, loads plugins, configures LLM guard and permission handlers
- `render.go` — `renderGuardEvent()` for yellow guard event display
- `system_prompt.go` — Safety guard instructions for the LLM
- `internal/event.go` — `PreviewGuard` constant
