package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/melodyogonna/solai/session"
	"github.com/melodyogonna/solai/tool"
)

// Server exposes the control-plane HTTP API.
type Server struct {
	mgr *Manager
	mux *http.ServeMux
}

// NewServer builds a control-plane server.
func NewServer(mgr *Manager) *Server {
	s := &Server{mgr: mgr, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the server's HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("POST /sessions", s.createSession)
	s.mux.HandleFunc("GET /sessions", s.listSessions)
	s.mux.HandleFunc("GET /sessions/{id}", s.getSession)
	s.mux.HandleFunc("POST /sessions/{id}/stop", s.stopSession)
	s.mux.HandleFunc("GET /sessions/{id}/events", s.streamEvents)
	s.mux.HandleFunc("GET /tools", s.listTools)
	s.mux.HandleFunc("GET /capabilities", s.listCapabilities)
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var spec session.Spec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid session spec: "+err.Error())
		return
	}

	view, err := s.mgr.Create(spec)
	switch {
	case errors.Is(err, ErrNoGoals), errors.Is(err, ErrUnknownCapability):
		writeError(w, http.StatusBadRequest, err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
	case view != nil:
		writeJSON(w, http.StatusCreated, view)
	}
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mgr.List())
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	view, err := s.mgr.Get(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) stopSession(w http.ResponseWriter, r *http.Request) {
	view, err := s.mgr.Stop(r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	replay, ch, done, unsub, err := s.mgr.Subscribe(id)
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !done {
		defer unsub()
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	writeEvent := func(e session.Event) {
		data, err := json.Marshal(e)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for _, e := range replay {
		writeEvent(e)
	}
	if done {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeEvent(e)
		}
	}
}

func (s *Server) listTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, tool.Summaries())
}

func (s *Server) listCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, CapabilityNames())
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
