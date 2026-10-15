package controlplane

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/melodyogonna/solai/session"
)

// ---- fakes ------------------------------------------------------------------

type staticProc struct{ r io.Reader }

func (p *staticProc) Events() io.Reader { return p.r }
func (p *staticProc) Stop() error       { return nil }
func (p *staticProc) Wait() error       { return nil }

type blockingProc struct {
	once    sync.Once
	unblock chan struct{}
}

func newBlockingProc() *blockingProc { return &blockingProc{unblock: make(chan struct{})} }

func (p *blockingProc) Events() io.Reader { return &blockingReader{unblock: p.unblock} }
func (p *blockingProc) Stop() error       { p.once.Do(func() { close(p.unblock) }); return nil }
func (p *blockingProc) Wait() error       { return nil }

type blockingReader struct{ unblock chan struct{} }

func (r *blockingReader) Read([]byte) (int, error) { <-r.unblock; return 0, io.EOF }

type fakeLauncher struct {
	events string
	proc   RuntimeProcess
	err    error
	specs  []session.Spec
}

func (l *fakeLauncher) Start(_ context.Context, spec session.Spec) (RuntimeProcess, error) {
	l.specs = append(l.specs, spec)
	if l.err != nil {
		return nil, l.err
	}
	if l.proc != nil {
		return l.proc, nil
	}
	return &staticProc{r: strings.NewReader(l.events)}, nil
}

const doneEvents = `{"type":"session.started","status":"running"}
{"type":"goal.started","goal_id":"g1","message":"do it"}
{"type":"goal.done","goal_id":"g1","status":"done","message":"finished"}
{"type":"session.done","status":"done"}
`

func newTestServer(t *testing.T, l Launcher, toolsDir string) *httptest.Server {
	t.Helper()
	mgr := NewManager(context.Background(), l)
	srv := httptest.NewServer(NewServer(mgr, toolsDir).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postSession(t *testing.T, base, body string) (*http.Response, Session) {
	t.Helper()
	resp, err := http.Post(base+"/sessions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /sessions: %v", err)
	}
	var view Session
	if resp.StatusCode == http.StatusCreated {
		if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
			t.Fatalf("decoding session: %v", err)
		}
	}
	return resp, view
}

func getSession(t *testing.T, base, id string) Session {
	t.Helper()
	resp, err := http.Get(base + "/sessions/" + id)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer resp.Body.Close()
	var view Session
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decoding session: %v", err)
	}
	return view
}

func waitForStatus(t *testing.T, base, id, want string) Session {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view := getSession(t, base, id)
		if view.Status == want {
			return view
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("session %s did not reach status %q (last: %s)", id, want, getSession(t, base, id).Status)
	return Session{}
}

// ---- tests ------------------------------------------------------------------

func TestCreateSession_LaunchesAndTracksGoals(t *testing.T) {
	l := &fakeLauncher{events: doneEvents}
	srv := newTestServer(t, l, t.TempDir())

	resp, created := postSession(t, srv.URL, `{"goals":["do it"],"model":"test/model"}`)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status: got %d", resp.StatusCode)
	}
	resp.Body.Close()

	if len(l.specs) != 1 || l.specs[0].Goals[0] != "do it" {
		t.Fatalf("launcher did not receive the spec: %+v", l.specs)
	}

	view := waitForStatus(t, srv.URL, created.ID, string(session.StatusDone))
	if len(view.Goals) != 1 || view.Goals[0].Status != session.GoalDone {
		t.Errorf("unexpected goals: %+v", view.Goals)
	}
	if view.Goals[0].Note != "finished" {
		t.Errorf("goal note not recorded: %+v", view.Goals[0])
	}
}

