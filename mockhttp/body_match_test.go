package mockhttp

import (
	"bytes"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func serveBody(t *testing.T, s *Server, target, body string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("POST", target, strings.NewReader(body)))
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestJSONMatchers(t *testing.T) {
	methods, err := restclient.Parse("body.http", strings.NewReader(`### Alice
@json.user.name = alice
POST /login

alice

### Admin
@json.user.roles.0 = admin
POST /login

admin

### Numeric age
@json.user.age = ~[0-9]+
POST /login

aged

### Active flag
@json.active = true
POST /login

active

### Object value
@json.meta = {"a":1}
POST /login

object

### No user
@json.user = !
POST /login

no user

### Fallback
POST /login

fallback
`))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct{ body, want string }{
		{`{"user":{"name":"alice"}}`, "alice"},
		{"{\n  \"user\": {\"name\": \"alice\"}\n}", "alice"},
		{`{"user":{"name":"bob","roles":["admin","dev"]}}`, "admin"},
		{`{"user":{"name":"bob","roles":["dev","admin"]}}`, "fallback"},
		{`{"user":{"age":42}}`, "aged"},
		{`{"user":{"age":"42"}}`, "aged"},
		{`{"user":{"age":4.2}}`, "fallback"},
		{`{"user":{},"active":true}`, "active"},
		{`{"user":{},"active":"true"}`, "active"}, // compared as text
		{`{"user":{},"meta":{"a":1}}`, "object"},
		{`{"user":null}`, "no user"},
		{`{}`, "no user"},
		{``, "no user"},  // a blank body has no fields
		{`not json`, "fallback"}, // invalid JSON matches no @json matcher, even "!"
		{`{"user":1} {"x":2}`, "fallback"},
	}
	for _, tt := range tests {
		if code, body := serveBody(t, s, "/login", tt.body); code != 200 || body != tt.want {
			t.Errorf("POST %q = %d %q, want %q", tt.body, code, body, tt.want)
		}
	}
}

func TestBodyMatchers(t *testing.T) {
	methods, err := restclient.Parse("body.http", strings.NewReader(`### Exact
@body = ping
POST /echo

pong

### Contains
@body = ~.*hello.*
POST /echo

greeting

### Empty
@body = !
POST /echo

empty

### Anything else
@body = *
POST /echo

other
`))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct{ body, want string }{
		{"ping", "pong"},
		{"ping\n", "other"}, // exact means exact
		{"say hello there", "greeting"},
		{"line one\nhello\nline three", "greeting"}, // . matches newlines in @body
		{"", "empty"},
		{"   ", "empty"},
		{"x", "other"},
	}
	for _, tt := range tests {
		if code, body := serveBody(t, s, "/echo", tt.body); code != 200 || body != tt.want {
			t.Errorf("POST %q = %d %q, want %q", tt.body, code, body, tt.want)
		}
	}
}

func TestOversizedBodyMatchesNoBodyMatcher(t *testing.T) {
	methods, err := restclient.Parse("body.http", strings.NewReader("### Any body\n@body = *\nPOST /big\n\nmatched\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if code, _ := serveBody(t, s, "/big", strings.Repeat("a", maxMatchBodyBytes)); code != 200 {
		t.Fatalf("1 MiB body status = %d, want 200", code)
	}
	if code, _ := serveBody(t, s, "/big", strings.Repeat("a", maxMatchBodyBytes+1)); code != 404 {
		t.Fatalf("oversized body status = %d, want 404", code)
	}
	reasons := strings.Join(s.events[len(s.events)-1].Match.Candidates[0].Reasons, "\n")
	if !strings.Contains(reasons, "larger than 1 MiB") {
		t.Fatalf("reasons = %q", reasons)
	}
	if got := s.events[len(s.events)-1].Request.Body; !got.Truncated {
		t.Fatal("logged request body should still be truncated at the capture limit")
	}
}

func TestBodyMismatchReasons(t *testing.T) {
	methods, err := restclient.Parse("body.http", strings.NewReader(`### Constrained
@body = ~.*token.*
@json.user.name = alice
@json.user.id = !
POST /login

ok
`))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	serveBody(t, s, "/login", `{"user":{"name":"bob","id":7}}`)
	reasons := strings.Join(s.events[0].Match.Candidates[0].Reasons, "\n")
	for _, want := range []string{
		`Body: does not match pattern ".*token.*"`,
		`JSON user.name: expected "alice"; received ["bob"]`,
		`JSON user.id: must be absent; received ["7"]`,
	} {
		if !strings.Contains(reasons, want) {
			t.Errorf("reasons missing %q:\n%s", want, reasons)
		}
	}
	serveBody(t, s, "/login", `oops`)
	if reasons := strings.Join(s.events[1].Match.Candidates[0].Reasons, "\n"); !strings.Contains(reasons, "JSON user.name: request body is not valid JSON") {
		t.Fatalf("reasons = %q", reasons)
	}
}

func TestWarnShadowedRoutes(t *testing.T) {
	methods, err := restclient.Parse("routes.http", strings.NewReader(`### [201] Create
POST /users

one

### [400] Duplicate create
POST /users

two

### Specific after general
@json.name = alice
POST /users

three

### Different path
POST /accounts

ok

### Alice account
@json.name = alice
POST /accounts

ok

### Bob account
@json.name = bob
POST /accounts

ok
`))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_ = New(methods, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	logText := buf.String()
	for _, want := range []string{`method="Duplicate create"`, `method="Specific after general"`, `earlier route \"Create\" (routes.http:1)`} {
		if !strings.Contains(logText, want) {
			t.Errorf("log missing %s:\n%s", want, logText)
		}
	}
	if strings.Count(logText, "route can never match") != 4 {
		t.Errorf("want 4 shadow warnings (two /users routes, Alice and Bob accounts):\n%s", logText)
	}

	ordered, err := restclient.Parse("routes.http", strings.NewReader(`### Alice
@json.name = alice
POST /users

a

### Bob
@json.name = bob
POST /users

b

### Anyone
POST /users

c
`))
	if err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	_ = New(ordered, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if strings.Contains(buf.String(), "route can never match") {
		t.Fatalf("specific-first routes warned:\n%s", buf.String())
	}
}
