package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a user-defined prompt template invoked via slash commands.
type Skill struct {
	Name        string // derived from filename (without .md extension) or frontmatter
	Description string // from frontmatter or first line heading
	Prompt      string // the content after frontmatter is stripped
	Source      string // "project" or "user"
	Trigger     string // when the LLM should auto-invoke this skill
}

// frontmatter represents YAML frontmatter in a skill file.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Trigger     string `yaml:"trigger"`
}

// parseFrontmatter extracts YAML frontmatter from content.
// Returns the parsed metadata and the remaining body after frontmatter.
// If no valid frontmatter is found, returns zero-value metadata and the original content.
func parseFrontmatter(content string) (frontmatter, string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return frontmatter{}, content
	}

	// Find the closing delimiter after the opening "---\n"
	rest := content[4:] // skip "---\n"
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return frontmatter{}, content
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

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		// Malformed YAML: fall back to no frontmatter
		return frontmatter{}, content
	}

	return fm, afterClose
}

// Manager discovers and loads skills from the filesystem.
type Manager struct {
	skills map[string]Skill
}

// skillDirs are the directory prefixes to scan for skills, in order of
// increasing precedence (.bitcode wins over .claude wins over .agents).
var skillDirs = []string{".agents", ".claude", ".bitcode"}

// NewManager creates a Manager and loads skills from project and user directories.
// Directories are scanned in precedence order: .agents < .claude < .bitcode,
// and user-level < project-level. Later loads overwrite earlier ones.
func NewManager() *Manager {
	m := &Manager{skills: make(map[string]Skill)}

	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	// User-level skills (lower precedence)
	if home != "" {
		for _, d := range skillDirs {
			m.loadDirRecursive(filepath.Join(home, d, "skills"), "user", "")
		}
	}

	// Project-level skills (higher precedence, overwrites user-level)
	for _, d := range skillDirs {
		m.loadDirRecursive(filepath.Join(wd, d, "skills"), "project", "")
	}

	return m
}

// loadDirRecursive loads skills from dir and its subdirectories.
// prefix is used for namespacing nested skills (e.g. "git:" for skills in a git/ subdirectory).
func (m *Manager) loadDirRecursive(dir, source, prefix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		fullPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			// Recurse into subdirectory with namespace prefix
			m.loadDirRecursive(fullPath, source, prefix+entry.Name()+":")
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}

		content := string(data)
		fm, body := parseFrontmatter(content)

		// Determine skill name: frontmatter > filename
		name := strings.TrimSuffix(entry.Name(), ".md")
		if fm.Name != "" {
			name = fm.Name
		}
		name = prefix + name

		// Determine description: frontmatter > first heading
		desc := fm.Description
		if desc == "" {
			if first, _, ok := strings.Cut(body, "\n"); ok {
				first = strings.TrimSpace(first)
				if after, okTrim := strings.CutPrefix(first, "# "); okTrim {
					desc = after
				}
			}
		}

		m.skills[name] = Skill{
			Name:        name,
			Description: desc,
			Prompt:      body,
			Source:      source,
			Trigger:     fm.Trigger,
		}
	}
}

// Get returns a skill by name.
func (m *Manager) Get(name string) (Skill, bool) {
	s, ok := m.skills[name]
	return s, ok
}

// List returns all loaded skills.
func (m *Manager) List() []Skill {
	result := make([]Skill, 0, len(m.skills))
	for _, s := range m.skills {
		result = append(result, s)
	}
	return result
}

// FormatPrompt returns the skill prompt with optional arguments appended.
func (s Skill) FormatPrompt(args string) string {
	if args == "" {
		return s.Prompt
	}
	return fmt.Sprintf("%s\n\n%s", s.Prompt, args)
}
