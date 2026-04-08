package guard

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// isShellTool reports whether the given tool name is a shell execution tool.
// The tool is registered as "Bash" on Unix and "PowerShell" on Windows.
func isShellTool(name string) bool {
	return name == "Bash" || name == "PowerShell"
}

// shellPathRe matches absolute paths in shell commands.
// On Windows it matches drive-letter paths (C:\...) and UNC paths (\\...).
// On Unix it matches paths starting with /.
var shellPathRe *regexp.Regexp

func init() {
	if runtime.GOOS == "windows" {
		shellPathRe = regexp.MustCompile(`(?i)(?:^|[\s;|&>"'])([A-Za-z]:\\[^\s;|&>"']*|\\\\[^\s;|&>"']+)`)
	} else {
		shellPathRe = regexp.MustCompile(`(?:^|[\s;|&>"'])(/[^\s;|&>"']+)`)
	}
}

// --- WorkingDirRule ---

// WorkingDirRule checks that file operations stay within the working directory.
type WorkingDirRule struct{}

func (r *WorkingDirRule) Evaluate(ctx *EvalContext) *Decision {
	switch ctx.ToolName {
	case "Read", "Write", "Edit", "Glob":
		return r.checkFileTool(ctx)
	case "Bash", "PowerShell":
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

// shellWriteCommands are commands that modify the filesystem (Unix and PowerShell).
var shellWriteCommands = map[string]bool{
	// Unix
	"rm": true, "mv": true, "cp": true, "chmod": true,
	"mkdir": true, "rmdir": true, "tee": true, "dd": true,
	"chown": true, "touch": true,
	// PowerShell cmdlets (first word / alias)
	"Remove-Item": true, "Move-Item": true, "Copy-Item": true,
	"New-Item": true, "Rename-Item": true,
	"Set-Content": true, "Add-Content": true, "Out-File": true,
	"Clear-Content": true,
}

func (r *WorkingDirRule) checkBash(ctx *EvalContext) *Decision {
	cmd := extractCommand(ctx)
	if cmd == "" {
		return nil
	}

	matches := shellPathRe.FindAllStringSubmatch(cmd, -1)

	for _, m := range matches {
		absPath := filepath.Clean(m[1])
		if isInsideDir(absPath, ctx.WorkingDir) {
			continue
		}

		// Check if the command is write-oriented
		cmdName := extractFirstWord(cmd)
		if shellWriteCommands[cmdName] {
			return &Decision{
				Verdict: VerdictAsk,
				Reason:  fmt.Sprintf("Shell command modifies %s which is outside working directory %s", absPath, ctx.WorkingDir),
			}
		}
	}

	return nil
}

// --- DangerousCommandRule ---

// DangerousCommandRule catches dangerous shell commands.
type DangerousCommandRule struct{}

// Patterns that are always denied (Unix + PowerShell).
var denyPatterns = []*regexp.Regexp{
	// Unix: rm -rf /  rm -rf ~  rm -rf $HOME
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|(-[a-zA-Z]*f[a-zA-Z]*r))\s+/\s*$`),
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|(-[a-zA-Z]*f[a-zA-Z]*r))\s+~\s*$`),
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|(-[a-zA-Z]*f[a-zA-Z]*r))\s+\$HOME\b`),
	// Unix: mkfs, dd to block device, fork bomb, chmod 777 /
	regexp.MustCompile(`\bmkfs\b`),
	regexp.MustCompile(`\bdd\b.*\bof=/dev/`),
	regexp.MustCompile(`:\(\)\{.*\|.*\};:`),
	regexp.MustCompile(`\bchmod\s+-R\s+777\s+/\s*$`),
	// PowerShell: Remove-Item -Recurse -Force targeting a drive root or system dirs
	regexp.MustCompile(`(?i)\bRemove-Item\b.*-Recurse.*-Force\s+[A-Za-z]:\\\s*$`),
	regexp.MustCompile(`(?i)\bRemove-Item\b.*-Force.*-Recurse\s+[A-Za-z]:\\\s*$`),
	// PowerShell: Remove-Item on home directory
	regexp.MustCompile(`(?i)\bRemove-Item\b.*\$env:USERPROFILE\b`),
	regexp.MustCompile(`(?i)\bRemove-Item\b.*\$HOME\b`),
	// PowerShell: Format-Volume (disk format)
	regexp.MustCompile(`(?i)\bFormat-Volume\b`),

	// PowerShell: Encoded/obfuscated command execution
	regexp.MustCompile(`(?i)(powershell|pwsh)\s+.*-enc(odedcommand)?\s`),

	// PowerShell: AMSI bypass (disables PowerShell's primary security layer)
	regexp.MustCompile(`(?i)System\.Management\.Automation\.Amsi`),
	regexp.MustCompile(`(?i)\[Ref\]\.Assembly\.GetType`),

	// PowerShell: Execution Policy bypass
	regexp.MustCompile(`(?i)(powershell|pwsh)\s+.*-e(xecutionpolicy)?\p{L}*\s+(Bypass|Unrestricted)`),

	// PowerShell: ScriptBlock/Reflection-based execution
	regexp.MustCompile(`(?i)\[ScriptBlock\]::Create\(`),
	regexp.MustCompile(`(?i)\[PowerShell\]::Create\(\)`),

	// PowerShell: .NET reflection (arbitrary assembly loading)
	regexp.MustCompile(`(?i)\[System\.Reflection\.Assembly\]::(Load|LoadFile|LoadFrom)\(`),

	// PowerShell: Defender/security tampering
	regexp.MustCompile(`(?i)\bSet-MpPreference\b.*-Disable`),
	regexp.MustCompile(`(?i)\bAdd-MpPreference\b.*-Exclusion`),

	// PowerShell: Constrained Language Mode bypass
	regexp.MustCompile(`(?i)\.LanguageMode\s*=\s*['"]?FullLanguage`),
}

// Patterns that require user approval (Unix + PowerShell).
var askPatterns = []struct {
	re     *regexp.Regexp
	reason string
}{
	// Unix patterns
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
	// PowerShell: Invoke-Expression / iex (arbitrary code execution)
	{regexp.MustCompile(`(?i)\bInvoke-Expression\b|\biex\b`), "Invoke-Expression can execute arbitrary code"},
	// PowerShell: download-and-execute (iwr/irm | iex)
	{regexp.MustCompile(`(?i)\b(Invoke-WebRequest|Invoke-RestMethod|iwr|irm)\b.*\|\s*(Invoke-Expression|iex)\b`), "download-and-execute is dangerous"},
	{regexp.MustCompile(`(?i)\b(Invoke-Expression|iex)\s*\(?\s*(Invoke-WebRequest|Invoke-RestMethod|iwr|irm)\b`), "download-and-execute is dangerous"},
	// PowerShell: Start-Process as Administrator
	{regexp.MustCompile(`(?i)\bStart-Process\b.*-Verb\s+RunAs\b`), "running as Administrator requires approval"},
	// PowerShell: Download cradles (network fetch + potential execute)
	{regexp.MustCompile(`(?i)\bNew-Object\b.*\bNet\.WebClient\b`), "WebClient download cradle requires approval"},
	{regexp.MustCompile(`(?i)\bSystem\.Net\.WebClient\b`), "WebClient usage requires approval"},
	// PowerShell: Base64 decoding (can be used for obfuscation but also has legitimate uses)
	{regexp.MustCompile(`(?i)\[System\.Convert\]::FromBase64String`), "Base64 decoding requires approval"},
	// PowerShell: Dynamic invocation via ExecutionContext
	{regexp.MustCompile(`(?i)\$ExecutionContext\.InvokeCommand\.(ExpandString|InvokeScript|NewScriptBlock)`), "dynamic code execution requires approval"},
	// PowerShell: WMI process creation
	{regexp.MustCompile(`(?i)\bInvoke-WmiMethod\b.*\bWin32_Process\b`), "WMI process creation requires approval"},
	{regexp.MustCompile(`(?i)\bInvoke-CimMethod\b.*\bWin32_Process\b`), "CIM process creation requires approval"},
	// PowerShell: Registry persistence paths
	{regexp.MustCompile(`(?i)(Set-ItemProperty|New-ItemProperty|Remove-ItemProperty).*(HKLM|HKCU).*(Run|RunOnce|Winlogon)`), "registry persistence path modification requires approval"},
	// PowerShell: COM object instantiation (scriptable shell/network objects)
	{regexp.MustCompile(`(?i)\bNew-Object\b.*-ComObject\b`), "COM object creation requires approval"},
	// PowerShell: Credential access
	{regexp.MustCompile(`(?i)\bGet-Credential\b`), "credential prompt requires approval"},
	{regexp.MustCompile(`(?i)\bGet-StoredCredential\b`), "credential store access requires approval"},
	{regexp.MustCompile(`(?i)\bvaultcmd\b`), "credential vault access requires approval"},
	// PowerShell: Hidden window execution
	{regexp.MustCompile(`(?i)(powershell|pwsh)\s+.*-W(indowStyle)?\s+Hidden`), "hidden window execution requires approval"},
}

func (r *DangerousCommandRule) Evaluate(ctx *EvalContext) *Decision {
	if !isShellTool(ctx.ToolName) {
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

// SensitiveFileRule requires approval before reading or modifying sensitive files.
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
	switch ctx.ToolName {
	case "Read", "Write", "Edit":
		// fall through to check
	default:
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

	action := "accessing"
	if ctx.ToolName == "Write" || ctx.ToolName == "Edit" {
		action = "modifying"
	}

	base := filepath.Base(path)
	for _, pattern := range sensitivePatterns {
		if matched, _ := filepath.Match(pattern, base); matched {
			return &Decision{
				Verdict: VerdictAsk,
				Reason:  fmt.Sprintf("%s sensitive file: %s", action, path),
			}
		}
		// Also check against the full path for patterns like ".ssh/*"
		if matched, _ := filepath.Match(pattern, path); matched {
			return &Decision{
				Verdict: VerdictAsk,
				Reason:  fmt.Sprintf("%s sensitive file: %s", action, path),
			}
		}
	}

	return nil
}

// --- DefaultPolicyRule ---

// DefaultPolicyRule provides baseline policy when no other rule fires.
type DefaultPolicyRule struct{}

// safePrefixes lists known-safe shell command prefixes.
// Populated at init time so it can be platform-aware.
var safePrefixes []string

func init() {
	common := []string{
		"echo ", "pwd",
		"git status", "git log", "git diff", "git branch", "git show", "git stash",
		"go build", "go test", "go run", "go vet", "go fmt", "go mod tidy",
		"npm test", "npm run", "npm ci", "npm install",
		"cargo build", "cargo test", "cargo check",
		"make", "cmake",
		"grep ", "rg ",
	}
	if runtime.GOOS == "windows" {
		safePrefixes = append(common,
			// PowerShell-specific safe read-only / query commands
			"Get-ChildItem", "gci ", "dir ", "ls ",
			"Get-Content ", "gc ",
			"Write-Output ", "Write-Host ",
			"Get-Location", "cd ",
			"Test-Path ", "Get-Item ",
			"Select-String ",
			"where ", "where.exe ",
			"$PSVersionTable", "$env:",
		)
	} else {
		safePrefixes = append(common,
			"which ", "env", "printenv",
			"ls ", "ls\n", "cat ", "head ", "tail ", "wc ", "sort ", "uniq ", "diff ",
			"find ", "fd ", "ag ",
		)
	}
}

func (r *DefaultPolicyRule) Evaluate(ctx *EvalContext) *Decision {
	switch ctx.ToolName {
	case "Skill", "Read", "Glob":
		return &Decision{Verdict: VerdictAllow, Reason: "read-only tool"}
	case "Write", "Edit":
		return &Decision{Verdict: VerdictAllow, Reason: "file write in working directory"}
	case "Bash", "PowerShell":
		cmd := extractCommand(ctx)
		if cmd == "" {
			return &Decision{Verdict: VerdictAllow, Reason: "empty command"}
		}

		trimmed := strings.TrimSpace(cmd)
		for _, prefix := range safePrefixes {
			if strings.HasPrefix(trimmed, prefix) || trimmed == strings.TrimSpace(prefix) {
				return &Decision{Verdict: VerdictAllow, Reason: "known-safe command"}
			}
		}

		return &Decision{
			Verdict: VerdictLLM,
			Reason:  fmt.Sprintf("unknown shell command needs validation: %s", truncate(cmd, 80)),
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
