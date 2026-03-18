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

type FileSizeInput struct {
	FilePath string `json:"file_path"`
}

type FileSizeTool struct{}

var _ Tool = (*FileSizeTool)(nil)

func (f *FileSizeTool) Name() string {
	return "FileSize"
}

func (f *FileSizeTool) Description() string {
	return `Gets the size of a file in bytes.

IMPORTANT:
- Use this tool BEFORE reading files to assess their size and avoid wasting context.
- Supports both absolute and relative paths.
- Returns size in bytes and human-readable format (KB, MB, GB).
- This tool can only get file sizes, not directory sizes.

Parameters:
- file_path (required): The path to the file (absolute or relative to current working directory)`
}

func (f *FileSizeTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file (absolute or relative to current working directory)",
			},
		},
		"required": []string{"file_path"},
	}
}

func (f *FileSizeTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params FileSizeInput
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

	stat, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{}, fmt.Errorf("file does not exist: %w", err)
		}
		return ToolResult{}, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.IsDir() {
		return ToolResult{}, fmt.Errorf("path is a directory, not a file: %s", cleanPath)
	}

	size := stat.Size()
	humanReadable := formatSize(size)

	info := fmt.Sprintf("%s (%d bytes)", humanReadable, size)
	eventsCh <- internal.Event{
		Name:    f.Name(),
		Args:    []string{cleanPath},
		Message: info,
	}

	return ToolResult{
		Content: fmt.Sprintf("%d bytes (%s) %s", size, humanReadable, cleanPath),
	}, nil
}

// formatSize converts a size in bytes to a human-readable string.
func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
