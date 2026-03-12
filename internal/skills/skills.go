package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a user-defined prompt template invoked via slash commands.
type Skill struct {
	Name        string // derived from filename (without .md extension)
	Description string // first line of the file (if it starts with "# ")
	Prompt      string // the full content of the skill file
	Source      string // "project" or "user"
}

// Manager discovers and loads skills from the filesystem.
type Manager struct {
	skills map[string]Skill
}

// NewManager creates a Manager and loads skills from project and user directories.
// Project skills (.bitcode/skills/) take precedence over user skills (~/.bitcode/skills/).
func NewManager() *Manager {
	m := &Manager{skills: make(map[string]Skill)}

	// Load user-level skills first (lower precedence)
	if home, err := os.UserHomeDir(); err == nil {
		m.loadDir(filepath.Join(home, ".bitcode", "skills"), "user")
	}

	// Load project-level skills (higher precedence, overwrites user-level)
	wd, _ := os.Getwd()
	m.loadDir(filepath.Join(wd, ".bitcode", "skills"), "project")

	return m
}

func (m *Manager) loadDir(dir, source string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		content := string(data)

		desc := ""
		if first, _, ok := strings.Cut(content, "\n"); ok {
			first = strings.TrimSpace(first)
			if after, okTrim := strings.CutPrefix(first, "# "); okTrim {
				desc = after
			}
		}

		m.skills[name] = Skill{
			Name:        name,
			Description: desc,
			Prompt:      content,
			Source:      source,
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
