package tools

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func executeBash(t *testing.T, input BashInput) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	tool := &BashTool{}
	ch := makeEventsCh()
	result, err := tool.Execute(raw, ch)
	close(ch)
	return result, err
}

func TestBashTool_SimpleCommand(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "echo hello",
		Description: "Print hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Content) != "hello" {
		t.Errorf("expected 'hello', got: %q", result.Content)
	}
}

func TestBashTool_MultilineOutput(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "echo 'line1\nline2\nline3'",
		Description: "Print multiple lines",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "line1") || !strings.Contains(result.Content, "line3") {
		t.Errorf("expected multiline output, got: %q", result.Content)
	}
}

func TestBashTool_ExitCodeZero(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "true",
		Description: "Run true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Exit code 0 should NOT prepend exit code info
	if strings.Contains(result.Content, "Exit code:") {
		t.Errorf("exit code 0 should not prepend exit info, got: %q", result.Content)
	}
}

func TestBashTool_NonZeroExitCode(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "exit 42",
		Description: "Exit with code 42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "Exit code: 42") {
		t.Errorf("expected 'Exit code: 42' in output, got: %q", result.Content)
	}
}

func TestBashTool_StderrOutput(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "echo error_msg >&2",
		Description: "Write to stderr",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "error_msg") {
		t.Errorf("expected stderr output in result, got: %q", result.Content)
	}
}

func TestBashTool_CombinedStdoutStderr(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "echo out && echo err >&2",
		Description: "Combined stdout and stderr",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "out") || !strings.Contains(result.Content, "err") {
		t.Errorf("expected both stdout and stderr, got: %q", result.Content)
	}
}

func TestBashTool_StderrPreviewPrefix(t *testing.T) {
	raw, _ := json.Marshal(BashInput{
		Command:     "echo out && echo err_line >&2",
		Description: "Stdout and stderr preview",
	})
	tool := &BashTool{}
	ch := makeEventsCh()
	_, err := tool.Execute(raw, ch)
	close(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []internal.Event
	for e := range ch {
		events = append(events, e)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].PreviewType != internal.PreviewBash {
		t.Errorf("expected PreviewBash type, got %q", events[0].PreviewType)
	}

	// Stdout line should be plain, stderr line should be prefixed with "stderr:"
	foundStdout := false
	foundStderr := false
	for _, line := range events[0].Preview {
		if line == "out" {
			foundStdout = true
		}
		if line == "stderr:err_line" {
			foundStderr = true
		}
	}
	if !foundStdout {
		t.Errorf("expected plain stdout line 'out' in preview, got %v", events[0].Preview)
	}
	if !foundStderr {
		t.Errorf("expected 'stderr:err_line' in preview, got %v", events[0].Preview)
	}
}

func TestBashTool_EmptyCommand(t *testing.T) {
	_, err := executeBash(t, BashInput{
		Command:     "",
		Description: "Empty command",
	})
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
	if !strings.Contains(err.Error(), "command is required") {
		t.Errorf("expected 'command is required' error, got: %v", err)
	}
}

func TestBashTool_Timeout(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "sleep 10",
		Description: "Sleep that should timeout",
		Timeout:     500, // 500ms timeout
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "timed out") {
		t.Errorf("expected timeout message, got: %q", result.Content)
	}
}

func TestBashTool_TimeoutClamped(t *testing.T) {
	// Verify that timeout > 600000 is clamped (we can't easily test the actual
	// clamping without waiting, but we verify it doesn't error)
	result, err := executeBash(t, BashInput{
		Command:     "echo ok",
		Description: "Test timeout clamping",
		Timeout:     999999,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "ok") {
		t.Errorf("expected 'ok', got: %q", result.Content)
	}
}

func TestBashTool_PipedCommands(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "echo 'hello world' | tr ' ' '_'",
		Description: "Pipe echo to tr",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Content) != "hello_world" {
		t.Errorf("expected 'hello_world', got: %q", result.Content)
	}
}

