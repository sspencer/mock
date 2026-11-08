// Package mocktest runs a mock server from .http request files inside a Go
// test and asserts on the requests it received.
//
//	func TestSignup(t *testing.T) {
//		api := mocktest.Start(t, "testdata/api.http")
//		client := NewClient(api.URL)
//		// ... exercise the code under test ...
//		api.AssertCalled(t, "POST /users", 1)
//		api.AssertAllMatched(t)
//	}
//
// Every route is served on the one test server's port; ports in request
// targets and @port are ignored. The server is closed when the test ends.
package mocktest

import (
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

// Options configures StartWith.
type Options struct {
	// Files are .http request files, loaded in order.
	Files []string
	// Source is .http text, loaded after Files.
	Source string
	// Vars set @name variables in every file, as mock's -v flag does.
	Vars map[string]string
	// Quiet discards load warnings instead of logging them with t.Log.
	Quiet bool
}

// Server is a running mock server.
type Server struct {
	// URL is the server's base URL, such as http://127.0.0.1:53124.
	URL string
	// Mock is the underlying server, for anything this package does not wrap.
	Mock *mockhttp.Server
}

// Start serves the routes in the request files until the test ends.
func Start(t testing.TB, files ...string) *Server {
	t.Helper()
	return StartWith(t, Options{Files: files})
}

// StartSource serves the routes in .http source text until the test ends.
func StartSource(t testing.TB, source string) *Server {
	t.Helper()
	return StartWith(t, Options{Source: source})
}

// StartWith serves the routes described by opts until the test ends. It fails
// the test if the routes do not load.
func StartWith(t testing.TB, opts Options) *Server {
	t.Helper()
	parse := restclient.Options{Vars: opts.Vars}
	var methods []restclient.Method
	if len(opts.Files) > 0 {
		loaded, err := parse.Load(opts.Files)
		if err != nil {
			t.Fatalf("mocktest: %v", err)
		}
		methods = append(methods, loaded...)
	}
	if opts.Source != "" {
		parsed, err := parse.Parse("source.http", strings.NewReader(opts.Source))
		if err != nil {
			t.Fatalf("mocktest: %v", err)
		}
		methods = append(methods, parsed...)
	}
	if len(methods) == 0 {
		t.Fatal("mocktest: no routes; pass request files or source text")
	}
	for i := range methods {
		methods[i].Port = 0
	}

	var out io.Writer = io.Discard
	if !opts.Quiet {
		out = testWriter{t}
	}
	logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelWarn}))
	mock := mockhttp.New(methods, logger)
	ts := httptest.NewServer(mock)
	t.Cleanup(ts.Close)
	return &Server{URL: ts.URL, Mock: mock}
}

// testWriter sends log lines to t.Log so they appear with the test's output.
type testWriter struct{ t testing.TB }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// Requests returns the received requests that pass the filter, oldest first.
func (s *Server) Requests(filter mockhttp.RequestFilter) []mockhttp.RequestEvent {
	return s.Mock.Requests(filter)
}

// Calls returns the received requests that call names, oldest first. A call
// is either a method and path, such as "POST /users" (the path must match
// exactly; add ?query to require that exact query string too), or the name
// of the route that answered, such as "Create user".
func (s *Server) Calls(call string) []mockhttp.RequestEvent {
	method, target, ok := splitCall(call)
	if !ok {
		return s.Mock.Requests(mockhttp.RequestFilter{Route: call})
	}
	path, _, hasQuery := strings.Cut(target, "?")
	events := s.Mock.Requests(mockhttp.RequestFilter{Method: method, Path: path})
	if !hasQuery {
		return events
	}
	out := events[:0]
	for _, event := range events {
		if event.Request.URL == target {
			out = append(out, event)
		}
	}
	return out
}

// splitCall splits "METHOD /path" into its parts.
func splitCall(call string) (method, target string, ok bool) {
	method, target, ok = strings.Cut(strings.TrimSpace(call), " ")
	target = strings.TrimSpace(target)
	if !ok || method == "" || !strings.HasPrefix(target, "/") {
		return "", "", false
	}
	for _, c := range method {
		if c < 'A' || c > 'Z' {
			return "", "", false
		}
	}
	return method, target, true
}

// AssertCalled fails the test unless call (see Calls) was received exactly
// n times.
func (s *Server) AssertCalled(t testing.TB, call string, n int) {
	t.Helper()
	if got := len(s.Calls(call)); got != n {
		t.Errorf("mocktest: %s called %d %s, want %d\n%s", call, got, plural(got), n, s.received())
	}
}

// AssertNotCalled fails the test if call (see Calls) was received.
func (s *Server) AssertNotCalled(t testing.TB, call string) {
	t.Helper()
	s.AssertCalled(t, call, 0)
}

// AssertAllMatched fails the test if any received request was answered by no
// route.
func (s *Server) AssertAllMatched(t testing.TB) {
	t.Helper()
	unmatched := false
	if events := s.Mock.Requests(mockhttp.RequestFilter{Matched: &unmatched}); len(events) > 0 {
		var lines []string
		for _, event := range events {
			lines = append(lines, "  "+event.Request.Method+" "+event.Request.URL)
		}
		t.Errorf("mocktest: %d %s matched no route:\n%s", len(events), pluralRequests(len(events)), strings.Join(lines, "\n"))
	}
}

// Clear forgets the requests received so far, so later assertions count only
// what follows.
func (s *Server) Clear() {
	s.Mock.ClearEvents()
}

// received describes every retained request for a failure message.
func (s *Server) received() string {
	events := s.Mock.Requests(mockhttp.RequestFilter{})
	if len(events) == 0 {
		return "no requests received"
	}
	var b strings.Builder
	b.WriteString("requests received:")
	for _, event := range events {
		route := "no route"
		if event.Match.Route != nil {
			route = fmt.Sprintf("%q", event.Match.Route.Name)
		}
		fmt.Fprintf(&b, "\n  %s %s -> %d (%s)", event.Request.Method, event.Request.URL, event.Response.Status, route)
	}
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return "time"
	}
	return "times"
}

func pluralRequests(n int) string {
	if n == 1 {
		return "request"
	}
	return "requests"
}
