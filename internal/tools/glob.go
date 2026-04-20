package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sazid/bitcode/internal"
)

type GlobInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type GlobTool struct{}

var _ Tool = (*GlobTool)(nil)

func (g *GlobTool) Name() string {
	return "Glob"
}

func (g *GlobTool) Description() string {
	return `Find files by path pattern.

When to use this:
- Use Glob to discover candidate files or directories when you do not know the exact path yet.
- Prefer Glob over shell commands like find or ls for file discovery.
- Use Read after Glob once you know the exact file you want to inspect.

Important:
- Supports glob patterns like "**/*.go" and "src/**/*.ts".
- path sets the directory to search in and defaults to the current working directory.
- Returns matching file paths sorted by modification time, newest first.
- Searches file paths, not file contents.

Parameters:
- pattern (required): Glob pattern to match.
- path (optional): Directory to search from.`
}

func (g *GlobTool) ParametersSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": `Glob pattern to match files against (e.g. "**/*.go", "src/**/*.ts")`,
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search in (absolute or relative to cwd). Defaults to cwd.",
			},
		},
		"required": []string{"pattern"},
	}
}

func (g *GlobTool) Execute(input json.RawMessage, eventsCh chan<- internal.Event) (ToolResult, error) {
	var params GlobInput
	if err := json.Unmarshal(input, &params); err != nil {
		return ToolResult{}, fmt.Errorf("invalid input: %w", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to get working directory: %w", err)
	}

	searchRoot := wd
	if params.Path != "" {
		if filepath.IsAbs(params.Path) {
			searchRoot = params.Path
		} else {
			searchRoot = filepath.Join(wd, params.Path)
		}
	}
	searchRoot = filepath.Clean(searchRoot)
	if strings.Contains(searchRoot, "..") {
		return ToolResult{}, fmt.Errorf("path cannot contain '..' for security reasons")
	}

	type fileEntry struct {
		path    string
		modTime int64
	}

	var matches []fileEntry

	err = filepath.WalkDir(searchRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}

		// Match against the pattern using the path relative to searchRoot so
		// patterns like "**/*.go" work correctly regardless of absolute prefix.
		rel, err := filepath.Rel(searchRoot, p)
		if err != nil {
			return nil
		}

		matched, err := doubleStarMatch(params.Pattern, rel)
		if err != nil {
			return fmt.Errorf("invalid pattern %q: %w", params.Pattern, err)
		}
		if !matched {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		matches = append(matches, fileEntry{path: p, modTime: info.ModTime().UnixNano()})
		return nil
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("failed to walk directory: %w", err)
	}

	// Sort by modification time, most recent first.
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].modTime > matches[j].modTime
	})

	paths := make([]string, len(matches))
	for i, m := range matches {
		paths[i] = m.path
	}

	content := strings.Join(paths, "\n")

	previewPaths := paths
	if len(previewPaths) > 5 {
		previewPaths = previewPaths[:5]
	}
	previewLines := make([]string, len(previewPaths))
	copy(previewLines, previewPaths)
	if len(paths) > 5 {
		previewLines = append(previewLines, "...")
	}

	info := fmt.Sprintf("Found %d files", len(paths))

	eventsCh <- internal.Event{
		Name:        g.Name(),
		Args:        []string{params.Pattern},
		Message:     info,
		Preview:     previewLines,
		PreviewType: internal.PreviewFileList,
	}

	return ToolResult{
		Content: content,
	}, nil
}

// doubleStarMatch extends filepath.Match to support "**" which matches any
// number of path segments (including zero).
func doubleStarMatch(pattern, name string) (bool, error) {
	// Fast path: no "**" — delegate straight to filepath.Match.
	if !strings.Contains(pattern, "**") {
		return filepath.Match(pattern, name)
	}

	// Split both into segments and do recursive matching.
	patParts := strings.Split(filepath.ToSlash(pattern), "/")
	nameParts := strings.Split(filepath.ToSlash(name), "/")
	return matchSegments(patParts, nameParts)
}

func matchSegments(pat, name []string) (bool, error) {
	for len(pat) > 0 {
		if pat[0] == "**" {
			pat = pat[1:]
			if len(pat) == 0 {
				// "**" at the end matches everything remaining.
				return true, nil
			}
			// Try matching pat against every suffix of name.
			for i := 0; i <= len(name); i++ {
				ok, err := matchSegments(pat, name[i:])
				if err != nil {
					return false, err
				}
				if ok {
					return true, nil
				}
			}
			return false, nil
		}

		if len(name) == 0 {
			return false, nil
		}

		ok, err := filepath.Match(pat[0], name[0])
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}

		pat = pat[1:]
		name = name[1:]
	}

	return len(name) == 0, nil
}
