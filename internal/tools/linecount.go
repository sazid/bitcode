package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/sazid/bitcode/internal"
)

type LineCountInput struct {
	FilePath string `json:"file_path"`
}

type LineCountTool struct{}

var _ Tool = (*LineCountTool)(nil)

func (l *LineCountTool) Name() string {
	return "LineCount"
}

func (l *LineCountTool) Description() string {
	return `Counts the number of lines in a file efficiently.

IMPORTANT:
- Use this tool BEFORE reading files to assess their size and avoid wasting context.
- This tool is highly optimized for speed using SIMD instructions (AVX2/SSE on x86, NEON on ARM).
- Supports both absolute and relative paths.
- Returns line count and file path.

Parameters:
- file_path (required): The path to the file (absolute or relative to current working directory)`
}

func (l *LineCountTool) ParametersSchema() map[string]any {
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

func (l *LineCountTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params LineCountInput
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

	// Open the file
	f, err := os.Open(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{}, fmt.Errorf("file does not exist: %w", err)
		}
		return ToolResult{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Disable GC for this specific workload since we only allocate a single reusable buffer.
	// This prevents GC pauses during counting of huge files (GBs).
	debug.SetGCPercent(-1)
	defer debug.SetGCPercent(100) // Restore default

	// Use a 1MB buffer - optimal balance between syscall overhead and cache locality.
	buf := make([]byte, 1024*1024)
	lineSep := []byte{'\n'}
	count := 0

	for {
		n, err := f.Read(buf)
		if err != nil && err != io.EOF {
			return ToolResult{}, fmt.Errorf("failed to read file: %w", err)
		}

		// bytes.Count uses SIMD instructions internally (AVX2/SSE on x86, NEON on ARM).
		// It counts newlines much faster than a Go for-loop.
		count += bytes.Count(buf[:n], lineSep)

		if err == io.EOF {
			break
		}
	}

	// Check if file ends with newline - if not, the last line is still counted
	// but we need to get accurate count by checking the last byte
	// Actually, bytes.Count gives us number of newlines, which equals number of lines
	// for files that end with newline. For files without trailing newline, we have
	// count-1 lines visually. Let's stat to check file end.
	stat, err := f.Stat()
	if err == nil && stat.Size() > 0 {
		// Read last byte to check if file ends with newline
		_, err = f.Seek(-1, io.SeekEnd)
		if err == nil {
			lastByte := make([]byte, 1)
			_, err = f.Read(lastByte)
			if err == nil && lastByte[0] != '\n' {
				// File doesn't end with newline, so there's one more line
				count++
			}
		}
	}

	info := fmt.Sprintf("%d lines", count)
	eventsCh <- internal.Event{
		Name:    l.Name(),
		Args:    []string{cleanPath},
		Message: info,
	}

	return ToolResult{
		Content: fmt.Sprintf("%d %s", count, cleanPath),
	}, nil
}
