package plugin

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseFrontmatter extracts YAML frontmatter from content.
// Returns the raw metadata map and the remaining body after frontmatter.
// If no valid frontmatter is found, returns nil and the original content.
func ParseFrontmatter(content string) (map[string]any, string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return nil, content
	}

	// Find the closing delimiter after the opening "---\n"
	rest := content[4:] // skip "---\n"
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, content
	}

	yamlBlock := rest[:idx]

	// Find the start of body after closing "---\n"
	afterClose := rest[idx+4:] // skip "\n---"
	// Skip optional newline after closing ---
	if strings.HasPrefix(afterClose, "\n") {
		afterClose = afterClose[1:]
	} else if strings.HasPrefix(afterClose, "\r\n") {
		afterClose = afterClose[2:]
	}

	var raw map[string]any
	if err := yaml.Unmarshal([]byte(yamlBlock), &raw); err != nil {
		// Malformed YAML: fall back to no frontmatter
		return nil, content
	}

	return raw, afterClose
}
