package mockhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// requestBody is the part of a request body that body matchers see.
// unavailable is set when the body was larger than maxMatchBodyBytes or could
// not be read; then no body matcher matches.
type requestBody struct {
	data        []byte
	unavailable bool
}

// requestFacts is what routes are matched against, computed once per request.
// The JSON body is decoded lazily, on the first @json matcher.
type requestFacts struct {
	r         *http.Request
	query     url.Values
	queryErr  error
	headers   http.Header
	localPort int
	body      requestBody

	jsonDone bool
	json     any
	jsonErr  error
}

func newRequestFacts(r *http.Request, body requestBody) *requestFacts {
	query, err := url.ParseQuery(r.URL.RawQuery)
	return &requestFacts{r: r, query: query, queryErr: err, headers: requestEventHeaders(r), localPort: requestLocalPort(r), body: body}
}

// findMethod returns the first route, in load order, that matches r.
func (s *Server) findMethod(r *http.Request, body requestBody) (*restclient.Method, map[string]string, MatchInfo, bool) {
	// Routes are immutable once installed; SetMethods swaps the slice, so a
	// snapshot keeps one request within one configuration revision.
	s.mu.Lock()
	methods, revision := s.methods, s.config.Revision
	s.mu.Unlock()

	facts := newRequestFacts(r, body)
	if facts.queryErr == nil {
		for i := range methods {
			method := &methods[i]
			if values, ok := facts.match(method); ok {
				route := describeRoute(*method, i, revision)
				return method, values, MatchInfo{Route: &route, Revision: revision}, true
			}
		}
	}
	return nil, nil, MatchInfo{Revision: revision, Candidates: closestRoutes(methods, facts)}, false
}

// match reports whether method accepts the request and returns the path and
// query values available to its templates.
func (f *requestFacts) match(method *restclient.Method) (map[string]string, bool) {
	if method.Method != f.r.Method || !portMatches(method.Port, f.localPort) {
		return nil, false
	}
	values, ok := matchPath(routePattern(*method), f.r.URL.EscapedPath())
	if !ok || !queryMatches(method.Query, f.query) || !queryMatcherMatches(method.MatchQuery, f.query) {
		return nil, false
	}
	if !pathMatcherMatches(method.MatchPath, values) || !headerMatches(method.MatchHeaders, f.headers) {
		return nil, false
	}
	if method.MatchBody != "" && !f.bodyMatches(method.MatchBody) {
		return nil, false
	}
	for path, expected := range method.MatchJSON {
		if !f.jsonMatches(path, expected) {
			return nil, false
		}
	}
	for name, queryValues := range f.query {
		if _, exists := values[name]; !exists && len(queryValues) > 0 {
			values[name] = queryValues[0]
		}
	}
	return values, true
}

func routePattern(method restclient.Method) string {
	return (&url.URL{Path: method.Path, RawPath: method.EscapedPath}).EscapedPath()
}

func (f *requestFacts) bodyMatches(expected string) bool {
	if f.body.unavailable {
		return false
	}
	return restclient.MustBodyMatcher(expected).Match(f.bodyValues())
}

// bodyValues is the body as matcher values: none when it is empty.
func (f *requestFacts) bodyValues() []string {
	if len(f.body.data) == 0 {
		return nil
	}
	return []string{string(f.body.data)}
}

func (f *requestFacts) jsonMatches(path, expected string) bool {
	values, err := f.jsonValues(path)
	return err == nil && restclient.MustMatcher(expected).Match(values)
}

// jsonValues looks up a dot-separated field path in the JSON body. Numeric
// segments index arrays. A missing field or null is no value; strings are
// their text; other values are compact JSON. A blank body has no fields; any
// other body that is not JSON is an error, and then no @json matcher matches.
func (f *requestFacts) jsonValues(path string) ([]string, error) {
	if f.body.unavailable {
		return nil, errBodyUnavailable
	}
	if !f.jsonDone {
		f.jsonDone = true
		if len(bytes.TrimSpace(f.body.data)) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(f.body.data))
			decoder.UseNumber()
			if err := decoder.Decode(&f.json); err != nil {
				f.jsonErr = errBodyNotJSON
			} else if decoder.More() {
				f.jsonErr = errBodyNotJSON
			}
		}
	}
	if f.jsonErr != nil {
		return nil, f.jsonErr
	}
	value := f.json
	for _, segment := range strings.Split(path, ".") {
		switch node := value.(type) {
		case map[string]any:
			value = node[segment]
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) {
				return nil, nil
			}
			value = node[index]
		default:
			return nil, nil
		}
	}
	switch value := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{value}, nil
	default:
		text, err := json.Marshal(value)
		if err != nil {
			return nil, nil
		}
		return []string{string(text)}, nil
	}
}

var (
	errBodyUnavailable = errors.New("request body is larger than 1 MiB or could not be read")
	errBodyNotJSON     = errors.New("request body is not valid JSON")
)

// requestLocalPort returns the port of the listener that accepted r, or 0 when
// it is unknown (as for requests built in tests without a connection).
func requestLocalPort(r *http.Request) int {
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// portMatches reports whether a route declared for port serves a request that
// arrived on localPort. A route without a port, or a request whose listener
// is unknown, matches any port.
func portMatches(port, localPort int) bool {
	return port == 0 || localPort == 0 || port == localPort
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

// headerMatches requires every expected header matcher to accept the request's
// values for that header: an exact value, "*" (present and non-blank), "!"
// (missing or blank), or "~pattern" (a full-value regular expression).
func headerMatches(expected http.Header, actual http.Header) bool {
	for name, expectedValues := range expected {
		actualValues := actual.Values(name)
		for _, expectedValue := range expectedValues {
			if !restclient.MustMatcher(expectedValue).Match(actualValues) {
				return false
			}
		}
	}
	return true
}

// queryMatcherMatches applies @query.name matchers to the request query.
func queryMatcherMatches(expected url.Values, actual url.Values) bool {
	for name, expectedValues := range expected {
		for _, expectedValue := range expectedValues {
			if !restclient.MustMatcher(expectedValue).Match(actual[name]) {
				return false
			}
		}
	}
	return true
}

// pathMatcherMatches applies @path.name matchers to the decoded path parameters.
func pathMatcherMatches(expected map[string]string, params map[string]string) bool {
	for name, expectedValue := range expected {
		value, ok := params[name]
		if !ok || !restclient.MustMatcher(expectedValue).Match([]string{value}) {
			return false
		}
	}
	return true
}
