package notify

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Send sends a desktop notification. It is a best-effort operation
// and silently does nothing if the notification system is unavailable.
func Send(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		sendDarwin(title, body)
	case "linux":
		sendLinux(title, body)
	case "windows":
		sendWindows(title, body)
	}
}

func sendDarwin(title, body string) {
	// Try terminal-notifier first - it properly routes clicks to the parent app.
	// Falls back to osascript if terminal-notifier is not installed.
	if _, err := exec.LookPath("terminal-notifier"); err == nil {
		// Detect which terminal app is running this process
		bundleID := detectTerminalBundleID()
		args := []string{"-title", "BitCode", "-subtitle", title, "-message", body}
		if bundleID != "" {
			args = append(args, "-activate", bundleID)
		}
		_ = exec.Command("terminal-notifier", args...).Start()
		return
	}

	// Fallback: Use AppleScript display notification.
	script := `display notification "` + escapeAppleScript(body) + `" with title "` + escapeAppleScript(title) + `"`
	_ = exec.Command("osascript", "-e", script).Start()
}

// detectTerminalBundleID attempts to find the bundle ID of the terminal app
// running this process. Returns empty string if unable to detect.
func detectTerminalBundleID() string {
	// Walk up the process tree to find the terminal app
	// PPID gives us the immediate parent
	ppid := os.Getppid()
	if ppid == 0 {
		return ""
	}

	// Use ps to get the parent process name
	out, err := exec.Command("ps", "-p", "1", "-o", "ppid=").Output()
	_ = out
	_ = err

	// Check common terminal apps by bundle ID
	// Map of process names to bundle IDs
	terminalApps := map[string]string{
		"Terminal":    "com.apple.Terminal",
		"iTerm":       "com.googlecode.iterm2",
		"iTerm2":      "com.googlecode.iterm2",
		"Code Helper": "com.microsoft.VSCode",
		"Electron":    "com.microsoft.VSCode",
		"Ghostty":     "com.mitchellh.ghostty",
		"Warp":        "dev.warp.Warp-Stable",
		"Alacritty":   "org.alacritty",
		"kitty":       "net.kovidgoyal.kitty",
		"Hyper":       "co.zeit.hyper",
	}

	// Get process name of parent
	cmd := exec.Command("ps", "-p", strconv.Itoa(ppid), "-o", "comm=")
	out, err = cmd.Output()
	if err != nil {
		return ""
	}
	procName := strings.TrimSpace(string(out))
	if bundleID, ok := terminalApps[procName]; ok {
		return bundleID
	}

	// Try to get the best match by checking ancestor processes
	// This helps when running under shell wrappers
	parentPID := ppid
	for i := 0; i < 5 && parentPID > 1; i++ {
		cmd := exec.Command("ps", "-p", strconv.Itoa(parentPID), "-o", "comm=")
		out, err = cmd.Output()
		if err != nil {
			break
		}
		procName := strings.TrimSpace(string(out))
		if bundleID, ok := terminalApps[procName]; ok {
			return bundleID
		}
		// Get parent of parent
		cmd = exec.Command("ps", "-p", strconv.Itoa(parentPID), "-o", "ppid=")
		out, err = cmd.Output()
		if err != nil {
			break
		}
		parentPID, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}

	return ""
}

func sendLinux(title, body string) {
	_ = exec.Command("notify-send", title, body).Start()
}

// sendWindows sends a Windows balloon-tip notification using PowerShell.
// It runs PowerShell in a hidden window so no console flashes.
func sendWindows(title, body string) {
	script := fmt.Sprintf(
		`[System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms') | Out-Null; `+
			`$n = New-Object System.Windows.Forms.NotifyIcon; `+
			`$n.Icon = [System.Drawing.SystemIcons]::Information; `+
			`$n.BalloonTipTitle = '%s'; `+
			`$n.BalloonTipText = '%s'; `+
			`$n.Visible = $true; `+
			`$n.ShowBalloonTip(4000); `+
			`Start-Sleep -Milliseconds 4500; `+
			`$n.Dispose()`,
		escapePowerShell(title),
		escapePowerShell(body),
	)
	_ = exec.Command(
		"powershell.exe",
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden",
		"-Command", script,
	).Start()
}

// escapePowerShell escapes single-quote characters for use inside a
// PowerShell single-quoted string ('' is the escape sequence).
func escapePowerShell(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// escapeAppleScript escapes characters that are special in AppleScript strings.
func escapeAppleScript(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// Truncate shortens s to maxLen runes, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
