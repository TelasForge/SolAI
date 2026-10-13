package session

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// EventType identifies the kind of session event.
type EventType string

const (
	EventSessionStarted EventType = "session.started"
	EventGoalStarted    EventType = "goal.started"
	EventMessage        EventType = "message"
	EventToolCall       EventType = "tool.call"
	EventToolResult     EventType = "tool.result"
	EventGoalDone       EventType = "goal.done"
	EventSessionDone    EventType = "session.done"
	EventError          EventType = "error"
)

// Event is one item in the session's event stream.
type Event struct {
	Type    EventType `json:"type"`
	Time    time.Time `json:"time"`
	GoalID  string    `json:"goal_id,omitempty"`
	Status  string    `json:"status,omitempty"`
	Message string    `json:"message,omitempty"`
	Tool    string    `json:"tool,omitempty"`
	Args    string    `json:"args,omitempty"`
	Result  string    `json:"result,omitempty"`
}

// Emitter receives session events. It must be safe for sequential use; the
// line emitter is also safe under concurrent use.
type Emitter func(Event)

// NewLineEmitter returns an Emitter that writes one JSON object per line to w.
func NewLineEmitter(w io.Writer) Emitter {
	enc := json.NewEncoder(w)
	var mu sync.Mutex
	return func(e Event) {
		if e.Time.IsZero() {
			e.Time = time.Now().UTC()
		}
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(e)
	}
}
