package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/sazid/bitcode/internal"
)

type WriteInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type WriteTool struct{}

var _ Tool = (*WriteTool)(nil)

func (w *WriteTool) Name() string {
	return "Write"
}

func (w *WriteTool) Description() string {
	return `Write content to a file, creating or replacing it.

When to use this:
- Use Write when you need to create a new file or replace the full contents of a file.
- Prefer Edit for smaller in-place changes to existing files.
- Avoid Write when a targeted patch would be safer or easier to review.

Important:
- Supports both absolute and relative paths.
- Relative paths are resolved from the current working directory.
- Parent directories are created automatically if needed.
- Writes files only, not directories.

Parameters:
- file_path (required): Path to the file.
- content (required): Full file contents to write.`
}

func (w *WriteTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file (absolute or relative to current working directory)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		"required": []string{"file_path", "content"},
	}
}

func (w *WriteTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params WriteInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to get working directory: %w", err)
	}

	fullPath := params.FilePath
	if !filepath.IsAbs(fullPath) {
		fullPath = path.Join(wd, fullPath)
	}

	cleanPath := filepath.Clean(fullPath)
	if strings.Contains(cleanPath, "..") {
		return ToolResult{}, fmt.Errorf("file_path cannot contain '..' for security reasons")
	}

	previousContent := ""
	if buf, readErr := os.ReadFile(cleanPath); readErr == nil {
		previousContent = string(buf)
	} else if !os.IsNotExist(readErr) {
		return ToolResult{}, fmt.Errorf("failed to read existing file: %w", readErr)
	}

	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return ToolResult{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	if err := os.WriteFile(cleanPath, []byte(params.Content), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	lineCount := int64(strings.Count(params.Content, "\n"))
	if len(params.Content) > 0 && !strings.HasSuffix(params.Content, "\n") {
		lineCount++
	}

	previewLines := buildDiffPreview(previewPathForDiff(wd, cleanPath), previousContent, params.Content, 6)
	info := fmt.Sprintf("Wrote %d lines", lineCount)

	eventsCh <- internal.Event{
		Name:        w.Name(),
		Args:        []string{cleanPath},
		Message:     info,
		Preview:     previewLines,
		PreviewType: internal.PreviewDiff,
	}

	return ToolResult{
		Content: fmt.Sprintf("Successfully wrote %d lines to %s", lineCount, cleanPath),
	}, nil
}
