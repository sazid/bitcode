package reminder

import (
	"sort"
	"sync"
	"time"
)

// ReminderEvaluator is the interface for evaluating and registering reminders.
type ReminderEvaluator interface {
	Evaluate(state *ConversationState) []Reminder
	Register(r Reminder)
}

// Manager collects reminders from all sources and evaluates which to fire.
type Manager struct {
	mu         sync.RWMutex
	reminders  []Reminder
	fireCounts map[string]int
	lastFired  map[string]time.Time
	startTime  time.Time
}

// NewManager creates a new reminder manager.
func NewManager() *Manager {
	return &Manager{
		reminders:  make([]Reminder, 0),
		fireCounts: make(map[string]int),
		lastFired:  make(map[string]time.Time),
		startTime:  time.Now(),
	}
}

// Register adds a reminder to the manager.
func (m *Manager) Register(r Reminder) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Replace existing reminder with same ID
	for i, existing := range m.reminders {
		if existing.ID == r.ID {
			m.reminders[i] = r
			return
		}
	}
	m.reminders = append(m.reminders, r)
}

// Remove deactivates a reminder by ID.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, r := range m.reminders {
		if r.ID == id {
			m.reminders[i].Active = false
			return
		}
	}
}

// Evaluate returns all reminders that should fire given the current state.
// It updates internal counters and deactivates one-shot reminders after firing.
// Results are sorted by priority (ascending), so higher priority reminders
// appear later in the list (closer to the end of the injected text = more LLM attention).
func (m *Manager) Evaluate(state *ConversationState) []Reminder {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var result []Reminder

	for i, r := range m.reminders {
		if !r.Active {
			continue
		}

		// Check max fires
		if r.Schedule.MaxFires > 0 && m.fireCounts[r.ID] >= r.Schedule.MaxFires {
			m.reminders[i].Active = false
			continue
		}

		shouldFire := false

		switch r.Schedule.Kind {
		case ScheduleAlways:
			shouldFire = true

		case ScheduleTurn:
			interval := r.Schedule.TurnInterval
			if interval <= 0 {
				interval = 1
			}
			shouldFire = state.Turn%interval == 0

		case ScheduleTimer:
			last, ok := m.lastFired[r.ID]
			if !ok {
				shouldFire = true // first time
			} else {
				shouldFire = now.Sub(last) >= r.Schedule.Interval
			}

		case ScheduleOneShot:
			shouldFire = m.fireCounts[r.ID] == 0

		case ScheduleCondition:
			if r.Schedule.Condition != nil {
				shouldFire = r.Schedule.Condition(state)
			}
		}

		if shouldFire {
			result = append(result, r)
			m.fireCounts[r.ID]++
			m.lastFired[r.ID] = now

			// Deactivate one-shot reminders
			if r.Schedule.Kind == ScheduleOneShot {
				m.reminders[i].Active = false
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority < result[j].Priority
	})

	return result
}
