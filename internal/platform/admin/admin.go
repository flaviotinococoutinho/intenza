//intenza:paradigm imperative-shell

// Package admin is the operations port every binary exposes apart from its
// public surface: liveness, readiness with named checks, and, when enabled,
// fault knobs for chaos experiments.
package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Check is a named readiness probe.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

// Server is the admin mux.
type Server struct {
	Mux    *http.ServeMux
	mu     sync.Mutex
	faults map[string]Fault
}

// Fault is a knob a chaos experiment can turn.
type Fault struct {
	Describe string
	Set      func(value string) error
	Get      func() string
}

// New builds the admin mux with liveness and readiness.
func New(checks ...Check) *Server {
	s := &Server{Mux: http.NewServeMux(), faults: map[string]Fault{}}
	s.Mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	s.Mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		res := map[string]string{}
		code := http.StatusOK
		for _, c := range checks {
			if err := c.Fn(ctx); err != nil {
				res[c.Name], code = err.Error(), http.StatusServiceUnavailable
				continue
			}
			res[c.Name] = "ok"
		}
		writeJSON(w, code, res)
	})
	return s
}

// EnableFaults exposes GET/POST /debug/faults. Never enable it in production.
func (s *Server) EnableFaults() {
	s.Mux.HandleFunc("GET /debug/faults", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := map[string]map[string]string{}
		for k, f := range s.faults {
			out[k] = map[string]string{"value": f.Get(), "describe": f.Describe}
		}
		writeJSON(w, http.StatusOK, out)
	})
	s.Mux.HandleFunc("POST /debug/faults", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, v := range req {
			f, ok := s.faults[k]
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "falha desconhecida: " + k})
				return
			}
			if err := f.Set(v); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// Register adds a fault knob.
func (s *Server) Register(name string, f Fault) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.faults[name] = f
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
