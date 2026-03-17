package reminder

import (
	"testing"
	"time"

	"github.com/sazid/bitcode/internal/llm"
	"github.com/sazid/bitcode/internal/plugin"
	"gopkg.in/yaml.v3"
)

func TestEvaluate_Always(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:       "always-r",
		Content:  "always content",
		Schedule: Schedule{Kind: ScheduleAlways},
		Active:   true,
	})

	state := &ConversationState{Turn: 0}
	result := m.Evaluate(state)
	if len(result) != 1 {
		t.Fatalf("expected 1 reminder, got %d", len(result))
	}
	if result[0].ID != "always-r" {
		t.Errorf("expected 'always-r', got %q", result[0].ID)
	}

	// Should fire again on next turn
	state.Turn = 1
	result = m.Evaluate(state)
	if len(result) != 1 {
		t.Fatalf("expected 1 reminder on turn 1, got %d", len(result))
	}
}

func TestEvaluate_TurnInterval(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:       "turn-r",
		Content:  "every 3 turns",
		Schedule: Schedule{Kind: ScheduleTurn, TurnInterval: 3},
		Active:   true,
	})

	state := &ConversationState{}

	// Turn 0: 0 % 3 == 0 → fire
	state.Turn = 0
	if len(m.Evaluate(state)) != 1 {
		t.Error("expected fire on turn 0")
	}

	// Turn 1: 1 % 3 != 0 → skip
	state.Turn = 1
	if len(m.Evaluate(state)) != 0 {
		t.Error("expected no fire on turn 1")
	}

	// Turn 3: 3 % 3 == 0 → fire
	state.Turn = 3
	if len(m.Evaluate(state)) != 1 {
		t.Error("expected fire on turn 3")
	}
}

func TestEvaluate_OneShot(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:       "oneshot-r",
		Content:  "fire once",
		Schedule: Schedule{Kind: ScheduleOneShot},
		Active:   true,
	})

	state := &ConversationState{Turn: 0}

	// First evaluation: should fire
	result := m.Evaluate(state)
	if len(result) != 1 {
		t.Fatalf("expected 1 reminder on first fire, got %d", len(result))
	}

	// Second evaluation: should NOT fire (deactivated)
	state.Turn = 1
	result = m.Evaluate(state)
	if len(result) != 0 {
		t.Errorf("expected 0 reminders after one-shot, got %d", len(result))
	}
}

func TestEvaluate_Condition(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:      "cond-r",
		Content: "condition met",
		Schedule: Schedule{
			Kind: ScheduleCondition,
			Condition: func(state *ConversationState) bool {
				for _, tc := range state.LastToolCalls {
					if tc == "Edit" {
						return true
					}
				}
				return false
			},
		},
		Active: true,
	})

	// No Edit tool call → should not fire
	state := &ConversationState{Turn: 0, LastToolCalls: []string{"Read"}}
	result := m.Evaluate(state)
	if len(result) != 0 {
		t.Errorf("expected 0 reminders without Edit, got %d", len(result))
	}

	// Edit tool call → should fire
	state.LastToolCalls = []string{"Edit", "Write"}
	result = m.Evaluate(state)
	if len(result) != 1 {
		t.Errorf("expected 1 reminder with Edit, got %d", len(result))
	}
}

func TestEvaluate_MaxFires(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:       "max-r",
		Content:  "limited",
		Schedule: Schedule{Kind: ScheduleAlways, MaxFires: 2},
		Active:   true,
	})

	state := &ConversationState{}

	// First fire
	state.Turn = 0
	if len(m.Evaluate(state)) != 1 {
		t.Error("expected fire 1")
	}

	// Second fire
	state.Turn = 1
	if len(m.Evaluate(state)) != 1 {
		t.Error("expected fire 2")
	}

	// Third: should be deactivated
	state.Turn = 2
	if len(m.Evaluate(state)) != 0 {
		t.Error("expected no fire after max")
	}
}

func TestEvaluate_Priority(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:       "high",
		Content:  "high priority",
		Schedule: Schedule{Kind: ScheduleAlways},
		Priority: 10,
		Active:   true,
	})
	m.Register(Reminder{
		ID:       "low",
		Content:  "low priority",
		Schedule: Schedule{Kind: ScheduleAlways},
		Priority: 1,
		Active:   true,
	})

	state := &ConversationState{Turn: 0}
	result := m.Evaluate(state)
	if len(result) != 2 {
		t.Fatalf("expected 2 reminders, got %d", len(result))
	}
	if result[0].ID != "low" {
		t.Errorf("expected 'low' first (lower priority), got %q", result[0].ID)
	}
	if result[1].ID != "high" {
		t.Errorf("expected 'high' second (higher priority), got %q", result[1].ID)
	}
}