func TestCreateSession_NoGoals(t *testing.T) {
	srv := newTestServer(t, &fakeLauncher{}, t.TempDir())
	resp, _ := postSession(t, srv.URL, `{"model":"m"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestCreateSession_LaunchFailure(t *testing.T) {
	srv := newTestServer(t, &fakeLauncher{err: context.DeadlineExceeded}, t.TempDir())
	resp, _ := postSession(t, srv.URL, `{"goals":["g"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", resp.StatusCode)
	}
}

func TestListSessions(t *testing.T) {
	srv := newTestServer(t, &fakeLauncher{events: doneEvents}, t.TempDir())
	for i := 0; i < 2; i++ {
		resp, _ := postSession(t, srv.URL, `{"goals":["g"]}`)
		resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var views []Session
	if err := json.NewDecoder(resp.Body).Decode(&views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(views))
	}
}

func TestGetSession_NotFound(t *testing.T) {
	srv := newTestServer(t, &fakeLauncher{}, t.TempDir())
	resp, err := http.Get(srv.URL + "/sessions/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestStopSession(t *testing.T) {
	srv := newTestServer(t, &fakeLauncher{proc: newBlockingProc()}, t.TempDir())

	resp, created := postSession(t, srv.URL, `{"goals":["long task"]}`)
	resp.Body.Close()

	resp, err := http.Post(srv.URL+"/sessions/"+created.ID+"/stop", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stop status: got %d", resp.StatusCode)
	}

	waitForStatus(t, srv.URL, created.ID, string(session.StatusStopped))
}

func TestStreamEvents_ReplaysTerminalSession(t *testing.T) {
	srv := newTestServer(t, &fakeLauncher{events: doneEvents}, t.TempDir())
	resp, created := postSession(t, srv.URL, `{"goals":["g"]}`)
	resp.Body.Close()
	waitForStatus(t, srv.URL, created.ID, string(session.StatusDone))

	stream, err := http.Get(srv.URL + "/sessions/" + created.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if ct := stream.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type: got %q", ct)
	}
	data, _ := io.ReadAll(stream.Body)
	for _, want := range []string{"session.started", "goal.done", "session.done"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in SSE stream: %s", want, data)
		}
	}
	if strings.Count(string(data), "data: ") != 4 {
		t.Errorf("expected 4 SSE events, got %d:\n%s", strings.Count(string(data), "data: "), data)
	}
}

func TestListTools(t *testing.T) {
	dir := t.TempDir()
	toolDir := filepath.Join(dir, "t1", "bin")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"t1","description":"a tool","version":"1.2.3","executable":"./bin/t1","required_capabilities":["network-manager"]}`
	if err := os.WriteFile(filepath.Join(dir, "t1", "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, &fakeLauncher{}, dir)
	resp, err := http.Get(srv.URL + "/tools")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var views []toolView
	if err := json.NewDecoder(resp.Body).Decode(&views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Name != "t1" || views[0].Version != "1.2.3" {
		t.Errorf("unexpected tools: %+v", views)
	}
}

func TestListCapabilities(t *testing.T) {
	srv := newTestServer(t, &fakeLauncher{}, t.TempDir())
	resp, err := http.Get(srv.URL + "/capabilities")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var names []string
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"wallet": true, "solana": true, "memory": true}
	for _, n := range names {
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing capabilities: %v (got %v)", want, names)
	}
}

// ---- ExecLauncher -----------------------------------------------------------

func TestExecLauncher_StreamsEventsAndWaits(t *testing.T) {
	l := ExecLauncher{Command: `cat >/dev/null; printf '%s\n' '{"type":"session.done","status":"done"}'`}
	proc, err := l.Start(context.Background(), session.Spec{Goals: []string{"g"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	out, _ := io.ReadAll(proc.Events())
	if err := proc.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(string(out), "session.done") {
		t.Errorf("unexpected event stream: %q", out)
	}
}

func TestExecLauncher_Stop(t *testing.T) {
	l := ExecLauncher{Command: `cat >/dev/null; exec sleep 30`}
	proc, err := l.Start(context.Background(), session.Spec{Goals: []string{"g"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := proc.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := proc.Wait(); err == nil {
		t.Error("expected a non-nil wait error after killing the process")
	}
}
