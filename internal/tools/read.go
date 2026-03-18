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
	return `Reads a file from local filesystem.

  IMPORTANT:
  - Supports both absolute and relative paths
  - Relative paths are resolved from the current working directory
  - This tool can read images (PNG, JPG, etc.), PDFs, and Jupyter notebooks
  - For images, contents will be presented visually since this is a multimodal LLM
  - This tool can only read files, not directories
  - Returns content with line numbers starting from 1

  Parameters:
  - file_path (required): The path to the file (absolute or relative)
  - offset (optional): The line number to start reading from (default: 0)
  - limit (optional): The number of lines to read (default: read entire file)`
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

	eventsCh <- internal.Event{
		Name:    r.Name(),
		Args:    []string{cleanPath},
		Message: info,
	}

	return ToolResult{
		Content: content.String(),
	}, nil
}
