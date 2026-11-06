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

func newTestServer(t *testing.T, input string) *Server {
	t.Helper()
	methods, err := restclient.Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func serveRequest(t *testing.T, s *Server, method, target, contentType, body string, headers ...string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestWildcardPaths(t *testing.T) {
	s := newTestServer(t, `### File
GET /files/*path

file {{$path}}

### Literal star
GET /stars/%2A

literal star

### API fallback
GET /api/*

api fallback

### Everything
GET /*

everything
`)
	tests := []struct{ target, want string }{
		{"/files/a.txt", "file a.txt"},
		{"/files/docs/2026/report.pdf", "file docs/2026/report.pdf"},
		{"/files/a%20b/c%2Fd", "file a b/c/d"},
		{"/files", "file"}, // the wildcard may match nothing
		{"/files/", "file"},
		{"/stars/%2A", "literal star"},
		{"/stars/x", "everything"},
		{"/api/users/7", "api fallback"},
		{"/api", "api fallback"},
		{"/", "everything"},
		{"/anything/else", "everything"},
	}
	for _, tt := range tests {
		if code, body := serveRequest(t, s, "GET", tt.target, "", ""); code != 200 || body != tt.want {
			t.Errorf("GET %s = %d %q, want %q", tt.target, code, body, tt.want)
		}
	}
}

func TestFormMatchers(t *testing.T) {
	s := newTestServer(t, `### Client credentials
@form.grant_type = client_credentials
@form.client_id = ~(?P<client>[a-z]+)-[0-9]+
POST /token
Content-Type: application/json

{"access_token":"{{$client}}-token","scope":"{{$form.scope}}"}

### Password grant
@form.grant_type = password
@form.username = *
POST /token
Content-Type: application/json

{"user":"{{$form.username}}"}

### [400] No grant type
@form.grant_type = !
POST /token

missing grant_type

### [400] Unsupported grant
POST /token

unsupported
`)
	form := "application/x-www-form-urlencoded"
	tests := []struct{ body, want string }{
		{"grant_type=client_credentials&client_id=acme-42&scope=read+write", `{"access_token":"acme-token","scope":"read write"}`},
		{"client_id=acme-42&grant_type=client_credentials", `{"access_token":"acme-token","scope":"{{$form.scope}}"}`}, // order-independent; missing field stays literal
		{"grant_type=client_credentials&client_id=ACME-42", "unsupported"},
		{"grant_type=password&username=sam%40example.test", `{"user":"sam@example.test"}`},
		{"grant_type=password&grant_type=client_credentials&username=x", `{"user":"x"}`}, // any value may match
		{"", "missing grant_type"},
		{"scope=read", "missing grant_type"},
		{"grant_type=%zz", "unsupported"}, // invalid form data matches no @form matcher, not even !
	}
	for _, tt := range tests {
		if _, body := serveRequest(t, s, "POST", "/token", form, tt.body); body != tt.want {
			t.Errorf("POST %q = %q, want %q", tt.body, body, tt.want)
		}
	}
}

func TestBodyPlaceholdersAndCaptures(t *testing.T) {
	s := newTestServer(t, `### Create user
@json.email = ~(?P<user>[^@]+)@(?P<domain>.+)
POST /users
Content-Type: application/json
X-User: {{$json.name}}

{"name":"{{$json.name}}","user":"{{$user}}","domain":"{{$domain}}","profile":{{$json.profile}},"age":{{$json.age}},"first":"{{$json.tags.0}}","missing":"{{$json.nope}}"}

### Header capture
@header.Authorization = ~Bearer (?P<token>.+)
GET /me

token={{$token}}

### Path param beats capture
@path.id = ~(?P<id>[0-9]+)
@query.id = ~(?P<id>.+)
GET /items/:id

item {{$id}}

### Body capture
@body = ~.*order=(?P<order>[0-9]+).*
POST /orders
Content-Type: text/plain

order {{$order}}
`)
	_, body := serveRequest(t, s, "POST", "/users", "application/json",
		`{"name":"Ann \"A\"","email":"ann@example.test","profile":{"city":"Oslo"},"age":42,"tags":["x","y"]}`)
	want := `{"name":"Ann \"A\"","user":"ann","domain":"example.test","profile":{"city":"Oslo"},"age":42,"first":"x","missing":"{{$json.nope}}"}`
	if body != want {
		t.Errorf("users body =\n%s\nwant\n%s", body, want)
	}
	req := httptest.NewRequest("POST", "/users", strings.NewReader(`{"name":"Bo","email":"bo@x"}`))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-User"); got != "Bo" {
		t.Errorf("X-User = %q, want Bo", got)
	}
	if _, body := serveRequest(t, s, "GET", "/me", "", "", "Authorization", "Bearer abc.def"); body != "token=abc.def" {
		t.Errorf("/me = %q", body)
	}
	if _, body := serveRequest(t, s, "GET", "/items/7?id=other", "", ""); body != "item 7" {
		t.Errorf("/items = %q, want the path parameter", body)
	}
	if _, body := serveRequest(t, s, "POST", "/orders", "text/plain", "a=1\norder=99\nz=2"); body != "order 99" {
		t.Errorf("/orders = %q", body)
	}
}

func TestBodyPlaceholdersAreNotUnknown(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Echo
@header.X-Id = ~(?P<xid>.+)
POST /echo/*rest

{{$json.a.b}} {{$form.c}} {{$xid}} {{$rest}} {{$nope}}
`))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_ = New(methods, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	logText := buf.String()
	if strings.Count(logText, "unresolved placeholder") != 1 || !strings.Contains(logText, "placeholder=nope") {
		t.Fatalf("want exactly one warning, for $nope:\n%s", logText)
	}
}

func TestFormMismatchReasons(t *testing.T) {
	s := newTestServer(t, "### Token\n@form.grant_type = client_credentials\nPOST /token\n\nok\n")
	serveRequest(t, s, "POST", "/token", "", "grant_type=password")
	serveRequest(t, s, "POST", "/token", "", "grant_type=%zz")
	first := strings.Join(s.events[0].Match.Candidates[0].Reasons, "\n")
	second := strings.Join(s.events[1].Match.Candidates[0].Reasons, "\n")
	if !strings.Contains(first, `Form grant_type: expected "client_credentials"; received ["password"]`) {
		t.Errorf("reasons = %q", first)
	}
	if !strings.Contains(second, "Form grant_type: request body is not valid URL-encoded form data") {
		t.Errorf("reasons = %q", second)
	}
}
