---
name: simulate
description: Step-by-step code tracing to predict execution output before rendering a verdict
auto_invoke: false
---
# Code Simulation Protocol

When asked to simulate code, follow these steps precisely:

## Step 1 — Parse
List all variable declarations and their initial values:
```
VAR name = value
VAR name = <unknown>  (if value depends on env/runtime)
```

## Step 2 — Trace
Step through each statement in execution order:
```
[line N] expression → value
```

## Step 3 — Expand
For every variable reference, substitute its current value before evaluating. Pay special attention to:
- Shell variables that may be empty (e.g., `rm -rf "$DIR/"*` with empty `$DIR` becomes `rm -rf /*`)
- Environment variables not in context → treat as `<unknown>` → assume worst-case for safety

## Step 4 — Identify side effects
For each I/O or system call, explicitly state:
- Files read / written / deleted
- Commands spawned (including shell strings from `eval`, `exec`, `subprocess`, `child_process`, etc.)
- Network connections made (host, port, method)
- Environment modifications

## Step 5 — Summarize
Produce a final execution summary:
```
Files written:   [list or "none"]
Files deleted:   [list or "none"]
Commands:        [list or "none"]
Network:         [list or "none"]
Env changes:     [list or "none"]
```

## Step 6 — Verdict
Based on the execution summary, state your verdict with reason:
```
ALLOW  — routine dev operation, no destructive or exfiltration risk
ASK    — ambiguous or context-dependent risk, user should decide
DENY   — clearly destructive, exfiltrating data, or escalating privilege
```

**Safety rule:** When a variable value is `<unknown>`, use the worst-case expansion in your safety analysis. An unknown path in `rm -rf $X` must be treated as a potentially critical path.
