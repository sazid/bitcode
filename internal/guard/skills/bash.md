---
name: Bash Security Expert
description: Pattern library for dangerous Bash constructs — auto-applied to all Bash tool calls
language: bash
auto_invoke: true
---
# Bash Security Patterns

You are reviewing a Bash command or script. Apply these checks in order.

## High-risk patterns (lean toward DENY or ASK)

### Command substitution
`$(...)` and backtick expansion execute arbitrary subcommands. Trace what runs inside them.
```bash
rm -rf $(cat /tmp/dirs)   # contents of /tmp/dirs control what gets deleted
```

### Eval and dynamic execution
`eval`, `source <(...)`, `bash -c "..."`, `sh -c "..."` treat a string as shell code.
Any user-controlled or environment-controlled value reaching eval is code injection.

### Pipe-to-shell
```bash
curl URL | bash
wget -O- URL | sh
```
Downloads and executes arbitrary remote code. Always ASK or DENY.

### Redirect to sensitive paths
```bash
> /etc/passwd
>> ~/.bashrc
>> ~/.ssh/authorized_keys
> /usr/local/bin/somebinary
```
Overwrites system files or gains persistence. DENY unless path is clearly within cwd.

### Variable expansion edge cases
```bash
DIR=""
rm -rf "$DIR/"*    # expands to: rm -rf /*  — deletes root
```
Always check: what happens if a variable is empty or contains path separators?

### Here-string injection
```bash
bash <<< "$var"    # executes $var as shell code if user-controlled
```

### Glob expansion in unexpected directories
```bash
cd /tmp && rm *    # rm all files in /tmp
```
Check the working directory before glob operations.

### PATH manipulation
```bash
export PATH="/tmp/evil:$PATH"
```
Prepending untrusted directories allows command hijacking. ASK.

### Subshell tricks
```bash
$(IFS=X;cmdXarg)   # IFS manipulation to split/join args unexpectedly
```

## Low-risk patterns (lean toward ALLOW)

- Simple reads: `cat`, `ls`, `grep`, `find`, `head`, `tail`, `wc`
- Git read-only: `git status`, `git log`, `git diff`, `git show`
- Build/test: `go test`, `npm test`, `cargo test`, `make` (check Makefile targets)
- File creation within cwd with literal paths
- Variable assignments without side effects

## Simulation checklist

1. Does any variable in the command have an unknown or potentially empty value?
2. Are there any pipe-to-shell patterns?
3. Does any redirection target a path outside cwd?
4. Does any eval/source receive external input?
5. Does the command modify PATH, LD_PRELOAD, or other env vars?
