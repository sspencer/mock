package mockhttp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/sspencer/mock/restclient"
)

// RouteInfo describes a fixture without reading potentially large body files.
type RouteInfo struct {
	ID            string            `json:"id"`
	Revision      uint64            `json:"revision"`
	Name          string            `json:"name"`
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         string            `json:"query,omitempty"`
	Source        string            `json:"source"`
	Line          int               `json:"line"`
	Status        int               `json:"status"`
	Delay         string            `json:"delay"`
	File          string            `json:"file,omitempty"`
	Headers       http.Header       `json:"headers"`
	MatchHeaders  http.Header       `json:"matchHeaders"`
	Variables     map[string]string `json:"variables"`
	Body          string            `json:"body"`
	BodyTruncated bool              `json:"bodyTruncated"`
}

type MatchInfo struct {
	Route      *RouteInfo      `json:"route,omitempty"`
	Position   int             `json:"position,omitempty"`
	Total      int             `json:"total,omitempty"`
	Revision   uint64          `json:"revision"`
	Candidates []RouteMismatch `json:"candidates,omitempty"`
}

type RouteMismatch struct {
	Name    string   `json:"name"`
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Source  string   `json:"source"`
	Line    int      `json:"line"`
	Reasons []string `json:"reasons"`
	score   int
}

type ConfigState struct {
	Revision    uint64 `json:"revision"`
	RouteCount  int    `json:"routeCount"`
	LastAttempt string `json:"lastAttempt"`
	LastSuccess string `json:"lastSuccess"`
	Loading     bool   `json:"loading"`
	Error       string `json:"error,omitempty"`
}

func describeRoute(method restclient.Method, index int, revision uint64) RouteInfo {
	body := method.Body
	truncated := len(body) > maxLoggedBodyBytes
	if truncated {
		body = body[:maxLoggedBodyBytes]
	}
	return RouteInfo{ID: fmt.Sprintf("%d:%d", revision, index), Revision: revision, Name: method.Name, Method: method.Method,
		Path: (&url.URL{Path: method.Path, RawPath: method.EscapedPath}).EscapedPath(), Query: method.Query.Encode(), Source: method.Source, Line: method.Line, Status: method.Status,
		Delay: method.Delay.String(), File: method.Variables["file"], Headers: method.Headers.Clone(), MatchHeaders: method.MatchHeaders.Clone(),
		Variables: cloneVariables(method.Variables), Body: body, BodyTruncated: truncated}
}

func cloneVariables(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func (s *Server) BeginReload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Loading = true
	s.config.LastAttempt = time.Now().UTC().Format(time.RFC3339Nano)
}

func (s *Server) ReloadFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Loading = false
	s.config.Error = err.Error()
}

func (s *Server) ServeState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", 405)
		return
	}
	s.mu.Lock()
	state := s.config
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(state)
}

// Resetting fixture sequences is independent of clearing diagnostic history.
func (s *Server) ServeReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden", 403)
		return
	}
	s.ResetCounters()
	w.WriteHeader(http.StatusNoContent)
}

func mismatch(method restclient.Method, r *http.Request, actual url.Values, queryErr error, headers http.Header) RouteMismatch {
	pattern := (&url.URL{Path: method.Path, RawPath: method.EscapedPath}).EscapedPath()
	result := RouteMismatch{Name: method.Name, Method: method.Method, Path: pattern, Source: method.Source, Line: method.Line}
	if _, ok := matchPath(pattern, r.URL.EscapedPath()); !ok {
		result.Reasons = append(result.Reasons, fmt.Sprintf("Path %q does not match %q", r.URL.EscapedPath(), pattern))
		result.score += 10
		result.score += abs(len(splitPath(pattern)) - len(splitPath(r.URL.EscapedPath())))
	}
	if method.Method != r.Method {
		result.Reasons = append(result.Reasons, fmt.Sprintf("Expected method %s; received %s", method.Method, r.Method))
		result.score += 4
	}
	if queryErr != nil {
		result.Reasons = append(result.Reasons, "Request query is malformed: "+queryErr.Error())
		result.score++
	} else {
		for _, key := range sortedKeys(method.Query) {
			expected := method.Query[key]
			if !queryMatches(url.Values{key: expected}, actual) {
				result.Reasons = append(result.Reasons, fmt.Sprintf("Query %s: expected %q; received %q", key, expected, actual[key]))
				result.score++
			}
		}
	}
	for _, key := range sortedKeys(method.MatchHeaders) {
		if !headerMatches(http.Header{key: method.MatchHeaders[key]}, headers) {
			// Do not repeat credential values in diagnostic summaries.
			reason := "is missing"
			if len(headers.Values(key)) > 0 {
				reason = "does not match"
			}
			result.Reasons = append(result.Reasons, fmt.Sprintf("Header %s: required value %s", key, reason))
			result.score++
		}
	}
	return result
}
func sortedKeys[M ~map[string][]string](values M) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func closestRoutes(methods []restclient.Method, r *http.Request) []RouteMismatch {
	query, err := url.ParseQuery(r.URL.RawQuery)
	headers := requestEventHeaders(r)
	candidates := make([]RouteMismatch, 0, len(methods))
	for _, method := range methods {
		candidates = append(candidates, mismatch(method, r, query, err, headers))
	}
	slices.SortStableFunc(candidates, func(a, b RouteMismatch) int { return a.score - b.score })
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	for i := range candidates {
		if len(candidates[i].Reasons) == 0 {
			candidates[i].Reasons = []string{"No matching response available"}
		}
	}
	return candidates
}
