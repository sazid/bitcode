package guard

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// --- WorkingDirRule ---

// WorkingDirRule checks that file operations stay within the working directory.
type WorkingDirRule struct{}

func (r *WorkingDirRule) Evaluate(ctx *EvalContext) *Decision {
	switch ctx.ToolName {
	case "Read", "Write", "Edit", "Glob":
		return r.checkFileTool(ctx)
	case "Bash":
		return r.checkBash(ctx)
	default:
		return nil
	}
}

func (r *WorkingDirRule) checkFileTool(ctx *EvalContext) *Decision {
	var input map[string]any
	if err := json.Unmarshal(ctx.Input, &input); err != nil {
		return nil
	}

	// Check file_path or path field
	path, _ := input["file_path"].(string)
	if path == "" {
		path, _ = input["path"].(string)
	}
	if path == "" {
		return nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	absPath = filepath.Clean(absPath)

	if !isInsideDir(absPath, ctx.WorkingDir) {
		return &Decision{
			Verdict: VerdictAsk,
			Reason:  fmt.Sprintf("%s targets %s which is outside working directory %s", ctx.ToolName, absPath, ctx.WorkingDir),
		}
	}

	return nil
}

// bashWriteCommands are commands that modify the filesystem.
var bashWriteCommands = map[string]bool{
	"rm": true, "mv": true, "cp": true, "chmod": true,
	"mkdir": true, "rmdir": true, "tee": true, "dd": true,
	"chown": true, "touch": true,
}

func (r *WorkingDirRule) checkBash(ctx *EvalContext) *Decision {
	cmd := extractCommand(ctx)
	if cmd == "" {
		return nil
	}

	// Find absolute paths in the command
	pathRe := regexp.MustCompile(`(?:^|[\s;|&>"'])(/[^\s;|&>"']+)`)
	matches := pathRe.FindAllStringSubmatch(cmd, -1)

	for _, m := range matches {
		absPath := filepath.Clean(m[1])
		if isInsideDir(absPath, ctx.WorkingDir) {
			continue
		}

		// Check if the command is write-oriented
		cmdName := extractFirstWord(cmd)
		if bashWriteCommands[cmdName] {
			return &Decision{
				Verdict: VerdictAsk,
				Reason:  fmt.Sprintf("Bash command modifies %s which is outside working directory %s", absPath, ctx.WorkingDir),
			}
		}
	}

	return nil
}

// --- DangerousCommandRule ---

// DangerousCommandRule catches dangerous shell commands.
type DangerousCommandRule struct{}

// Patterns that are always denied.
var denyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|(-[a-zA-Z]*f[a-zA-Z]*r))\s+/\s*$`),
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|(-[a-zA-Z]*f[a-zA-Z]*r))\s+~\s*$`),
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|(-[a-zA-Z]*f[a-zA-Z]*r))\s+\$HOME\b`),
	regexp.MustCompile(`\bmkfs\b`),
	regexp.MustCompile(`\bdd\b.*\bof=/dev/`),
	regexp.MustCompile(`:\(\)\{.*\|.*\};:`),               // fork bomb
	regexp.MustCompile(`\bchmod\s+-R\s+777\s+/\s*$`),
}

// Patterns that require user approval.
var askPatterns = []struct {
	re     *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`\bsudo\b`), "sudo command requires approval"},
	{regexp.MustCompile(`\bcurl\b.*\|\s*(ba)?sh`), "pipe-to-shell is dangerous"},
	{regexp.MustCompile(`\bwget\b.*\|\s*(ba)?sh`), "pipe-to-shell is dangerous"},
	{regexp.MustCompile(`\bgit\s+push\s+--force\b`), "force push requires approval"},
	{regexp.MustCompile(`\bgit\s+push\s+-f\b`), "force push requires approval"},
	{regexp.MustCompile(`\bgit\s+reset\s+--hard\b`), "hard reset requires approval"},
	{regexp.MustCompile(`\bnpm\s+publish\b`), "publishing packages requires approval"},
	{regexp.MustCompile(`\bcargo\s+publish\b`), "publishing packages requires approval"},
	{regexp.MustCompile(`\bdocker\s+run\b`), "docker run requires approval"},
	{regexp.MustCompile(`\bdocker\s+exec\b`), "docker exec requires approval"},
}

func (r *DangerousCommandRule) Evaluate(ctx *EvalContext) *Decision {
	if ctx.ToolName != "Bash" {
		return nil
	}

	cmd := extractCommand(ctx)
	if cmd == "" {
		return nil
	}

	// Check deny patterns
	for _, re := range denyPatterns {
		if re.MatchString(cmd) {
			return &Decision{
				Verdict: VerdictDeny,
				Reason:  fmt.Sprintf("dangerous command blocked: %s", cmd),
			}
		}
	}

	// Check ask patterns
	for _, p := range askPatterns {
		if p.re.MatchString(cmd) {
			return &Decision{
				Verdict: VerdictAsk,
				Reason:  p.reason,
			}
		}
	}

	return nil
}

// --- SensitiveFileRule ---

// SensitiveFileRule requires approval before modifying sensitive files.
type SensitiveFileRule struct{}

var sensitivePatterns = []string{
	".env",
	".env.*",
	"*credentials*",
	"*secret*",
	"*.pem",
	"*.key",
	".git/config",
	".ssh/*",
}

func (r *SensitiveFileRule) Evaluate(ctx *EvalContext) *Decision {
	if ctx.ToolName != "Write" && ctx.ToolName != "Edit" {
		return nil
	}

	var input map[string]any
	if err := json.Unmarshal(ctx.Input, &input); err != nil {
		return nil
	}

	path, _ := input["file_path"].(string)
	if path == "" {
		return nil
	}

	base := filepath.Base(path)
	for _, pattern := range sensitivePatterns {
		if matched, _ := filepath.Match(pattern, base); matched {
			return &Decision{
				Verdict: VerdictAsk,
				Reason:  fmt.Sprintf("modifying sensitive file: %s", path),
			}
		}
		// Also check against the full path for patterns like ".ssh/*"
		if matched, _ := filepath.Match(pattern, path); matched {
			return &Decision{
				Verdict: VerdictAsk,
				Reason:  fmt.Sprintf("modifying sensitive file: %s", path),
			}
		}
	}

	return nil
}

// --- DefaultPolicyRule ---

// DefaultPolicyRule provides baseline policy when no other rule fires.
type DefaultPolicyRule struct{}

// Known-safe bash command prefixes that skip the guard.
var safeBashPrefixes = []string{
	"echo ", "pwd", "which ", "env", "printenv",
	"ls ", "ls\n", "cat ", "head ", "tail ", "wc ", "sort ", "uniq ", "diff ",
	"git status", "git log", "git diff", "git branch", "git show", "git stash",
	"go build", "go test", "go run", "go vet", "go fmt", "go mod tidy",
	"npm test", "npm run", "npm ci", "npm install",
	"cargo build", "cargo test", "cargo check",
	"make", "cmake",
	"grep ", "rg ", "ag ", "fd ", "find ",
}

func (r *DefaultPolicyRule) Evaluate(ctx *EvalContext) *Decision {
	switch ctx.ToolName {
	case "Skill", "Read", "Glob":
		return &Decision{Verdict: VerdictAllow, Reason: "read-only tool"}
	case "Write", "Edit":
		return &Decision{Verdict: VerdictAllow, Reason: "file write in working directory"}
	case "Bash":
		cmd := extractCommand(ctx)
		if cmd == "" {
			return &Decision{Verdict: VerdictAllow, Reason: "empty command"}
		}

		trimmed := strings.TrimSpace(cmd)
		for _, prefix := range safeBashPrefixes {
			if strings.HasPrefix(trimmed, prefix) || trimmed == strings.TrimSpace(prefix) {
				return &Decision{Verdict: VerdictAllow, Reason: "known-safe command"}
			}
		}

		return &Decision{
			Verdict: VerdictLLM,
			Reason:  fmt.Sprintf("unknown bash command needs validation: %s", truncate(cmd, 80)),
		}
	default:
		return &Decision{Verdict: VerdictAllow, Reason: "unknown tool, default allow"}
	}
}

// --- Helpers ---

func extractCommand(ctx *EvalContext) string {
	var input map[string]any
	if err := json.Unmarshal(ctx.Input, &input); err != nil {
		return ""
	}
	cmd, _ := input["command"].(string)
	return cmd
}

func extractFirstWord(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, " \t\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}

func isInsideDir(path, dir string) bool {
	// Normalize both paths
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
