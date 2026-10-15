package controlplane

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/session"
)

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("control plane: session not found")

// ErrNoGoals is returned when a session spec has no goals.
var ErrNoGoals = errors.New("control plane: session has no goals")

// subscriberBuffer is the per-subscriber event channel capacity.
const subscriberBuffer = 256

// Session is the control plane's view of a session.
type Session struct {
	ID        string         `json:"id"`
	Status    string         `json:"status"`
	Goals     []session.Goal `json:"goals"`
	Spec      session.Spec   `json:"spec"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Manager owns in-memory sessions and launches their runtimes.
type Manager struct {
	launcher Launcher
	ctx      context.Context
	cfg      config.Config

	mu       sync.Mutex
	sessions map[string]*sessionState
}

// NewManager creates a manager. ctx is the parent context for every launched
// runtime, so cancelling it stops all sessions. cfg supplies the values that
// capabilities are provisioned from.
func NewManager(ctx context.Context, cfg config.Config, launcher Launcher) *Manager {
	return &Manager{
		launcher: launcher,
		ctx:      ctx,
		cfg:      cfg,
		sessions: make(map[string]*sessionState),
	}
}

// Create registers a session and launches its runtime.
func (m *Manager) Create(spec session.Spec) (*Session, error) {
	if len(spec.Goals) == 0 {
		return nil, ErrNoGoals
	}

	env, err := provision(spec.Capabilities, m.cfg)
	if err != nil {
		return nil, err
	}

	st := newSessionState(newID(), spec)
	m.mu.Lock()
	m.sessions[st.id] = st
	m.mu.Unlock()

	proc, err := m.launcher.Start(m.ctx, spec, env)
	if err != nil {
		st.finishWith(session.StatusFailed)
		return st.view(), err
	}
	st.setProc(proc)
	go m.consume(st, proc)
	return st.view(), nil
}

// Get returns a session by id.
func (m *Manager) Get(id string) (*Session, error) {
	st, ok := m.lookup(id)
	if !ok {
		return nil, ErrNotFound
	}
	return st.view(), nil
}

// List returns all sessions, oldest first.
func (m *Manager) List() []*Session {
	m.mu.Lock()
	states := make([]*sessionState, 0, len(m.sessions))
	for _, st := range m.sessions {
		states = append(states, st)
	}
	m.mu.Unlock()

	out := make([]*Session, 0, len(states))
	for _, st := range states {
		out = append(out, st.view())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Stop terminates a session's runtime.
func (m *Manager) Stop(id string) (*Session, error) {
	st, ok := m.lookup(id)
	if !ok {
		return nil, ErrNotFound
	}
	proc := st.markStopped()
	if proc != nil {
		_ = proc.Stop()
	}
	return st.view(), nil
}

// Subscribe returns a session's buffered events, a live channel, and an
// unsubscribe function. done is true when the session is already terminal, in
// which case ch is nil and only the replay is meaningful.
func (m *Manager) Subscribe(id string) (replay []session.Event, ch chan session.Event, done bool, unsub func(), err error) {
	st, ok := m.lookup(id)
	if !ok {
		return nil, nil, false, nil, ErrNotFound
	}
	replay, ch, done, unsub = st.subscribe()
	return replay, ch, done, unsub, nil
}

func (m *Manager) lookup(id string) (*sessionState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.sessions[id]
	return st, ok
}

// consume reads the runtime's event stream until it ends, then finalizes the
// session.
func (m *Manager) consume(st *sessionState, proc RuntimeProcess) {
	scanner := bufio.NewScanner(proc.Events())
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e session.Event
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		st.record(e)
		logEvent(st.id, e)
	}
	_ = proc.Wait()
	st.finish()
}

// logEvent mirrors the runtime's event stream into the control-plane log so
// the agent's output is visible on the terminal, not just over SSE.
func logEvent(id string, e session.Event) {
	switch e.Type {
	case session.EventMessage:
		slog.Info("session message", "session", id, "goal", e.GoalID, "text", e.Message)
	case session.EventToolCall:
		slog.Info("tool call", "session", id, "tool", e.Tool)
	case session.EventToolResult:
		slog.Debug("tool result", "session", id, "tool", e.Tool, "result", e.Result)
	case session.EventGoalDone:
		slog.Info("goal resolved", "session", id, "goal", e.GoalID, "status", e.Status, "note", e.Message)
	case session.EventSessionDone:
		slog.Info("session finished", "session", id, "status", e.Status)
	case session.EventError:
		slog.Warn("session error", "session", id, "message", e.Message)
	default:
		slog.Debug("session event", "session", id, "type", e.Type)
	}
}

// ---- sessionState -----------------------------------------------------------

type sessionState struct {
	mu      sync.Mutex
	id      string
	spec    session.Spec
	status  string
	goals   []session.Goal
	created time.Time
	updated time.Time

	events  []session.Event
	subs    map[chan session.Event]struct{}
	proc    RuntimeProcess
	stopped bool
}

func newSessionState(id string, spec session.Spec) *sessionState {
	goals := make([]session.Goal, len(spec.Goals))
	for i, text := range spec.Goals {
		goals[i] = session.Goal{ID: fmt.Sprintf("g%d", i+1), Text: text, Status: session.GoalPending}
	}
	now := time.Now().UTC()
	return &sessionState{
		id:      id,
		spec:    spec,
		status:  string(session.StatusPending),
		goals:   goals,
		created: now,
		updated: now,
		subs:    make(map[chan session.Event]struct{}),
	}
}

func (s *sessionState) setProc(proc RuntimeProcess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.proc = proc
}

// markStopped records the stop intent and returns the process to kill.
func (s *sessionState) markStopped() RuntimeProcess {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	if !isTerminal(s.status) {
		s.status = string(session.StatusStopped)
		s.updated = time.Now().UTC()
	}
	return s.proc
}

func (s *sessionState) record(e session.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, e)
	s.updated = time.Now().UTC()

	switch e.Type {
	case session.EventSessionStarted:
		s.status = string(session.StatusRunning)
	case session.EventGoalStarted:
		s.upsertGoal(e.GoalID, e.Message, session.GoalRunning, "")
	case session.EventGoalDone:
		s.upsertGoal(e.GoalID, "", session.GoalStatus(e.Status), e.Message)
	case session.EventSessionDone:
		s.status = e.Status
	}

	for ch := range s.subs {
		select {
		case ch <- e:
		default: // slow subscriber; drop rather than block the runtime
		}
	}
}

func (s *sessionState) upsertGoal(id, text string, status session.GoalStatus, note string) {
	if id == "" {
		return
	}
	for i := range s.goals {
		if s.goals[i].ID != id {
			continue
		}
		if text != "" {
			s.goals[i].Text = text
		}
		if status != "" {
			s.goals[i].Status = status
		}
		if note != "" {
			s.goals[i].Note = note
		}
		return
	}
	s.goals = append(s.goals, session.Goal{ID: id, Text: text, Status: status, Note: note})
}

func (s *sessionState) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !isTerminal(s.status) {
		if s.stopped {
			s.status = string(session.StatusStopped)
		} else {
			s.status = string(session.StatusFailed)
		}
	}
	s.updated = time.Now().UTC()
	s.closeSubscribersLocked()
}

func (s *sessionState) finishWith(status session.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = string(status)
	s.updated = time.Now().UTC()
	s.closeSubscribersLocked()
}

func (s *sessionState) closeSubscribersLocked() {
	for ch := range s.subs {
		close(ch)
		delete(s.subs, ch)
	}
}

func (s *sessionState) subscribe() (replay []session.Event, ch chan session.Event, done bool, unsub func()) {
	s.mu.Lock()
	defer s.mu.Unlock()

	replay = append([]session.Event(nil), s.events...)
	if isTerminal(s.status) {
		return replay, nil, true, func() {}
	}

	ch = make(chan session.Event, subscriberBuffer)
	s.subs[ch] = struct{}{}
	unsub = func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
	return replay, ch, false, unsub
}

func (s *sessionState) view() *Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	goals := append([]session.Goal(nil), s.goals...)
	return &Session{
		ID:        s.id,
		Status:    s.status,
		Goals:     goals,
		Spec:      s.spec,
		CreatedAt: s.created,
		UpdatedAt: s.updated,
	}
}

func isTerminal(status string) bool {
	switch session.Status(status) {
	case session.StatusDone, session.StatusFailed, session.StatusStopped, session.StatusPaused:
		return true
	}
	return false
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
