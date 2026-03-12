package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func executeEdit(t *testing.T, input EditInput) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	tool := &EditTool{}
	ch := make(chan internal.Event, 10)
	result, err := tool.Execute(raw, ch)
	close(ch)
	return result, err
}

func TestEditTool_BasicReplacement(t *testing.T) {
	path := writeTempFile(t, "hello world\n")
	_, err := executeEdit(t, EditInput{FilePath: path, OldString: "world", NewString: "Go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "hello Go\n" {
		t.Errorf("got %q, want %q", string(got), "hello Go\n")
	}
}

func TestEditTool_ReplacesOnlyFirstOccurrenceByDefault(t *testing.T) {
	path := writeTempFile(t, "foo foo foo\n")
	_, err := executeEdit(t, EditInput{FilePath: path, OldString: "foo", NewString: "bar"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "bar foo foo\n" {
		t.Errorf("got %q, want %q", string(got), "bar foo foo\n")
	}
}

func TestEditTool_ReplaceAll(t *testing.T) {
	path := writeTempFile(t, "foo foo foo\n")
	result, err := executeEdit(t, EditInput{FilePath: path, OldString: "foo", NewString: "bar", ReplaceAll: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "bar bar bar\n" {
		t.Errorf("got %q, want %q", string(got), "bar bar bar\n")
	}
	if result.Content != "Replaced 3 occurrence(s)" {
		t.Errorf("expected replacement message, got %q", result.Content)
	}
}

func TestEditTool_OldStringNotFound(t *testing.T) {
	path := writeTempFile(t, "hello world\n")
	_, err := executeEdit(t, EditInput{FilePath: path, OldString: "notpresent", NewString: "x"})
	if err == nil {
		t.Fatal("expected error when old_string not found, got nil")
	}
}

func TestEditTool_FileNotFound(t *testing.T) {
	_, err := executeEdit(t, EditInput{FilePath: "/nonexistent/file.txt", OldString: "x", NewString: "y"})
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestEditTool_PreservesRestOfFile(t *testing.T) {
	content := "line1\nline2\nline3\n"
	path := writeTempFile(t, content)
	_, err := executeEdit(t, EditInput{FilePath: path, OldString: "line2", NewString: "replaced"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "line1\nreplaced\nline3\n" {
		t.Errorf("got %q, want %q", string(got), "line1\nreplaced\nline3\n")
	}
}

func TestEditTool_MultilineReplacement(t *testing.T) {
	content := "func foo() {\n\treturn 1\n}\n"
	path := writeTempFile(t, content)
	_, err := executeEdit(t, EditInput{
		FilePath:  path,
		OldString: "func foo() {\n\treturn 1\n}",
		NewString: "func foo() {\n\treturn 42\n}",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "func foo() {\n\treturn 42\n}\n" {
		t.Errorf("got %q, want %q", string(got), "func foo() {\n\treturn 42\n}\n")
	}
}

func TestEditTool_EmitsEvent(t *testing.T) {
	filePath := writeTempFile(t, "abc\n")
	raw, _ := json.Marshal(EditInput{FilePath: filePath, OldString: "abc", NewString: "xyz"})
	tool := &EditTool{}
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
	if events[0].Name != "Edit" {
		t.Errorf("expected event name 'Edit', got %q", events[0].Name)
	}
	if len(events[0].Args) == 0 || events[0].Args[0] != filepath.Clean(filePath) {
		t.Errorf("expected event arg %q, got %v", filePath, events[0].Args)
	}
}

func TestEditTool_RelativePath(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) }) //nolint:errcheck
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "rel.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err = executeEdit(t, EditInput{FilePath: "rel.txt", OldString: "before", NewString: "after"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(dir, "rel.txt"))
	if string(got) != "after\n" {
		t.Errorf("got %q, want %q", string(got), "after\n")
	}
}
