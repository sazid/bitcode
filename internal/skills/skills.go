package skills

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/sazid/bitcode/internal/plugin"
)

// Skill represents a user-defined prompt template invoked via slash commands.
type Skill struct {
	Name        string         // derived from filename (without .md extension) or frontmatter
	Description string         // from frontmatter or first line heading
	Prompt      string         // the content after frontmatter is stripped
	Source      string         // "project", "user", or "builtin"
	Trigger     string         // when the LLM should auto-invoke this skill
	Metadata    map[string]any // all frontmatter keys (including extra guard-specific ones)
}

// Config controls how a Manager discovers and loads skills.
type Config struct {
	// SubDir is the directory name to scan inside each base dir.
	// Default: "skills". Guard agent uses "guard-skills".
	SubDir string

	// Embedded is an optional embedded FS of built-in skills
	// (loaded first, lowest precedence).
	Embedded fs.FS

	// EmbeddedSource is the Source label for embedded skills.
	// Default: "builtin".
	EmbeddedSource string
}

// SkillProvider is the interface for accessing skills.
type SkillProvider interface {
	Get(name string) (Skill, bool)
	List() []Skill
}

// Manager discovers and loads skills from the filesystem.
type Manager struct {
	skills map[string]Skill
	cfg    Config
}

// DefaultManager creates a Manager with the default "skills" subdirectory.
// This is the convenience constructor for the main agent.
func DefaultManager() *Manager {
	return NewManager(Config{SubDir: "skills"})
}

// NewManager creates a Manager and loads skills from configured sources.
// Precedence (lowest to highest): embedded FS → user dirs → project dirs.
// Later loads overwrite earlier ones (same name).
func NewManager(cfg Config) *Manager {
	if cfg.SubDir == "" {
		cfg.SubDir = "skills"
	}
	if cfg.EmbeddedSource == "" {
		cfg.EmbeddedSource = "builtin"
	}

	m := &Manager{skills: make(map[string]Skill), cfg: cfg}

	// Embedded FS first (lowest precedence — disk always wins)
	if cfg.Embedded != nil {
		m.loadEmbeddedDir(cfg.Embedded, ".", cfg.EmbeddedSource, "")
	}

	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	// User-level skills (lower precedence)
	if home != "" {
		for _, d := range plugin.BaseDirs {
			m.loadDirRecursive(filepath.Join(home, d, cfg.SubDir), "user", "")
		}
	}

	// Project-level skills (higher precedence, overwrites user-level)
	for _, d := range plugin.BaseDirs {
		m.loadDirRecursive(filepath.Join(wd, d, cfg.SubDir), "project", "")
	}

	return m
}

// skillFromContent parses raw file content into a Skill.
func skillFromContent(data []byte, filename, namePrefix, source string) Skill {
	content := string(data)
	raw, body := plugin.ParseFrontmatter(content)

	// Determine skill name: frontmatter > filename
	name := strings.TrimSuffix(filename, ".md")
	if nameVal, _ := raw["name"].(string); nameVal != "" {
		name = nameVal
	}
	name = namePrefix + name

	// Determine description: frontmatter > first heading
	desc, _ := raw["description"].(string)
	if desc == "" {
		if first, _, ok := strings.Cut(body, "\n"); ok {
			first = strings.TrimSpace(first)
			if after, ok := strings.CutPrefix(first, "# "); ok {
				desc = after
			}
		}
	}

	trigger, _ := raw["trigger"].(string)

	// Copy all frontmatter keys into Metadata (including the known ones)
	metadata := make(map[string]any, len(raw))
	maps.Copy(metadata, raw)

	return Skill{
		Name:        name,
		Description: desc,
		Prompt:      body,
		Source:      source,
		Trigger:     trigger,
		Metadata:    metadata,
	}
}

// loadEmbeddedDir loads skills from an embedded FS directory.
func (m *Manager) loadEmbeddedDir(fsys fs.FS, dir, source, prefix string) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		entryPath := entry.Name()
		if dir != "." {
			entryPath = dir + "/" + entry.Name()
		}

		if entry.IsDir() {
			m.loadEmbeddedDir(fsys, entryPath, source, prefix+entry.Name()+":")
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		data, err := fs.ReadFile(fsys, entryPath)
		if err != nil {
			continue
		}

		s := skillFromContent(data, entry.Name(), prefix, source)
		m.skills[s.Name] = s
	}
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

		s := skillFromContent(data, entry.Name(), prefix, source)
		m.skills[s.Name] = s
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
