package mockhttp

import (
	"net/http"
	"net/url"
	"strings"

	"mock/restclient"
)

func (s *Server) findMethod(r *http.Request) (*restclient.Method, map[string]string, bool) {
	type match struct {
		method *restclient.Method
		values map[string]string
	}

	// Snapshot the methods slice under the lock so hot-reload via SetMethods
	// cannot race with matching. Pointers into the snapshot remain valid for
	// this request even after a later SetMethods replaces s.methods.
	s.mu.Lock()
	methods := s.methods
	s.mu.Unlock()

	var matches []match
	for i := range methods {
		method := &methods[i]
		if method.Method != r.Method {
			continue
		}
		values, ok := matchPath(method.Path, r.URL.Path)
		if !ok || !queryMatches(method.Query, r.URL.Query()) {
			continue
		}
		for name, queryValues := range r.URL.Query() {
			if len(queryValues) > 0 {
				values[name] = queryValues[0]
			}
		}
		matches = append(matches, match{method: method, values: values})
	}
	if len(matches) == 0 {
		return nil, nil, false
	}

	selected := s.nextMatch(r, len(matches))
	return matches[selected].method, matches[selected].values, true
}

func (s *Server) nextMatch(r *http.Request, count int) int {
	if count == 1 {
		return 0
	}
	key := r.Method + " " + r.URL.RequestURI()
	s.mu.Lock()
	defer s.mu.Unlock()

	selected := s.counters[key] % count
	s.counters[key]++
	return selected
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
		if patternParts[i] != requestParts[i] {
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
