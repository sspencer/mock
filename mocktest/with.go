package mocktest

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

// With describes what a received request must carry, for AssertCalledWith.
// Header, Query and Body values use the request-file matcher syntax: exact
// text, "*" (present), "!" (absent), "~regex" (matching the whole value),
// "!text", ">100", "#>0".
type With struct {
	// Header maps a request header name to a matcher.
	Header map[string]string
	// Query maps a query parameter name to a matcher.
	Query map[string]string
	// JSON is a JSON document the body must contain, as @json does: every
	// member it names, with extra members allowed.
	JSON string
	// Body is a matcher for the whole body text, as @body.
	Body string
}

// AssertCalledWith fails the test unless at least one received request
// that call names (see Calls) satisfies every part of want. It returns the
// requests that did.
func (s *Server) AssertCalledWith(t testing.TB, call string, want With) []mockhttp.RequestEvent {
	t.Helper()
	calls := s.Calls(call)
	if len(calls) == 0 {
		t.Errorf("mocktest: %s was not called\n%s", call, s.received())
		return nil
	}
	var matched []mockhttp.RequestEvent
	var misses []string
	for _, event := range calls {
		reason, err := want.mismatch(event)
		if err != nil {
			t.Fatalf("mocktest: AssertCalledWith %s: %v", call, err)
		}
		if reason == "" {
			matched = append(matched, event)
		} else {
			misses = append(misses, fmt.Sprintf("  %s %s: %s", event.Request.Method, event.Request.URL, reason))
		}
	}
	if len(matched) == 0 {
		t.Errorf("mocktest: %s called %d %s, none as wanted:\n%s", call, len(calls), plural(len(calls)), strings.Join(misses, "\n"))
	}
	return matched
}

// mismatch returns why event does not satisfy w, or "" when it does. The
// error reports a malformed expectation.
func (w With) mismatch(event mockhttp.RequestEvent) (string, error) {
	for _, name := range slices.Sorted(maps.Keys(w.Header)) {
		m, err := restclient.ParseMatcher(w.Header[name])
		if err != nil {
			return "", fmt.Errorf("header %s: %v", name, err)
		}
		if values := event.Request.Headers.Values(name); !m.Match(values) {
			return fmt.Sprintf("header %s: want %s; got %s", name, w.Header[name], quoteValues(values)), nil
		}
	}
	if len(w.Query) > 0 {
		query := url.Values{}
		if parsed, err := url.Parse(event.Request.URL); err == nil {
			query = parsed.Query()
		}
		for _, name := range slices.Sorted(maps.Keys(w.Query)) {
			m, err := restclient.ParseMatcher(w.Query[name])
			if err != nil {
				return "", fmt.Errorf("query %s: %v", name, err)
			}
			if values := query[name]; !m.Match(values) {
				return fmt.Sprintf("query %s: want %s; got %s", name, w.Query[name], quoteValues(values)), nil
			}
		}
	}
	body := event.Request.Body
	if w.Body != "" {
		m, err := restclient.ParseBodyMatcher(w.Body)
		if err != nil {
			return "", fmt.Errorf("body: %v", err)
		}
		if body.Encoding != "" || !m.Match([]string{body.Text}) {
			return fmt.Sprintf("body: want %s; got %s", w.Body, preview(body)), nil
		}
	}
	if w.JSON != "" {
		expected, err := restclient.DecodeJSON([]byte(w.JSON))
		if err != nil {
			return "", fmt.Errorf("JSON: %v", err)
		}
		if body.Encoding != "" {
			return "JSON: the body is not text", nil
		}
		actual, err := restclient.DecodeJSON([]byte(body.Text))
		if err != nil {
			return fmt.Sprintf("JSON: the body is not JSON (%v)", err), nil
		}
		if ok, miss := restclient.JSONContains(actual, expected); !ok {
			path := miss.Path
			if path == "" {
				path = "body"
			}
			return fmt.Sprintf("JSON %s: want %s; got %s", path, miss.Expected, miss.Received), nil
		}
	}
	return "", nil
}

func quoteValues(values []string) string {
	if len(values) == 0 {
		return "nothing"
	}
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	return strings.Join(quoted, ", ")
}

func preview(body mockhttp.EventBody) string {
	if body.Encoding != "" {
		return "a binary body"
	}
	text := body.Text
	if len(text) > 60 {
		text = text[:60] + "..."
	}
	return fmt.Sprintf("%q", text)
}

// LastCall returns the most recent received request that call names (see
// Calls), failing the test now if there is none.
func (s *Server) LastCall(t testing.TB, call string) mockhttp.RequestEvent {
	t.Helper()
	calls := s.Calls(call)
	if len(calls) == 0 {
		t.Fatalf("mocktest: %s was not called\n%s", call, s.received())
	}
	return calls[len(calls)-1]
}

// WaitFor waits up to timeout for a request that call names (see Calls),
// for clients that call the server in the background, and returns the first
// one. It fails the test now if none arrives in time.
func (s *Server) WaitFor(t testing.TB, call string, timeout time.Duration) mockhttp.RequestEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if calls := s.Calls(call); len(calls) > 0 {
			return calls[0]
		}
		if time.Now().After(deadline) {
			t.Fatalf("mocktest: %s was not called within %v\n%s", call, timeout, s.received())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// AssertAllRoutesCalled fails the test if any route from the request files
// answered no request, like mock --strict=unused. Routes added with Stub are
// not checked.
func (s *Server) AssertAllRoutesCalled(t testing.TB) {
	t.Helper()
	var unused []string
	for _, route := range s.Mock.Routes() {
		if route.Temporary == "" && route.Hits == 0 {
			unused = append(unused, fmt.Sprintf("  %s %s (%s, %s:%d)", route.Method, route.Path, route.Name, route.Source, route.Line))
		}
	}
	if len(unused) > 0 {
		t.Errorf("mocktest: %d %s never called:\n%s", len(unused), pluralRoutes(len(unused)), strings.Join(unused, "\n"))
	}
}

func pluralRoutes(n int) string {
	if n == 1 {
		return "route"
	}
	return "routes"
}