func TestBashTool_ChainedCommands(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "echo first && echo second",
		Description: "Chained commands",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "first") || !strings.Contains(result.Content, "second") {
		t.Errorf("expected both outputs, got: %q", result.Content)
	}
}

func TestBashTool_EnvironmentVariables(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "echo $HOME",
		Description: "Print HOME env var",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Content) == "" || strings.TrimSpace(result.Content) == "$HOME" {
		t.Errorf("expected HOME to be expanded, got: %q", result.Content)
	}
}

func TestBashTool_WorkingDirectory(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "pwd",
		Description: "Print working directory",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(result.Content) == "" {
		t.Error("expected non-empty working directory")
	}
}

func TestBashTool_EmitsEvent(t *testing.T) {
	raw, _ := json.Marshal(BashInput{
		Command:     "echo test",
		Description: "Test event emission",
	})
	tool := &BashTool{}
	ch := makeEventsCh()
	_, err := tool.Execute(raw, ch)
	close(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []internal.Event
	for e := range ch {
		events = append(events, e)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Name != "Bash" {
		t.Errorf("expected event name 'Bash', got %q", events[0].Name)
	}
	if events[0].Message != "Exit code: 0" {
		t.Errorf("expected 'Exit code: 0', got %q", events[0].Message)
	}
	if len(events[0].Args) == 0 || events[0].Args[0] != "Test event emission" {
		t.Errorf("expected description in event args, got %v", events[0].Args)
	}
}

func TestBashTool_EventShowsErrorExitCode(t *testing.T) {
	raw, _ := json.Marshal(BashInput{
		Command:     "exit 1",
		Description: "Failing command",
	})
	tool := &BashTool{}
	ch := makeEventsCh()
	_, err := tool.Execute(raw, ch)
	close(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []internal.Event
	for e := range ch {
		events = append(events, e)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if !strings.Contains(events[0].Message, "(error)") {
		t.Errorf("expected error indicator in event message, got %q", events[0].Message)
	}
}

func TestBashTool_PreviewTruncation(t *testing.T) {
	// Generate more than 5 lines of output
	raw, _ := json.Marshal(BashInput{
		Command:     "seq 1 10",
		Description: "Generate 10 lines",
	})
	tool := &BashTool{}
	ch := makeEventsCh()
	_, err := tool.Execute(raw, ch)
	close(ch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []internal.Event
	for e := range ch {
		events = append(events, e)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	// Preview should be 5 lines + "..."
	if len(events[0].Preview) != 6 {
		t.Errorf("expected 6 preview lines (5 + ...), got %d: %v", len(events[0].Preview), events[0].Preview)
	}
	if events[0].Preview[5] != "..." {
		t.Errorf("expected last preview line to be '...', got %q", events[0].Preview[5])
	}
}

func TestBashTool_UsesUserShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test not applicable on Windows")
	}
	// The command should execute using whatever shell is set
	result, err := executeBash(t, BashInput{
		Command:     "echo $0",
		Description: "Print shell name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := strings.TrimSpace(result.Content)
	if output == "" {
		t.Error("expected shell name in output")
	}
}

func TestBashTool_InvalidJSON(t *testing.T) {
	tool := &BashTool{}
	ch := makeEventsCh()
	_, err := tool.Execute(json.RawMessage(`{invalid`), ch)
	close(ch)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid input") {
		t.Errorf("expected 'invalid input' error, got: %v", err)
	}
}

func TestBashTool_FailedCommand(t *testing.T) {
	result, err := executeBash(t, BashInput{
		Command:     "ls /nonexistent_directory_12345",
		Description: "List nonexistent directory",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have non-zero exit code
	if !strings.Contains(result.Content, "Exit code:") {
		t.Errorf("expected exit code in output for failed command, got: %q", result.Content)
	}
}
