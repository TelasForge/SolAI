package capability

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
)

// MemorySectionProvider is implemented by capabilities that inject a Markdown
// section into the per-cycle prompt. agent.go calls BuildMemorySection() on
// every capability that satisfies this interface.
type MemorySectionProvider interface {
	BuildMemorySection() string
}

const (
	maxObservations = 10
	maxCompleted    = 10
)

// MemoryCapability is an Internal capability that lets the coordinator LLM
// persist structured state across cycles: a current plan, recent observations,
// pending tasks, and completed tasks. The state is injected into each cycle
// prompt via BuildMemorySection().
type MemoryCapability struct {
	mu           sync.RWMutex
	plan         string
	observations []string // ring, max maxObservations
	pending      []string
	completed    []string // ring, max maxCompleted
}

// NewMemoryCapability creates an empty MemoryCapability.
func NewMemoryCapability() *MemoryCapability {
	return &MemoryCapability{}
}

func (m *MemoryCapability) Name() string                   { return "memory" }
func (m *MemoryCapability) Class() CapabilityClass         { return Internal }
func (m *MemoryCapability) ToolRequestDescription() string { return "" }

func (m *MemoryCapability) Description() string {
	return `Persist state across cycles. Input JSON with an "action" field:
  {"action":"update_plan","plan":"<text>"}          — replace current plan
  {"action":"add_observation","content":"<text>"}   — record an observation (last 10 kept)
  {"action":"add_pending","task":"<text>"}           — add a pending task
  {"action":"complete_task","task":"<text>"}         — mark task done, remove from pending
  {"action":"remove_pending","task":"<text>"}        — drop a stale pending task
  {"action":"read"}                                  — return current memory snapshot`
}

// memoryInput is the JSON input shape for Execute.
type memoryInput struct {
	Action  string `json:"action"`
	Plan    string `json:"plan"`
	Content string `json:"content"`
	Task    string `json:"task"`
}

// Execute dispatches on input.action and returns a JSON result or error string.
func (m *MemoryCapability) Execute(_ context.Context, input string) (string, error) {
	var in memoryInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return `{"error":"invalid JSON input"}`, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	slog.Debug("capability called", "capability", "memory", "action", in.Action)

	switch in.Action {
	case "update_plan":
		m.plan = in.Plan
		return `{"ok":true}`, nil

	case "add_observation":
		if in.Content == "" {
			return `{"error":"content is required"}`, nil
		}
		m.observations = appendRing(m.observations, in.Content, maxObservations)
		return `{"ok":true}`, nil

	case "add_pending":
		if in.Task == "" {
			return `{"error":"task is required"}`, nil
		}
		m.pending = append(m.pending, in.Task)
		return `{"ok":true}`, nil

	case "complete_task":
		if in.Task == "" {
			return `{"error":"task is required"}`, nil
		}
		m.pending = removeFirst(m.pending, in.Task)
		m.completed = appendRing(m.completed, in.Task, maxCompleted)
		return `{"ok":true}`, nil

	case "remove_pending":
		if in.Task == "" {
			return `{"error":"task is required"}`, nil
		}
		m.pending = removeFirst(m.pending, in.Task)
		return `{"ok":true}`, nil

	case "read":
		type snapshot struct {
			Plan         string   `json:"plan"`
			Observations []string `json:"observations"`
			Pending      []string `json:"pending"`
			Completed    []string `json:"completed"`
		}
		snap := snapshot{
			Plan:         m.plan,
			Observations: m.observations,
			Pending:      m.pending,
			Completed:    m.completed,
		}
		b, _ := json.Marshal(snap)
		return string(b), nil

	default:
		return `{"error":"unknown action"}`, nil
	}
}

// BuildMemorySection returns a Markdown block for injection into the cycle
// prompt. Returns "" when all fields are empty (nothing to show yet).
func (m *MemoryCapability) BuildMemorySection() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.plan == "" && len(m.observations) == 0 && len(m.pending) == 0 && len(m.completed) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Agent Memory\n")

	b.WriteString("### Current Plan\n")
	if m.plan != "" {
		b.WriteString(m.plan)
	} else {
		b.WriteString("(none)")
	}
	b.WriteString("\n")

	b.WriteString("\n### Recent Observations\n")
	if len(m.observations) > 0 {
		for _, o := range m.observations {
			b.WriteString("- ")
			b.WriteString(o)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("(none)\n")
	}

	b.WriteString("\n### Pending Tasks\n")
	if len(m.pending) > 0 {
		for _, t := range m.pending {
			b.WriteString("- ")
			b.WriteString(t)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("(none)\n")
	}

	b.WriteString("\n### Completed Tasks (recent)\n")
	if len(m.completed) > 0 {
		for _, t := range m.completed {
			b.WriteString("- ")
			b.WriteString(t)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("(none)\n")
	}

	return b.String()
}

// appendRing appends item to slice, evicting the oldest entry when len > max.
func appendRing(slice []string, item string, max int) []string {
	slice = append(slice, item)
	if len(slice) > max {
		slice = slice[len(slice)-max:]
	}
	return slice
}

// removeFirst removes the first occurrence of item from slice.
func removeFirst(slice []string, item string) []string {
	for i, v := range slice {
		if v == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
