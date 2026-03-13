package reminder

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// pluginDirs are the directory prefixes to scan for reminder plugins,
// in order of increasing precedence.
var pluginDirs = []string{".agents", ".claude", ".bitcode"}

// pluginFrontmatter represents the YAML structure in plugin files.
type pluginFrontmatter struct {
	ID       string         `yaml:"id"`
	Content  string         `yaml:"content"`
	Schedule pluginSchedule `yaml:"schedule"`
	Priority int            `yaml:"priority"`
}

type pluginSchedule struct {
	Kind         string `yaml:"kind"`
	Interval     string `yaml:"interval"`
	TurnInterval int    `yaml:"turn_interval"`
	MaxFires     int    `yaml:"max_fires"`
	Condition    string `yaml:"condition"`
}

// LoadPlugins scans reminder directories and returns parsed Reminders.
// Directories are scanned in precedence order: .agents < .claude < .bitcode,
// user-level < project-level. Later entries with the same ID overwrite earlier ones.
func LoadPlugins() []Reminder {
	seen := make(map[string]Reminder)

	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()

	// User-level (lower precedence)
	if home != "" {
		for _, d := range pluginDirs {
			loadPluginDir(filepath.Join(home, d, "reminders"), seen)
		}
	}

	// Project-level (higher precedence)
	for _, d := range pluginDirs {
		loadPluginDir(filepath.Join(wd, d, "reminders"), seen)
	}

	result := make([]Reminder, 0, len(seen))
	for _, r := range seen {
		result = append(result, r)
	}
	return result
}

func loadPluginDir(dir string, seen map[string]Reminder) {
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

		r, ok := parsePluginFile(string(data), ext, name)
		if !ok {
			continue
		}

		seen[r.ID] = r
	}
}

func parsePluginFile(content, ext, filename string) (Reminder, bool) {
	var fm pluginFrontmatter
	var body string

	if ext == ".yaml" || ext == ".yml" {
		// Pure YAML file
		if err := yaml.Unmarshal([]byte(content), &fm); err != nil {
			return Reminder{}, false
		}
		body = fm.Content
	} else {
		// Markdown with frontmatter
		fm, body = parseMarkdownFrontmatter(content)
	}

	if fm.ID == "" {
		// Derive ID from filename
		fm.ID = strings.TrimSuffix(filename, ext)
	}

	if body == "" && fm.Content == "" {
		return Reminder{}, false
	}

	if body == "" {
		body = fm.Content
	}

	schedule := buildSchedule(fm.Schedule)

	return Reminder{
		ID:       fm.ID,
		Content:  strings.TrimSpace(body),
		Schedule: schedule,
		Source:   "plugin",
		Priority: fm.Priority,
		Active:   true,
	}, true
}

func parseMarkdownFrontmatter(content string) (pluginFrontmatter, string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return pluginFrontmatter{}, content
	}

	rest := content[4:] // skip "---\n"
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return pluginFrontmatter{}, content
	}

	yamlBlock := rest[:idx]
	afterClose := rest[idx+4:]
	if strings.HasPrefix(afterClose, "\n") {
		afterClose = afterClose[1:]
	} else if strings.HasPrefix(afterClose, "\r\n") {
		afterClose = afterClose[2:]
	}

	var fm pluginFrontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return pluginFrontmatter{}, content
	}

	return fm, afterClose
}

func buildSchedule(ps pluginSchedule) Schedule {
	s := Schedule{
		MaxFires: ps.MaxFires,
	}

	switch ScheduleKind(ps.Kind) {
	case ScheduleAlways:
		s.Kind = ScheduleAlways
	case ScheduleTurn:
		s.Kind = ScheduleTurn
		s.TurnInterval = ps.TurnInterval
		if s.TurnInterval <= 0 {
			s.TurnInterval = 1
		}
	case ScheduleTimer:
		s.Kind = ScheduleTimer
		if d, err := time.ParseDuration(ps.Interval); err == nil {
			s.Interval = d
		} else {
			s.Interval = 5 * time.Minute
		}
	case ScheduleOneShot:
		s.Kind = ScheduleOneShot
	case ScheduleCondition:
		s.Kind = ScheduleCondition
		s.Condition = ParseConditionString(ps.Condition)
	default:
		s.Kind = ScheduleOneShot // safe default
	}

	return s
}

// ParseConditionString interprets simple condition strings:
//
//	"after_tool:Edit"       → fires when Edit was used last turn
//	"after_tool:Edit,Write" → fires when Edit OR Write was used
//	"turn_gt:20"            → fires after 20 turns
//	"always"                → every turn
func ParseConditionString(cond string) ConditionFunc {
	if cond == "" || cond == "always" {
		return func(_ *ConversationState) bool { return true }
	}

	if after, ok := strings.CutPrefix(cond, "after_tool:"); ok {
		tools := strings.Split(after, ",")
		return func(state *ConversationState) bool {
			for _, t := range tools {
				for _, last := range state.LastToolCalls {
					if strings.TrimSpace(t) == last {
						return true
					}
				}
			}
			return false
		}
	}

	if after, ok := strings.CutPrefix(cond, "turn_gt:"); ok {
		var threshold int
		for _, c := range after {
			if c >= '0' && c <= '9' {
				threshold = threshold*10 + int(c-'0')
			}
		}
		return func(state *ConversationState) bool {
			return state.Turn > threshold
		}
	}

	// Unknown condition: never fires
	return func(_ *ConversationState) bool { return false }
}
