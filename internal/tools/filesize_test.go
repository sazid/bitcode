package tools

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sazid/bitcode/internal"
)

func executeFileSize(t *testing.T, input FileSizeInput) (ToolResult, error) {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("failed to marshal input: %v", err)
	}
	tool := &FileSizeTool{}
	ch := makeEventsCh()
	result, err := tool.Execute(raw, ch)
	close(ch)
	return result, err
}

func TestFileSizeTool_GetsSize(t *testing.T) {
	content := "Hello, World!"
	path := writeTempFile(t, content)
	result, err := executeFileSize(t, FileSizeInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 13 bytes
	if !strings.Contains(result.Content, "13") {
		t.Errorf("expected 13 bytes, got: %q", result.Content)
	}
}

func TestFileSizeTool_EmptyFile(t *testing.T) {
	path := writeTempFile(t, "")
	result, err := executeFileSize(t, FileSizeInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "0") {
		t.Errorf("expected 0 bytes, got: %q", result.Content)
	}
}

func TestFileSizeTool_LargeFile(t *testing.T) {
	// Create a 1MB file
	content := strings.Repeat("x", 1024*1024)
	path := writeTempFile(t, content)
	result, err := executeFileSize(t, FileSizeInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Check for 1.00 MB in human-readable format
	if !strings.Contains(result.Content, "1.00 MB") && !strings.Contains(result.Content, "1048576") {
		t.Errorf("expected ~1MB size, got: %q", result.Content)
	}
}

func TestFileSizeTool_KBSize(t *testing.T) {
	// Create a 5KB file
	content := strings.Repeat("a", 5*1024)
	path := writeTempFile(t, content)
	result, err := executeFileSize(t, FileSizeInput{FilePath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content, "5") {
		t.Errorf("expected 5KB size, got: %q", result.Content)
	}
}

func TestFileSizeTool_FileNotFound(t *testing.T) {
	_, err := executeFileSize(t, FileSizeInput{FilePath: "/nonexistent/path/to/file.txt"})
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestFileSizeTool_DirectoryError(t *testing.T) {
	// Try to get size of a directory
	dir := t.TempDir()
	_, err := executeFileSize(t, FileSizeInput{FilePath: dir})
	if err == nil {
		t.Fatal("expected error for directory, got nil")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected directory error, got: %v", err)
	}
}

func TestFileSizeTool_PathTraversal(t *testing.T) {
	_, err := executeFileSize(t, FileSizeInput{FilePath: "../../etc/passwd"})
	// Should fail due to path traversal check
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
}

func TestFileSizeTool_EmitsEvent(t *testing.T) {
	content := "test content"
	path := writeTempFile(t, content)
	raw, _ := json.Marshal(FileSizeInput{FilePath: path})
	tool := &FileSizeTool{}
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
	if events[0].Name != "FileSize" {
		t.Errorf("expected event name 'FileSize', got %q", events[0].Name)
	}
	if events[0].Message == "" {
		t.Error("expected event message to not be empty")
	}
}

func TestFileSizeTool_RelativePath(t *testing.T) {
	dir := t.TempDir()
	filePath := dir + "/relative_test.txt"
	if err := os.WriteFile(filePath, []byte("content"), 0o600); err != nil {
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

	result, err := executeFileSize(t, FileSizeInput{FilePath: "relative_test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "content" = 7 bytes
	if !strings.Contains(result.Content, "7") {
		t.Errorf("expected 7 bytes, got: %q", result.Content)
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.00 KB"},
		{1024 * 1024, "1.00 MB"},
		{1024 * 1024 * 1024, "1.00 GB"},
		{1536, "1.50 KB"}, // 1.5 KB
	}

	for _, tt := range tests {
		result := formatSize(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, result, tt.expected)
		}
	}
}
