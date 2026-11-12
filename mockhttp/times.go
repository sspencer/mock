package mockhttp

import (
	"net/http"
	"sync/atomic"

	"github.com/sspencer/mock/restclient"
)

// usedUp reports whether a @times route has answered all its requests.
func usedUp(method *restclient.Method, uses *atomic.Uint64) bool {
	return method.Times > 0 && uses != nil && uses.Load() >= uint64(method.Times)
}

// takeUse counts one answer against a @times route, and reports false when
// none is left (another request took the last one first).
func takeUse(method *restclient.Method, uses *atomic.Uint64) bool {
	if method.Times <= 0 {
		return true
	}
	for {
		n := uses.Load()
		if n >= uint64(method.Times) {
			return false
		}
		if uses.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

// timesLeft is how many more requests a @times route answers, or nil for a
// route without @times.
func timesLeft(method *restclient.Method, uses *atomic.Uint64) *int {
	if method.Times <= 0 || uses == nil {
		return nil
	}
	left := max(0, method.Times-int(uses.Load()))
	return &left
}

// Reset returns the server's run-time state to where it started, without a
// reload: @times counts, @rate-limit budgets and @match.chance draws start
// over, and run-time fault overrides, route switches and temporary routes
// are removed. Hit
// counts, the request log and the --strict tally are kept.
func (s *Server) Reset() {
	s.mu.Lock()
	for i := range s.methods {
		s.uses[i].Store(0)
		s.rolls[i].Store(0)
	}
	s.mu.Unlock()
	s.limitMu.Lock()
	clear(s.limiters)
	s.limitMu.Unlock()
	_, _ = s.ClearFaults("")
	s.RemoveRoutes("")
}

// ServeReset handles the admin POST /reset endpoint (see Reset) and answers
// with the configuration state.
func (s *Server) ServeReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.Reset()
	s.ServeState(w, httpGet(r))
}

// httpGet returns a GET copy of r, for answering with a GET handler.
func httpGet(r *http.Request) *http.Request {
	get := r.Clone(r.Context())
	get.Method = http.MethodGet
	return get
}
