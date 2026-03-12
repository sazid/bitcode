package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sazid/bitcode/internal"
)

type BashInput struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	Timeout     int64  `json:"timeout,omitempty"`
}

type BashTool struct{}

var _ Tool = (*BashTool)(nil)

func (b *BashTool) Name() string {
	return "Bash"
}

func (b *BashTool) Description() string {
	return `Executes a given bash command and returns its output.

The working directory persists between commands, but shell state does not. The shell environment is initialized from the user's profile (bash or zsh).

IMPORTANT: Avoid using this tool to run ` + "`find`" + `, ` + "`cat`" + `, ` + "`head`" + `, ` + "`tail`" + `, ` + "`sed`" + `, ` + "`awk`" + `, or ` + "`echo`" + ` commands, unless explicitly instructed or after you have verified that a dedicated tool cannot accomplish your task. Instead, use the appropriate dedicated tool as this will provide a much better experience for the user:

 - File search: Use Glob (NOT find or ls)
 - Read files: Use Read (NOT cat/head/tail)
 - Edit files: Use Edit (NOT sed/awk)
 - Write files: Use Write (NOT echo >/cat <<EOF)
 - Communication: Output text directly (NOT echo/printf)
While the Bash tool can do similar things, it's better to use the built-in tools as they provide a better user experience and make it easier to review tool calls and give permission.

# Instructions
 - If your command will create new directories or files, first use this tool to run ` + "`ls`" + ` to verify the parent directory exists and is the correct location.
 - Always quote file paths that contain spaces with double quotes in your command (e.g., cd "path with spaces/file.txt")
 - Try to maintain your current working directory throughout the session by using absolute paths and avoiding usage of ` + "`cd`" + `. You may use ` + "`cd`" + ` if the User explicitly requests it.
 - You may specify an optional timeout in milliseconds (up to 600000ms / 10 minutes). By default, your command will timeout after 120000ms (2 minutes).
 - Write a clear, concise description of what your command does. For simple commands, keep it brief (5-10 words). For complex commands (piped commands, obscure flags, or anything hard to understand at a glance), include enough context so that the user can understand what your command will do.
 - When issuing multiple commands:
  - If the commands are independent and can run in parallel, make multiple Bash tool calls in a single message. Example: if you need to run "git status" and "git diff", send a single message with two Bash tool calls in parallel.
  - If the commands depend on each other and must run sequentially, use a single Bash call with '&&' to chain them together.
  - Use ';' only when you need to run commands sequentially but don't care if earlier commands fail.
  - DO NOT use newlines to separate commands (newlines are ok in quoted strings).
 - For git commands:
  - Prefer to create a new commit rather than amending an existing commit.
  - Before running destructive operations (e.g., git reset --hard, git push --force, git checkout --), consider whether there is a safer alternative that achieves the same goal. Only use destructive operations when they are truly the best approach.
  - Never skip hooks (--no-verify) or bypass signing (--no-gpg-sign, -c commit.gpgsign=false) unless the user has explicitly asked for it. If a hook fails, investigate and fix the underlying issue.
 - Avoid unnecessary ` + "`sleep`" + ` commands:
  - Do not sleep between commands that can run immediately — just run them.
  - Do not retry failing commands in a sleep loop — diagnose the root cause or consider an alternative approach.
  - If you must sleep, keep the duration short (1-5 seconds) to avoid blocking the user.`
}

func (b *BashTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Clear, concise description of what this command does in active voice. For simple commands, keep it brief (5-10 words). For complex commands, include enough context to clarify what it does.",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Optional timeout in milliseconds (max 600000)",
			},
		},
		"required": []string{"command", "description"},
	}
}

func (b *BashTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params BashInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	if params.Command == "" {
		return ToolResult{}, fmt.Errorf("command is required")
	}

	// Default timeout: 120 seconds, max: 600 seconds
	timeoutMs := params.Timeout
	if timeoutMs <= 0 {
		timeoutMs = 120000
	}
	if timeoutMs > 600000 {
		timeoutMs = 600000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Determine user's shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	cmd := exec.CommandContext(ctx, shell, "-c", params.Command)
	cmd.Dir, _ = os.Getwd()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Inherit environment
	cmd.Env = os.Environ()

	err := cmd.Run()

	// Build output combining stdout and stderr
	var output strings.Builder
	if stdout.Len() > 0 {
		output.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		if output.Len() > 0 && !strings.HasSuffix(output.String(), "\n") {
			output.WriteString("\n")
		}
		output.WriteString(stderr.String())
	}

	result := output.String()

	// Determine exit code
	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result = fmt.Sprintf("Command timed out after %dms\n%s", timeoutMs, result)
			exitCode = -1
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ToolResult{}, fmt.Errorf("failed to execute command: %w", err)
		}
	}

	// Build preview lines with stdout/stderr differentiation.
	// Stdout lines are plain, stderr lines are prefixed with "stderr:" so
	// the renderer can color them red.
	var previewLines []string
	stdoutStr := strings.TrimRight(stdout.String(), "\n")
	stderrStr := strings.TrimRight(stderr.String(), "\n")

	if stdoutStr != "" {
		for _, line := range strings.Split(stdoutStr, "\n") {
			previewLines = append(previewLines, line)
		}
	}
	if stderrStr != "" {
		for _, line := range strings.Split(stderrStr, "\n") {
			previewLines = append(previewLines, "stderr:"+line)
		}
	}

	if len(previewLines) > 5 {
		previewLines = previewLines[:5]
		previewLines = append(previewLines, "...")
	}

	message := fmt.Sprintf("Exit code: %d", exitCode)
	if exitCode != 0 {
		message = fmt.Sprintf("Exit code: %d (error)", exitCode)
	}

	eventsCh <- internal.Event{
		Name:        b.Name(),
		Args:        []string{params.Description, params.Command},
		Message:     message,
		Preview:     previewLines,
		PreviewType: internal.PreviewBash,
		IsError:     exitCode != 0,
	}

	// Prepend exit code info if non-zero
	if exitCode != 0 {
		result = fmt.Sprintf("Exit code: %d\n%s", exitCode, result)
	}

	return ToolResult{
		Content: result,
	}, nil
}
