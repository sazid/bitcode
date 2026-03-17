package plugin

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// RawPlugin represents a plugin file loaded from disk.
type RawPlugin struct {
	ID       string
	Body     string         // post-frontmatter text for .md; empty for .yaml/.yml
	Source   string         // "user" or "project"
	Metadata map[string]any // frontmatter keys for .md; full YAML for .yaml/.yml
}

// LoadFiles scans plugin directories for a given subDir and returns raw plugins.
// Directories are scanned in precedence order: user-level < project-level,
// .agents < .claude < .bitcode within each level.
// Later entries with the same ID overwrite earlier ones.
func LoadFiles(subDir string) []RawPlugin {
	seen := make(map[string]RawPlugin)

	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	// User-level (lower precedence)
	if home != "" {
		for _, d := range BaseDirs {
			loadPluginDir(filepath.Join(home, d, subDir), "user", seen)
		}
	}

	// Project-level (higher precedence)
	for _, d := range BaseDirs {
		loadPluginDir(filepath.Join(wd, d, subDir), "project", seen)
	}

	result := make([]RawPlugin, 0, len(seen))
	for _, p := range seen {
		result = append(result, p)
	}
	return result
}

func loadPluginDir(dir, source string, seen map[string]RawPlugin) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".md" && ext != ".yaml" && ext != ".yml" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}

		content := string(data)
		id := strings.TrimSuffix(name, ext)
		var metadata map[string]any
		var body string

		if ext == ".yaml" || ext == ".yml" {
			// Pure YAML: entire content is metadata
			if err := yaml.Unmarshal([]byte(content), &metadata); err != nil {
				continue
			}
		} else {
			// Markdown: extract frontmatter + body
			metadata, body = ParseFrontmatter(content)
		}

		// ID can be overridden from frontmatter/metadata
		if fmID, _ := metadata["id"].(string); fmID != "" {
			id = fmID
		}

		seen[id] = RawPlugin{
			ID:       id,
			Body:     body,
			Source:   source,
			Metadata: metadata,
		}
	}
}
