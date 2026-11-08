package mockhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// RequestFilter selects retained request events. Zero fields match anything.
type RequestFilter struct {
	// Method matches the request method, ignoring case.
	Method string
	// Path matches the request's URL path exactly, without the query.
	Path string
	// Route matches the name of the route that answered.
	Route string
	// Matched, when set, keeps only requests a route did (true) or did not
	// (false) answer. Requests forwarded to --upstream count as unmatched.
	Matched *bool
	// Since keeps only events with an ID greater than it.
	Since uint64
}

// Requests returns the retained request events that pass the filter, oldest
// first. The log keeps the most recent 1,000 requests since the last clear.
func (s *Server) Requests(filter RequestFilter) []RequestEvent {
	s.mu.Lock()
	events := append([]RequestEvent(nil), s.events...)
	s.mu.Unlock()
	out := events[:0]
	for _, event := range events {
		if filter.accepts(event) {
			out = append(out, event)
		}
	}
	return out
}

func (f RequestFilter) accepts(event RequestEvent) bool {
	if event.ID <= f.Since {
		return false
	}
	if f.Method != "" && !strings.EqualFold(f.Method, event.Request.Method) {
		return false
	}
	if f.Path != "" && requestPath(event.Request.URL) != f.Path {
		return false
	}
	matched := event.Match.Route != nil
	if f.Matched != nil && *f.Matched != matched {
		return false
	}
	if f.Route != "" && (!matched || event.Match.Route.Name != f.Route) {
		return false
	}
	return true
}

// requestPath returns the decoded path of a logged request URI.
func requestPath(uri string) string {
	u, err := url.ParseRequestURI(uri)
	if err != nil {
		path, _, _ := strings.Cut(uri, "?")
		return path
	}
	return u.Path
}

// ServeRequests handles GET /requests: the request log as a JSON array,
// narrowed by the method, path, route, matched and since query parameters.
func (s *Server) ServeRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	filter, err := parseRequestFilter(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	events := s.Requests(filter)
	if events == nil {
		events = []RequestEvent{}
	}
	s.mu.Lock()
	session := s.session
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Mock-Session", session)
	_ = json.NewEncoder(w).Encode(events)
}

func parseRequestFilter(query url.Values) (RequestFilter, error) {
	filter := RequestFilter{
		Method: query.Get("method"),
		Path:   query.Get("path"),
		Route:  query.Get("route"),
	}
	if raw := query.Get("matched"); raw != "" {
		matched, err := strconv.ParseBool(raw)
		if err != nil {
			return RequestFilter{}, fmt.Errorf("invalid matched %q: use true or false", raw)
		}
		filter.Matched = &matched
	}
	if raw := query.Get("since"); raw != "" {
		since, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return RequestFilter{}, fmt.Errorf("invalid since %q: use an event id", raw)
		}
		filter.Since = since
	}
	return filter, nil
}
