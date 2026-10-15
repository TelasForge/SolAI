// Package web is the minimal server-rendered UI for the control plane.
//
// It is deliberately small: a form to start a session and a list of sessions.
// Routing uses chi as a thin layer (path params, static files); templates are
// templ; interactivity is htmx.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/melodyogonna/solai/controlplane"
	"github.com/melodyogonna/solai/session"
)

//go:generate go tool templ generate

//go:embed static
var staticFS embed.FS

// New returns the web UI router.
func New(mgr *controlplane.Manager) *chi.Mux {
	h := &handler{mgr: mgr}
	r := chi.NewRouter()
	r.Get("/", h.index)
	r.Post("/sessions", h.createSession)
	r.Get("/sessions/{id}", h.session)
	if sub, err := fs.Sub(staticFS, "static"); err == nil {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	}
	return r
}

type handler struct{ mgr *controlplane.Manager }

func (h *handler) index(w http.ResponseWriter, r *http.Request) {
	render(w, r, indexPage(h.mgr.List()))
}

func (h *handler) createSession(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	goals := splitGoals(r.Form.Get("goals"))
	if len(goals) == 0 {
		http.Error(w, "at least one goal is required", http.StatusBadRequest)
		return
	}
	if _, err := h.mgr.Create(session.Spec{Goals: goals}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	render(w, r, sessionsList(h.mgr.List()))
}

func (h *handler) session(w http.ResponseWriter, r *http.Request) {
	view, err := h.mgr.Get(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, sessionPage(view))
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = c.Render(r.Context(), w)
}

func splitGoals(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
