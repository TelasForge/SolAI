package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/melodyogonna/solai/config"
	"github.com/melodyogonna/solai/controlplane"
	"github.com/melodyogonna/solai/session"
)

type nopProc struct{ r io.Reader }

func (p nopProc) Events() io.Reader { return p.r }
func (p nopProc) Stop() error       { return nil }
func (p nopProc) Wait() error       { return nil }

type nopLauncher struct{}

func (nopLauncher) Start(context.Context, session.Spec, []string) (controlplane.RuntimeProcess, error) {
	return nopProc{r: strings.NewReader("")}, nil
}

func newServer(t *testing.T) (*httptest.Server, *controlplane.Manager) {
	t.Helper()
	mgr := controlplane.NewManager(context.Background(), config.Config{}, nopLauncher{})
	srv := httptest.NewServer(New(mgr))
	t.Cleanup(srv.Close)
	return srv, mgr
}

func TestIndexRenders(t *testing.T) {
	srv, _ := newServer(t)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"Start a session", "htmx.min.js", "Sessions"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("index missing %q: %s", want, body)
		}
	}
}

func TestCreateAndViewSession(t *testing.T) {
	srv, mgr := newServer(t)

	resp, err := http.PostForm(srv.URL+"/sessions", url.Values{"goals": {"do the thing"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status: got %d", resp.StatusCode)
	}

	sessions := mgr.List()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	id := sessions[0].ID

	view, err := http.Get(srv.URL + "/sessions/" + id)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Body.Close()
	body, _ := io.ReadAll(view.Body)
	if !strings.Contains(string(body), "do the thing") {
		t.Errorf("session page missing goal: %s", body)
	}
}

func TestCreateSession_NoGoals(t *testing.T) {
	srv, _ := newServer(t)
	resp, err := http.PostForm(srv.URL+"/sessions", url.Values{"goals": {"   "}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestStaticAssetServed(t *testing.T) {
	srv, _ := newServer(t)
	resp, err := http.Get(srv.URL + "/static/app.css")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d", resp.StatusCode)
	}
}
