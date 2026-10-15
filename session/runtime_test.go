package session

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/melodyogonna/solai/llm"
)

// ---- fake OpenRouter server -------------------------------------------------

type fakeAPI struct {
	t         *testing.T
	responses []string
	requests  []string
	i         int
}

func (f *fakeAPI) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, string(body))
	if f.i >= len(f.responses) {
		http.Error(w, `{"error":{"message":"no more scripted responses"}}`, http.StatusBadRequest)
		return
	}
	resp := f.responses[f.i]
	f.i++
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, resp)
}

func newTestClient(t *testing.T, responses ...string) (*llm.Client, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{t: t, responses: responses}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	client, err := llm.NewClient(llm.Config{APIKey: "test", BaseURL: srv.URL, Model: "test/model"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, f
}

func toolCall(id, name, args string) string {
	return `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"` + id +
		`","type":"function","function":{"name":"` + name + `","arguments":` + strconvQuote(args) + `}}]}}]}`
}

func plainText(text string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + strconvQuote(text) + `}}]}`
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

const goalComplete = `{"action":"complete","note":"done"}`

// ---- fake tool --------------------------------------------------------------

type echoTool struct{ calls int }

func (t *echoTool) Name() string        { return "echo" }
func (t *echoTool) Description() string { return "echoes input" }
func (t *echoTool) Call(_ context.Context, argsJSON string) (string, error) {
	t.calls++
	return "ok:" + llm.ArgString(argsJSON, "input"), nil
}

func collectEvents() (*[]Event, Emitter) {
	events := &[]Event{}
	return events, func(e Event) { *events = append(*events, e) }
}

func hasEvent(events []Event, typ EventType) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// ---- tests ------------------------------------------------------------------

func TestRuntime_GoalCompleteImmediately(t *testing.T) {
	client, api := newTestClient(t, toolCall("c1", "goal", goalComplete))
	events, emit := collectEvents()

	rt := New(Spec{Goals: []string{"do the thing"}}, client, nil, emit)
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if api.i != 1 {
		t.Errorf("expected 1 model call, got %d", api.i)
	}
	goals := rt.Goals()
	if len(goals) != 1 || goals[0].Status != GoalDone {
		t.Fatalf("expected goal done, got %+v", goals)
	}
	for _, typ := range []EventType{EventSessionStarted, EventGoalStarted, EventToolCall, EventToolResult, EventGoalDone, EventSessionDone} {
		if !hasEvent(*events, typ) {
			t.Errorf("missing event %s in %v", typ, *events)
		}
	}
}

func TestRuntime_ToolThenComplete(t *testing.T) {
	client, _ := newTestClient(t,
		toolCall("c1", "echo", `{"input":"hi"}`),
		toolCall("c2", "goal", goalComplete),
	)
	events, emit := collectEvents()
	echo := &echoTool{}

	rt := New(Spec{Goals: []string{"g"}}, client, []llm.Tool{echo}, emit)
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if echo.calls != 1 {
		t.Errorf("expected echo called once, got %d", echo.calls)
	}
	if rt.Goals()[0].Status != GoalDone {
		t.Errorf("goal not done: %+v", rt.Goals())
	}
	// The echo result must have been fed back as a tool message.
	found := false
	for _, e := range *events {
		if e.Type == EventToolResult && e.Tool == "echo" && strings.Contains(e.Result, "ok:hi") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected echo tool result event, got %v", *events)
	}
}

func TestRuntime_ContinuesWhenModelStops(t *testing.T) {
	client, api := newTestClient(t,
		plainText("I think I am done"),
		toolCall("c1", "goal", goalComplete),
	)
	_, emit := collectEvents()

	rt := New(Spec{Goals: []string{"g"}}, client, nil, emit)
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if api.i != 2 {
		t.Fatalf("expected 2 model calls (continue prompt), got %d", api.i)
	}
	// The second request must contain the continue prompt.
	if !strings.Contains(api.requests[1], "Continue working") {
		t.Errorf("expected continue prompt in second request")
	}
	if rt.Goals()[0].Status != GoalDone {
		t.Errorf("goal not done: %+v", rt.Goals())
	}
}

func TestRuntime_SafetyCapPauses(t *testing.T) {
	client, _ := newTestClient(t, plainText("still thinking"))
	events, emit := collectEvents()

	rt := New(Spec{Goals: []string{"g"}, SafetyCap: 1}, client, nil, emit)
	err := rt.Run(context.Background())
	if err != ErrSafetyCap {
		t.Fatalf("expected ErrSafetyCap, got %v", err)
	}
	var finalStatus string
	for _, e := range *events {
		if e.Type == EventSessionDone {
			finalStatus = e.Status
		}
	}
	if finalStatus != string(StatusPaused) {
		t.Errorf("expected paused status, got %q", finalStatus)
	}
}

func TestRuntime_GoalFailMarksSessionFailed(t *testing.T) {
	client, _ := newTestClient(t, toolCall("c1", "goal", `{"action":"fail","note":"impossible"}`))
	events, emit := collectEvents()

	rt := New(Spec{Goals: []string{"g"}}, client, nil, emit)
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rt.Goals()[0].Status != GoalFailed {
		t.Errorf("expected goal failed, got %+v", rt.Goals())
	}
	for _, e := range *events {
		if e.Type == EventSessionDone && e.Status != string(StatusFailed) {
			t.Errorf("expected session failed, got %q", e.Status)
		}
	}
}

func TestRuntime_MultipleGoalsSequential(t *testing.T) {
	client, _ := newTestClient(t,
		toolCall("c1", "goal", goalComplete),
		toolCall("c2", "goal", goalComplete),
	)
	_, emit := collectEvents()

	rt := New(Spec{Goals: []string{"g1", "g2"}}, client, nil, emit)
	if err := rt.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	goals := rt.Goals()
	if len(goals) != 2 || goals[0].Status != GoalDone || goals[1].Status != GoalDone {
		t.Errorf("expected both goals done, got %+v", goals)
	}
}

func TestLineEmitter_WritesJSONLines(t *testing.T) {
	var buf strings.Builder
	emit := NewLineEmitter(&buf)
	emit(Event{Type: EventMessage, Message: "hello"})
	emit(Event{Type: EventSessionDone, Status: "done"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
	var e Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("line 1 not valid JSON: %v", err)
	}
	if e.Type != EventMessage || e.Message != "hello" {
		t.Errorf("unexpected event: %+v", e)
	}
	if e.Time.IsZero() {
		t.Error("expected emitter to populate time")
	}
}
