package mockhttp

import (
	"encoding/json"
	"net/http"
)

// SetReloader installs the function POST /reload calls to re-read the
// request files. Without one, /reload answers 409.
func (s *Server) SetReloader(reload func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloader = reload
}

// ServeReload handles POST /reload: it reloads the request files now and
// answers with the resulting state, 200 when the reload succeeded and 422
// when it failed (the previous routes stay active). It applies the same
// cross-site checks as /clear.
func (s *Server) ServeReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	reload := s.reloader
	s.mu.Unlock()
	if reload == nil {
		http.Error(w, "nothing to reload: routes were read from stdin", http.StatusConflict)
		return
	}
	reload()
	s.mu.Lock()
	state := s.config
	s.mu.Unlock()
	status := http.StatusOK
	if state.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(state)
}
