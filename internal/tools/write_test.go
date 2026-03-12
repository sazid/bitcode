package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func executeWrite(t *testing.T, input WriteInput) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	tool := &WriteTool{}
	ch := make(chan internal.Event, 10)
	result, err := tool.Execute(raw, ch)
	close(ch)
	return result, err
}

func TestWriteTool_CreatesNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.txt")
	_, err := executeWrite(t, WriteInput{FilePath: path, Content: "hello\n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("got %q, want %q", string(got), "hello\n")
	}
}

func TestWriteTool_OverwritesExistingFile(t *testing.T) {
	path := writeTempFile(t, "original content\n")
	_, err := executeWrite(t, WriteInput{FilePath: path, Content: "new content\n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != "new content\n" {
		t.Errorf("got %q, want %q", string(got), "new content\n")
	}
}

func TestWriteTool_CreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c", "file.txt")
	_, err := executeWrite(t, WriteInput{FilePath: path, Content: "deep\n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(got) != "deep\n" {
		t.Errorf("got %q, want %q", string(got), "deep\n")
	}
}

func TestWriteTool_LineCountWithTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lines.txt")
	result, err := executeWrite(t, WriteInput{FilePath: path, Content: "a\nb\nc\n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "3 lines") {
		t.Errorf("expected 3 lines in content, got %q", result.Content)
	}
}

func TestWriteTool_LineCountWithoutTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noeol.txt")
	result, err := executeWrite(t, WriteInput{FilePath: path, Content: "a\nb\nc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "3 lines") {
		t.Errorf("expected 3 lines in content, got %q", result.Content)
	}
}

func TestWriteTool_EmptyContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	result, err := executeWrite(t, WriteInput{FilePath: path, Content: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "0 lines") {
		t.Errorf("expected 0 lines in content, got %q", result.Content)
	}

	got, _ := os.ReadFile(path)
	if len(got) != 0 {
		t.Errorf("expected empty file, got %q", string(got))
	}
}

func TestWriteTool_EmitsEvent(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "event.txt")
	raw, _ := json.Marshal(WriteInput{FilePath: filePath, Content: "line\n"})
	tool := &WriteTool{}
	ch := make(chan internal.Event, 10)
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
	if events[0].Name != "Write" {
		t.Errorf("expected event name 'Write', got %q", events[0].Name)
	}
	if len(events[0].Args) == 0 || events[0].Args[0] != filepath.Clean(filePath) {
		t.Errorf("expected event arg %q, got %v", filePath, events[0].Args)
	}
}

func TestWriteTool_RelativePath(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) }) //nolint:errcheck
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, err = executeWrite(t, WriteInput{FilePath: "relative.txt", Content: "relative write\n"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "relative.txt"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(got), "relative write") {
		t.Errorf("unexpected content: %q", string(got))
	}
}
