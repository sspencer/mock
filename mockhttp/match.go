package mockhttp

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sspencer/mock/restclient"
)

type routeMatch struct {
	method *restclient.Method
	values map[string]string
	index  int
}

func (s *Server) findMethod(r *http.Request) (*restclient.Method, map[string]string, MatchInfo, bool) {
	// Keep matching and rotation selection in one configuration revision.
	// The immutable selected method remains valid after SetMethods replaces
	// the route slice, and old requests cannot advance a new revision's counters.
	s.mu.Lock()
	methods := s.methods
	defer s.mu.Unlock()

	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, nil, MatchInfo{Revision: s.config.Revision, Candidates: closestRoutes(methods, r)}, false
	}
	requestHeaders := requestEventHeaders(r)
	var matches []routeMatch
	for i := range methods {
		method := &methods[i]
		if method.Method != r.Method {
			continue
		}
		pattern := (&url.URL{Path: method.Path, RawPath: method.EscapedPath}).EscapedPath()
		values, ok := matchPath(pattern, r.URL.EscapedPath())
		if !ok || !queryMatches(method.Query, query) {
			continue
		}
		if !headerMatches(method.MatchHeaders, requestHeaders) {
			continue
		}
		for name, queryValues := range query {
			if _, exists := values[name]; !exists && len(queryValues) > 0 {
				values[name] = queryValues[0]
			}
		}
		matches = append(matches, routeMatch{method: method, values: values, index: i})
	}
	if len(matches) == 0 {
		return nil, nil, MatchInfo{Revision: s.config.Revision, Candidates: closestRoutes(methods, r)}, false
	}

	selected := s.nextMatch(matches)
	route := describeRoute(*matches[selected].method, matches[selected].index, s.config.Revision)
	return matches[selected].method, matches[selected].values, MatchInfo{Route: &route, Position: selected + 1, Total: len(matches), Revision: s.config.Revision}, true
}

func (s *Server) nextMatch(matches []routeMatch) int {
	count := len(matches)
	if count == 1 {
		return 0
	}
	key := rotationKey(matches)
	selected := s.counters[key] % count
	s.counters[key] = (selected + 1) % count
	return selected
}

func rotationKey(matches []routeMatch) string {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		parts = append(parts, fmt.Sprintf("%p", m.method))
	}
	return strings.Join(parts, "|")
}

func matchPath(pattern string, requestPath string) (map[string]string, bool) {
	if strings.HasSuffix(pattern, "/") && strings.TrimSuffix(requestPath, "index.html") == pattern {
		requestPath = pattern
	}
	patternParts := splitPath(pattern)
	requestParts := splitPath(requestPath)
	if len(patternParts) != len(requestParts) {
		return nil, false
	}

	values := make(map[string]string)
	for i := range patternParts {
		if key, ok := strings.CutPrefix(patternParts[i], ":"); ok {
			if key == "" {
				return nil, false
			}
			value, err := url.PathUnescape(requestParts[i])
			if err != nil {
				return nil, false
			}
			values[key] = value
			continue
		}
		literal, err := url.PathUnescape(requestParts[i])
		expected, patternErr := url.PathUnescape(patternParts[i])
		if err != nil || patternErr != nil || expected != literal {
			return nil, false
		}
	}
	return values, true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func queryMatches(expected url.Values, actual url.Values) bool {
	for key, expectedValues := range expected {
		actualValues, ok := actual[key]
		if !ok || len(actualValues) < len(expectedValues) {
			return false
		}
		for i, expectedValue := range expectedValues {
			if actualValues[i] != expectedValue {
				return false
			}
		}
	}
	return true
}

// headerMatches requires every expected header name to be present with a matching
// value. Expected value "*" matches any non-empty request header value.
func headerMatches(expected http.Header, actual http.Header) bool {
	for name, expectedValues := range expected {
		actualValues := actual.Values(name)
		if len(actualValues) == 0 {
			return false
		}
		for _, expectedValue := range expectedValues {
			if expectedValue == "*" {
				if strings.TrimSpace(actualValues[0]) == "" {
					return false
				}
				continue
			}
			if !headerValuePresent(actualValues, expectedValue) {
				return false
			}
		}
	}
	return true
}

func headerValuePresent(actual []string, want string) bool {
	for _, value := range actual {
		if value == want {
			return true
		}
	}
	return false
}
