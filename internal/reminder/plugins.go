package reminder

import (
	"strings"
	"time"

	"github.com/sazid/bitcode/internal/plugin"
	"gopkg.in/yaml.v3"
)

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
	var result []Reminder
	for _, raw := range plugin.LoadFiles("reminders") {
		r, ok := convertRawToReminder(raw)
		if !ok {
			continue
		}
		result = append(result, r)
	}
	return result
}

func convertRawToReminder(raw plugin.RawPlugin) (Reminder, bool) {
	var fm pluginFrontmatter

	if raw.Metadata != nil {
		// Convert generic metadata map to typed struct via YAML roundtrip
		yamlBytes, err := yaml.Marshal(raw.Metadata)
		if err == nil {
			yaml.Unmarshal(yamlBytes, &fm)
		}
	}

	body := raw.Body

	if fm.ID == "" {
		fm.ID = raw.ID
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
//	"after_tool:Edit"       -> fires when Edit was used last turn
//	"after_tool:Edit,Write" -> fires when Edit OR Write was used
//	"turn_gt:20"            -> fires after 20 turns
//	"always"                -> every turn
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
