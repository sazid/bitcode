package telemetry

import (
	"sync"
	"sync/atomic"
	"time"
)

const eventBufferSize = 256

// Collector implements Observer with a buffered channel and single-writer goroutine.
type Collector struct {
	sessionID string
	startTime time.Time
	store     *Store
	events    chan Event
	done      chan struct{}

	mu    sync.Mutex
	stats SessionStats
}

// NewCollector creates and starts a Collector.
func NewCollector(sessionID string, store *Store) *Collector {
	c := &Collector{
		sessionID: sessionID,
		startTime: time.Now(),
		store:     store,
		events:    make(chan Event, eventBufferSize),
		done:      make(chan struct{}),
		stats: SessionStats{
			SessionID:     sessionID,
			StartTime:     time.Now(),
			ToolCalls:     make(map[string]int),
			GuardVerdicts: make(map[string]int),
		},
	}
	go c.writer()
	return c
}

// writer drains the event channel and writes to the store.
func (c *Collector) writer() {
	defer close(c.done)
	for ev := range c.events {
		if c.store != nil {
			c.store.Write(ev)
		}
	}
}

// send enqueues an event. Non-blocking — drops if channel is full.
func (c *Collector) send(ev Event) {
	ev.Timestamp = time.Now()
	ev.SessionID = c.sessionID
	select {
	case c.events <- ev:
	default:
	}
}

func (c *Collector) RecordLLM(turn int, ev LLMEvent) {
	c.send(Event{
		Type: EventLLMCall,
		Turn: turn,
		LLM:  &ev,
	})

	c.mu.Lock()
	c.stats.LLMCalls++
	c.stats.TotalLatency += ev.Duration
	c.stats.InputTokens += ev.InputTokens
	c.stats.OutputTokens += ev.OutputTokens
	c.stats.CacheRead += ev.CacheRead
	c.stats.CacheCreate += ev.CacheCreate
	c.mu.Unlock()
}

func (c *Collector) RecordTool(turn int, ev ToolEvent) {
	c.send(Event{
		Type: EventToolCall,
		Turn: turn,
		Tool: &ev,
	})

	c.mu.Lock()
	c.stats.ToolCalls[ev.Name]++
	if !ev.Success {
		c.stats.ToolErrors++
	}
	c.mu.Unlock()
}

func (c *Collector) RecordGuard(turn int, ev GuardEvent) {
	c.send(Event{
		Type:  EventGuardEval,
		Turn:  turn,
		Guard: &ev,
	})

	c.mu.Lock()
	c.stats.GuardEvals++
	c.stats.GuardVerdicts[ev.Verdict]++
	c.mu.Unlock()
}

func (c *Collector) RecordSessionStart(mode string) {
	c.send(Event{
		Type:    EventSessionStart,
		Session: &SessionEvent{Mode: mode},
	})
}

func (c *Collector) RecordSessionEnd(duration time.Duration) {
	c.send(Event{
		Type:    EventSessionEnd,
		Session: &SessionEvent{Duration: duration},
	})
}

func (c *Collector) RecordError(turn int, component, message, ctx string) {
	c.send(Event{
		Type: EventError,
		Turn: turn,
		Error: &ErrorEvent{
			Component: component,
			Message:   message,
			Context:   ctx,
		},
	})

	c.mu.Lock()
	c.stats.Errors++
	c.mu.Unlock()
}

func (c *Collector) Stats() *SessionStats {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Return a copy
	s := c.stats
	s.ToolCalls = make(map[string]int, len(c.stats.ToolCalls))
	for k, v := range c.stats.ToolCalls {
		s.ToolCalls[k] = v
	}
	s.GuardVerdicts = make(map[string]int, len(c.stats.GuardVerdicts))
	for k, v := range c.stats.GuardVerdicts {
		s.GuardVerdicts[k] = v
	}
	return &s
}

func (c *Collector) ResetSession(newID string) {
	c.mu.Lock()
	c.sessionID = newID
	c.startTime = time.Now()
	c.stats = SessionStats{
		SessionID:     newID,
		StartTime:     time.Now(),
		ToolCalls:     make(map[string]int),
		GuardVerdicts: make(map[string]int),
	}
	c.mu.Unlock()
}

func (c *Collector) Close() {
	c.RecordSessionEnd(time.Since(c.startTime))
	close(c.events)
	<-c.done
	if c.store != nil {
		c.store.Close()
	}
}

// TurnCounter is a shared atomic counter for the current turn number.
// It's updated by the agent loop and read by telemetry wrappers.
type TurnCounter struct {
	value atomic.Int32
}

func NewTurnCounter() *TurnCounter {
	return &TurnCounter{}
}

func (tc *TurnCounter) Set(turn int) {
	tc.value.Store(int32(turn))
}

func (tc *TurnCounter) Get() int {
	return int(tc.value.Load())
}
