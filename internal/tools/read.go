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

type ReadInput struct {
	FilePath string `json:"file_path"`
	Offset   int64  `json:"offset,omitempty"`
	Limit    int64  `json:"limit,omitempty"`
}

type ReadTool struct{}

var _ Tool = (*ReadTool)(nil)

func (r *ReadTool) Name() string {
	return "Read"
}

func (r *ReadTool) Description() string {
	return `Read a file from the local filesystem.

When to use this:
- Use Read when you already know the file path and need to inspect its contents before editing or reasoning about code.
- Prefer Read over shell commands like cat, head, or tail.
- Use offset and limit for large files instead of reading everything at once.

Important:
- Supports both absolute and relative paths.
- Relative paths are resolved from the current working directory.
- Can read images, PDFs, and notebooks.
- Reads files only, not directories.
- Returns content with line numbers starting from 1.

Parameters:
- file_path (required): Path to the file.
- offset (optional): Zero-based starting line.
- limit (optional): Number of lines to read.`
}

func (r *ReadTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file (absolute or relative to current working directory)",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "The line number to start reading from (default: 0)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "The number of lines to read",
			},
		},
		"required": []string{"file_path"},
	}
}

func (r *ReadTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params ReadInput
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

	buf, err := os.ReadFile(cleanPath)

	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{}, fmt.Errorf("file does not exist: %w", err)
		} else {
			return ToolResult{}, fmt.Errorf("failed to read file: %w", err)
		}
	}

	lines := strings.Split(string(buf), "\n")

	start := max(params.Offset, 0)
	start = min(start, int64(len(lines)))
	end := start
	if params.Limit == 0 {
		end = int64(len(lines))
	} else {
		end = min(start+params.Limit, int64(len(lines)))
	}
	lineCount := end - start

	var content strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&content, "%5d\t%s\n", i+1, lines[i])
	}

	info := fmt.Sprintf("Read %d lines", lineCount)
	lineRange := formatReadRange(start, end)

	eventsCh <- internal.Event{
		Name:    r.Name(),
		Args:    []string{cleanPath, lineRange},
		Message: info,
	}

	return ToolResult{
		Content: content.String(),
	}, nil
}

func formatReadRange(start, end int64) string {
	if end <= start {
		return "empty"
	}
	if end-start == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d-%d", start+1, end)
}
