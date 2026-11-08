package mockhttp

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/sspencer/mock/restclient"
)

// maxUnmatchedKinds caps how many distinct unmatched method and path pairs
// are remembered; later ones are still counted in UnmatchedTotal.
const maxUnmatchedKinds = 100

// Coverage reports how often each current route answered and which requests
// no route answered, since the server started.
type Coverage struct {
	Routes []RouteHits `json:"routes"`
	// Unmatched lists distinct unmatched requests in the order first seen.
	Unmatched []UnmatchedRequest `json:"unmatched"`
	// UnmatchedTotal counts every unmatched request, including ones beyond
	// the Unmatched list's cap.
	UnmatchedTotal uint64 `json:"unmatchedTotal"`
}

// RouteHits is a route's location and how many requests it answered.
type RouteHits struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Port   int    `json:"port,omitempty"`
	Source string `json:"source"`
	Line   int    `json:"line"`
	Hits   uint64 `json:"hits"`
}

// UnmatchedRequest counts requests with one method and path that no route
// answered. Proxied is set when they were forwarded with --upstream.
type UnmatchedRequest struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Count   uint64 `json:"count"`
	Proxied bool   `json:"proxied,omitempty"`
}

type unmatchedLog struct {
	total uint64
	list  []UnmatchedRequest
	index map[string]int
}

// routeKey identifies a route across reloads: editing an unrelated section
// moves line numbers, so the line is not part of it.
func routeKey(method restclient.Method) string {
	return strings.Join([]string{method.Source, method.Name, method.Method, strconv.Itoa(method.Port), routePattern(method), method.Query.Encode()}, "\x00")
}

// carryHits returns hit counters for methods, reusing the counter of a route
// with the same identity in the previous configuration so counts survive a
// reload. Identical routes are paired in load order.
func carryHits(methods, previous []restclient.Method, hits []*atomic.Uint64) []*atomic.Uint64 {
	old := make(map[string][]*atomic.Uint64, len(previous))
	for i, method := range previous {
		key := routeKey(method)
		old[key] = append(old[key], hits[i])
	}
	out := make([]*atomic.Uint64, len(methods))
	for i, method := range methods {
		key := routeKey(method)
		if counters := old[key]; len(counters) > 0 {
			out[i], old[key] = counters[0], counters[1:]
		} else {
			out[i] = new(atomic.Uint64)
		}
	}
	return out
}

func (s *Server) recordUnmatched(r *http.Request, proxied bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log := &s.unmatched
	log.total++
	key := r.Method + " " + r.URL.EscapedPath()
	if i, ok := log.index[key]; ok {
		log.list[i].Count++
		log.list[i].Proxied = log.list[i].Proxied || proxied
		return
	}
	if len(log.list) >= maxUnmatchedKinds {
		return
	}
	if log.index == nil {
		log.index = make(map[string]int)
	}
	log.index[key] = len(log.list)
	log.list = append(log.list, UnmatchedRequest{Method: r.Method, Path: r.URL.EscapedPath(), Count: 1, Proxied: proxied})
}

// Coverage returns hit counts for the current routes and the requests no
// route answered. Clearing the request log does not reset them.
func (s *Server) Coverage() Coverage {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes := make([]RouteHits, len(s.methods))
	for i, method := range s.methods {
		routes[i] = RouteHits{Name: method.Name, Method: method.Method, Path: routePattern(method), Port: method.Port,
			Source: method.Source, Line: method.Line, Hits: s.hits[i].Load()}
		if query := method.Query.Encode(); query != "" {
			routes[i].Path += "?" + query
		}
	}
	return Coverage{Routes: routes, Unmatched: append([]UnmatchedRequest(nil), s.unmatched.list...), UnmatchedTotal: s.unmatched.total}
}