func TestEvaluate_Timer(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:       "timer-r",
		Content:  "timer",
		Schedule: Schedule{Kind: ScheduleTimer, Interval: 50 * time.Millisecond},
		Active:   true,
	})

	state := &ConversationState{Turn: 0}

	// First call: should fire (no previous fire)
	if len(m.Evaluate(state)) != 1 {
		t.Error("expected first timer fire")
	}

	// Immediate second call: should NOT fire (interval not elapsed)
	state.Turn = 1
	if len(m.Evaluate(state)) != 0 {
		t.Error("expected no fire before interval")
	}

	// Wait for interval
	time.Sleep(60 * time.Millisecond)
	state.Turn = 2
	if len(m.Evaluate(state)) != 1 {
		t.Error("expected fire after interval elapsed")
	}
}

func TestRemove(t *testing.T) {
	m := NewManager()
	m.Register(Reminder{
		ID:       "removable",
		Content:  "will be removed",
		Schedule: Schedule{Kind: ScheduleAlways},
		Active:   true,
	})

	state := &ConversationState{Turn: 0}
	if len(m.Evaluate(state)) != 1 {
		t.Fatal("expected 1 before remove")
	}

	m.Remove("removable")
	state.Turn = 1
	if len(m.Evaluate(state)) != 0 {
		t.Error("expected 0 after remove")
	}
}

func TestInjectReminders(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleSystem, "system prompt"),
		llm.TextMessage(llm.RoleUser, "hello"),
		llm.TextMessage(llm.RoleAssistant, "hi there"),
	}

	reminders := []Reminder{
		{ID: "r1", Content: "reminder one"},
		{ID: "r2", Content: "reminder two"},
	}

	result := InjectReminders(messages, reminders)

	// Original should be untouched
	if messages[1].Text() != "hello" {
		t.Errorf("original user message mutated: %q", messages[1].Text())
	}

	// Result should have reminders appended to the last user message (index 1)
	// since the assistant message (index 2) is not user/tool
	injectedText := result[1].Text()
	if injectedText == "hello" {
		t.Error("expected reminders injected, but text unchanged")
	}
	if !contains(injectedText, "<system-reminder>") {
		t.Error("expected <system-reminder> tag in injected text")
	}
	if !contains(injectedText, "reminder one") {
		t.Error("expected 'reminder one' in injected text")
	}
	if !contains(injectedText, "reminder two") {
		t.Error("expected 'reminder two' in injected text")
	}

	// Assistant message should be unchanged
	if result[2].Text() != "hi there" {
		t.Errorf("assistant message should be unchanged, got %q", result[2].Text())
	}
}

func TestInjectReminders_IntoToolMessage(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleUser, "do something"),
		{
			Role:       llm.RoleTool,
			Content:    []llm.ContentBlock{{Type: llm.ContentText, Text: "tool result"}},
			ToolCallID: "tc1",
		},
	}

	reminders := []Reminder{
		{ID: "r1", Content: "after tool"},
	}

	result := InjectReminders(messages, reminders)

	// Should inject into the tool message (last user/tool)
	if !contains(result[1].Text(), "after tool") {
		t.Error("expected reminder injected into tool message")
	}

	// User message should be untouched
	if result[0].Text() != "do something" {
		t.Errorf("user message should be unchanged, got %q", result[0].Text())
	}

	// ToolCallID should be preserved
	if result[1].ToolCallID != "tc1" {
		t.Errorf("ToolCallID lost, got %q", result[1].ToolCallID)
	}
}

func TestInjectReminders_Empty(t *testing.T) {
	messages := []llm.Message{
		llm.TextMessage(llm.RoleUser, "hello"),
	}

	result := InjectReminders(messages, nil)

	// Should return the same slice (no copy needed)
	if len(result) != len(messages) {
		t.Fatal("unexpected length change")
	}
	if result[0].Text() != "hello" {
		t.Error("unexpected text change")
	}
}

