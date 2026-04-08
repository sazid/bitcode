package tools

import (
	"os"
	"os/exec"
	"runtime"
)

// ShellInfo holds information about the shell used to execute commands.
type ShellInfo struct {
	// Path is the full path or name of the shell executable.
	Path string
	// Args are the arguments passed to the shell before the command string.
	// e.g. ["-c"] for bash, ["-NoProfile", "-NonInteractive", "-Command"] for PowerShell.
	Args []string
	// Name is the human-readable display name reported in the system prompt / environment block.
	Name string
}

// GetShellInfo returns the appropriate shell configuration for the current OS.
//
// On Windows it prefers (in order):
//  1. SHELL env var override (Git Bash / Cygwin / MSYS2 setups that set it)
//  2. pwsh  – PowerShell 7+ (cross-platform)
//  3. powershell.exe – Windows PowerShell 5.1 (always present on Win10+)
//
// On Unix it uses the SHELL environment variable, falling back to /bin/bash.
func GetShellInfo() ShellInfo {
	if runtime.GOOS == "windows" {
		// Honour an explicit SHELL override (e.g. Git Bash, Cygwin, MSYS2).
		if s := os.Getenv("SHELL"); s != "" {
			return ShellInfo{Path: s, Args: []string{"-c"}, Name: s}
		}
		// Prefer PowerShell 7+ (pwsh) when available.
		if path, err := exec.LookPath("pwsh"); err == nil {
			return ShellInfo{
				Path: path,
				Args: []string{"-NoProfile", "-NonInteractive", "-Command"},
				Name: "pwsh (PowerShell 7)",
			}
		}
		// Fall back to built-in Windows PowerShell 5.1.
		return ShellInfo{
			Path: "powershell.exe",
			Args: []string{"-NoProfile", "-NonInteractive", "-Command"},
			Name: "powershell.exe (Windows PowerShell 5.1)",
		}
	}

	// Unix: respect SHELL env var, default to /bin/bash.
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	return ShellInfo{Path: shell, Args: []string{"-c"}, Name: shell}
}
