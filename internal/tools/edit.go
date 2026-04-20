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

	previewLines := buildDiffPreview(previewPathForDiff(wd, cleanPath), original, updated, 8)
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

func buildDiffPreview(displayPath, beforeContent, afterContent string, maxChangedLines int) []string {
	beforeLines := previewContentLines(beforeContent)
	afterLines := previewContentLines(afterContent)

	prefix := 0
	for prefix < len(beforeLines) && prefix < len(afterLines) && beforeLines[prefix] == afterLines[prefix] {
		prefix++
	}

	beforeSuffix := len(beforeLines) - 1
	afterSuffix := len(afterLines) - 1
	for beforeSuffix >= prefix && afterSuffix >= prefix && beforeLines[beforeSuffix] == afterLines[afterSuffix] {
		beforeSuffix--
		afterSuffix--
	}

	const contextLines = 2
	beforeContextStart := maxInt(0, prefix-contextLines)
	afterContextStart := maxInt(0, prefix-contextLines)
	beforeContextEnd := minInt(len(beforeLines), beforeSuffix+1+contextLines)
	afterContextEnd := minInt(len(afterLines), afterSuffix+1+contextLines)

	oldCount := beforeContextEnd - beforeContextStart
	newCount := afterContextEnd - afterContextStart
	oldStart := hunkStartLine(beforeContextStart, oldCount)
	newStart := hunkStartLine(afterContextStart, newCount)

	previewLines := []string{
		fmt.Sprintf("--- %s", filepath.ToSlash(displayPath)),
		fmt.Sprintf("+++ %s", filepath.ToSlash(displayPath)),
		fmt.Sprintf("@@ -%s +%s @@", formatHunkRange(oldStart, oldCount), formatHunkRange(newStart, newCount)),
	}

	for _, line := range beforeLines[beforeContextStart:prefix] {
		previewLines = append(previewLines, " "+line)
	}
	previewLines = append(previewLines, truncatedDiffLines(beforeLines[prefix:beforeSuffix+1], "-", maxChangedLines)...)
	previewLines = append(previewLines, truncatedDiffLines(afterLines[prefix:afterSuffix+1], "+", maxChangedLines)...)
	for _, line := range afterLines[afterSuffix+1 : afterContextEnd] {
		previewLines = append(previewLines, " "+line)
	}

	return previewLines
}

func truncatedDiffLines(lines []string, prefix string, maxChangedLines int) []string {
	if len(lines) == 0 {
		return nil
	}
	if maxChangedLines <= 0 || len(lines) <= maxChangedLines {
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			out = append(out, prefix+line)
		}
		return out
	}

	out := make([]string, 0, maxChangedLines+1)
	for _, line := range lines[:maxChangedLines] {
		out = append(out, prefix+line)
	}
	out = append(out, "...")
	return out
}

func formatHunkRange(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

func hunkStartLine(startIndex, count int) int {
	if count == 0 {
		if startIndex == 0 {
			return 0
		}
		return startIndex
	}
	return startIndex + 1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func previewContentLines(content string) []string {
	trimmed := strings.TrimSuffix(content, "\n")
	if trimmed == "" {
		if content == "" {
			return nil
		}
		return []string{""}
	}
	return strings.Split(trimmed, "\n")
}

func previewPathForDiff(wd, cleanPath string) string {
	rel, err := filepath.Rel(wd, cleanPath)
	if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return cleanPath
}