func TestParseConditionString(t *testing.T) {
	t.Run("always", func(t *testing.T) {
		fn := ParseConditionString("always")
		if !fn(&ConversationState{}) {
			t.Error("always should return true")
		}
	})

	t.Run("empty", func(t *testing.T) {
		fn := ParseConditionString("")
		if !fn(&ConversationState{}) {
			t.Error("empty should return true")
		}
	})

	t.Run("after_tool match", func(t *testing.T) {
		fn := ParseConditionString("after_tool:Edit")
		state := &ConversationState{LastToolCalls: []string{"Edit"}}
		if !fn(state) {
			t.Error("should match Edit")
		}
	})

	t.Run("after_tool no match", func(t *testing.T) {
		fn := ParseConditionString("after_tool:Edit")
		state := &ConversationState{LastToolCalls: []string{"Read"}}
		if fn(state) {
			t.Error("should not match Read")
		}
	})

	t.Run("after_tool multiple", func(t *testing.T) {
		fn := ParseConditionString("after_tool:Edit,Write")
		state := &ConversationState{LastToolCalls: []string{"Write"}}
		if !fn(state) {
			t.Error("should match Write")
		}
	})

	t.Run("turn_gt match", func(t *testing.T) {
		fn := ParseConditionString("turn_gt:10")
		state := &ConversationState{Turn: 15}
		if !fn(state) {
			t.Error("15 > 10 should be true")
		}
	})

	t.Run("turn_gt no match", func(t *testing.T) {
		fn := ParseConditionString("turn_gt:10")
		state := &ConversationState{Turn: 5}
		if fn(state) {
			t.Error("5 > 10 should be false")
		}
	})

	t.Run("unknown condition", func(t *testing.T) {
		fn := ParseConditionString("something_weird:foo")
		if fn(&ConversationState{}) {
			t.Error("unknown condition should return false")
		}
	})
}

func TestLoadPlugins_Markdown(t *testing.T) {
	content := "---\nid: test-plugin\nschedule:\n  kind: always\npriority: 5\n---\nRemember to test everything."
	metadata, body := plugin.ParseFrontmatter(content)

	raw := plugin.RawPlugin{
		ID:       "test-plugin",
		Body:     body,
		Source:   "project",
		Metadata: metadata,
	}

	r, ok := convertRawToReminder(raw)
	if !ok {
		t.Fatal("expected successful conversion")
	}
	if r.Content != "Remember to test everything." {
		t.Errorf("unexpected content: %q", r.Content)
	}
	if r.Schedule.Kind != ScheduleAlways {
		t.Errorf("expected always schedule, got %q", r.Schedule.Kind)
	}
	if r.Priority != 5 {
		t.Errorf("expected priority 5, got %d", r.Priority)
	}
	if r.Source != "plugin" {
		t.Errorf("expected source 'plugin', got %q", r.Source)
	}
}

func TestLoadPlugins_YAML(t *testing.T) {
	content := "id: yaml-plugin\ncontent: YAML content here.\nschedule:\n  kind: turn\n  turn_interval: 5\npriority: 2\n"
	var metadata map[string]any
	yaml.Unmarshal([]byte(content), &metadata)

	raw := plugin.RawPlugin{
		ID:       "yaml-plugin",
		Body:     "",
		Source:   "project",
		Metadata: metadata,
	}

	r, ok := convertRawToReminder(raw)
	if !ok {
		t.Fatal("expected successful conversion")
	}
	if r.Content != "YAML content here." {
		t.Errorf("unexpected content: %q", r.Content)
	}
	if r.Schedule.Kind != ScheduleTurn {
		t.Errorf("expected turn schedule, got %q", r.Schedule.Kind)
	}
	if r.Schedule.TurnInterval != 5 {
		t.Errorf("expected turn interval 5, got %d", r.Schedule.TurnInterval)
	}
}

func TestLoadPlugins_IDFromFilename(t *testing.T) {
	content := "---\nschedule:\n  kind: oneshot\n---\nContent without explicit ID."
	metadata, body := plugin.ParseFrontmatter(content)

	// ID derived from filename (simulating what LoadFiles does)
	raw := plugin.RawPlugin{
		ID:       "my-reminder",
		Body:     body,
		Source:   "project",
		Metadata: metadata,
	}

	r, ok := convertRawToReminder(raw)
	if !ok {
		t.Fatal("expected successful conversion")
	}
	if r.ID != "my-reminder" {
		t.Errorf("expected ID 'my-reminder', got %q", r.ID)
	}
}

func TestConvertRawToReminder_EmptyContent(t *testing.T) {
	// RawPlugin with no body and no content in metadata should fail conversion
	raw := plugin.RawPlugin{
		ID:       "empty",
		Body:     "",
		Source:   "project",
		Metadata: nil,
	}

	_, ok := convertRawToReminder(raw)
	if ok {
		t.Error("expected conversion to fail for empty content")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsSubstring(s, substr)
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
