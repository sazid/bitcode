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

type EditInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type EditTool struct{}

var _ Tool = (*EditTool)(nil)

func (e *EditTool) Name() string {
	return "Edit"
}

func (e *EditTool) Description() string {
	return `Performs exact string replacement in a file.

  IMPORTANT:
  - Supports both absolute and relative paths
  - Relative paths are resolved from the current working directory
  - old_string must match the file content exactly (including whitespace and indentation)
  - The edit will fail if old_string is not found in the file
  - By default only the first occurrence is replaced; set replace_all to true to replace every occurrence

  Parameters:
  - file_path (required): The path to the file (absolute or relative)
  - old_string (required): The exact text to find and replace
  - new_string (required): The text to replace it with
  - replace_all (optional): Replace all occurrences instead of just the first (default: false)`
}

func (e *EditTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file (absolute or relative to current working directory)",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact text to find and replace",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The text to replace it with",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences instead of just the first (default: false)",
			},
		},
		"required": []string{"file_path", "old_string", "new_string"},
	}
}

func (e *EditTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params EditInput
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
		}
		return ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	original := string(buf)
	if !strings.Contains(original, params.OldString) {
		return ToolResult{}, fmt.Errorf("old_string not found in file")
	}

	var updated string
	var replacements int
	if params.ReplaceAll {
		replacements = strings.Count(original, params.OldString)
		updated = strings.ReplaceAll(original, params.OldString, params.NewString)
	} else {
		replacements = 1
		updated = strings.Replace(original, params.OldString, params.NewString, 1)
	}

	if err := os.WriteFile(cleanPath, []byte(updated), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	oldLines := strings.Split(params.OldString, "\n")
	newLines := strings.Split(params.NewString, "\n")

	var previewLines []string
	maxPreview := 5
	for i, line := range oldLines {
		if i >= maxPreview {
			previewLines = append(previewLines, "...")
			break
		}
		previewLines = append(previewLines, "-"+line)
	}
	for i, line := range newLines {
		if i >= maxPreview {
			previewLines = append(previewLines, "...")
			break
		}
		previewLines = append(previewLines, "+"+line)
	}

	msg := fmt.Sprintf("Replaced %d occurrence(s)", replacements)

	eventsCh <- internal.Event{
		Name:        e.Name(),
		Args:        []string{cleanPath},
		Message:     msg,
		Preview:     previewLines,
		PreviewType: internal.PreviewDiff,
	}

	return ToolResult{
		Content: msg,
	}, nil
}
