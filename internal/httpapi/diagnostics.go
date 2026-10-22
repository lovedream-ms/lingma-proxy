package httpapi

import (
	"context"
	"lingma-ipc-proxy/internal/observe"
	"net/http"
	"sort"
)

func (s *Server) Activity() []observe.Snapshot {
	s.activeMu.Lock()
	defer s.activeMu.Unlock()
	out := []observe.Snapshot{}
	for _, trace := range s.active {
		out = append(out, trace.Snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, 200, s.Activity())
}
func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.svc.DiagnosticTimeout())
	defer cancel()
	writeJSON(w, 200, s.svc.Diagnose(ctx, r.Method == http.MethodPost))
}
