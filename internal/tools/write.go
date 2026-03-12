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
	return `Writes content to a file on the local filesystem, creating it if it does not exist
or overwriting it if it does.

  IMPORTANT:
  - Supports both absolute and relative paths
  - Relative paths are resolved from the current working directory
  - Parent directories are created automatically if they do not exist
  - This tool can only write files, not directories

  Parameters:
  - file_path (required): The path to the file (absolute or relative)
  - content (required): The content to write to the file`
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

	contentLines := strings.Split(params.Content, "\n")
	previewCount := min(5, len(contentLines))
	previewLines := make([]string, previewCount)
	for i := 0; i < previewCount; i++ {
		previewLines[i] = fmt.Sprintf("%5d\t%s", i+1, contentLines[i])
	}
	if len(contentLines) > previewCount {
		previewLines = append(previewLines, "...")
	}

	info := fmt.Sprintf("Wrote %d lines", lineCount)

	eventsCh <- internal.Event{
		Name:        w.Name(),
		Args:        []string{cleanPath},
		Message:     info,
		Preview:     previewLines,
		PreviewType: internal.PreviewCode,
	}

	return ToolResult{
		Content: fmt.Sprintf("Successfully wrote %d lines to %s", lineCount, cleanPath),
	}, nil
}
