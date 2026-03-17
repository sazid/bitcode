package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func executeLineCount(t *testing.T, input LineCountInput) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	tool := &LineCountTool{}
	ch := makeEventsCh()
	result, err := tool.Execute(raw, ch)
	close(ch)
	return result, err
}

func TestLineCountTool_CountsLines(t *testing.T) {
	content := "line1\nline2\nline3\n"
	path := writeTempFile(t, content)
	result, err := executeLineCount(t, LineCountInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 3 newlines = 3 lines
	if !strings.Contains(result.Content, "3") {
		t.Errorf("expected 3 lines, got: %q", result.Content)
	}
}

func TestLineCountTool_FileWithoutNewline(t *testing.T) {
	content := "line1\nline2\nline3" // No trailing newline
	path := writeTempFile(t, content)
	result, err := executeLineCount(t, LineCountInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 newlines + 1 line without newline = 3 lines
	if !strings.Contains(result.Content, "3") {
		t.Errorf("expected 3 lines, got: %q", result.Content)
	}
}

func TestLineCountTool_EmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	result, err := executeLineCount(t, LineCountInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 0 lines
	if !strings.Contains(result.Content, "0") {
		t.Errorf("expected 0 lines, got: %q", result.Content)
	}
}

func TestLineCountTool_SingleLineNoNewline(t *testing.T) {
	content := "single line"
	path := writeTempFile(t, content)
	result, err := executeLineCount(t, LineCountInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1 line without newline = 1 line
	if !strings.Contains(result.Content, "1") {
		t.Errorf("expected 1 line, got: %q", result.Content)
	}
}

func TestLineCountTool_LargeFile(t *testing.T) {
	// Create a file with 10000 lines
	var sb strings.Builder
	for i := 0; i < 10000; i++ {
		sb.WriteString("test line content\n")
	}
	path := writeTempFile(t, sb.String())
	result, err := executeLineCount(t, LineCountInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "10000") {
		t.Errorf("expected 10000 lines, got: %q", result.Content)
	}
}

func TestLineCountTool_FileNotFound(t *testing.T) {
	_, err := executeLineCount(t, LineCountInput{FilePath: "/nonexistent/path/to/file.txt"})
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestLineCountTool_PathTraversal(t *testing.T) {
	_, err := executeLineCount(t, LineCountInput{FilePath: "../../etc/passwd"})
	// Should fail due to path traversal check
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestLineCountTool_EmitsEvent(t *testing.T) {
	content := "line1\nline2\n"
	path := writeTempFile(t, content)
	raw, _ := json.Marshal(LineCountInput{FilePath: path})
	tool := &LineCountTool{}
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
	if events[0].Name != "LineCount" {
		t.Errorf("expected event name 'LineCount', got %q", events[0].Name)
	}
	if events[0].Message == "" {
		t.Error("expected event message to not be empty")
	}
}

func TestLineCountTool_RelativePath(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/relative_test.txt"
	if err := os.WriteFile(filePath, []byte("line1\nline2\n"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Change working directory so relative path resolves.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) }) //nolint:errcheck
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	result, err := executeLineCount(t, LineCountInput{FilePath: "relative_test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "2") {
		t.Errorf("expected 2 lines, got: %q", result.Content)
	}
}