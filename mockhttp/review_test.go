package mockhttp

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func TestServerRotationIgnoresUnusedQueryParams(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Create User
# $status=201
POST /users
Content-Type: application/json

{"success":true}

### Create User Failure
# $status=400
POST /users
Content-Type: application/json

{"success":false}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	targets := []string{"/users", "/users?extra=1", "/users?foo=bar"}
	wants := []struct {
		status int
		body   string
	}{
		{status: http.StatusCreated, body: `"success":true`},
		{status: http.StatusBadRequest, body: `"success":false`},
		{status: http.StatusCreated, body: `"success":true`},
	}

	for i, want := range wants {
		request := httptest.NewRequest(http.MethodPost, targets[i], nil)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != want.status {
			t.Fatalf("request %d %s status = %d, want %d", i+1, targets[i], response.Code, want.status)
		}
		if body := response.Body.String(); !strings.Contains(body, want.body) {
			t.Fatalf("request %d body = %q, want to contain %q", i+1, body, want.body)
		}
	}
}

func TestWarnIncomingHeadersUsedAsResponse(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Auth as response
GET /oops
Authorization: Bearer secret
Content-Type: application/json
Cookie: session=1

{"ok":true}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	_ = New(methods, logger)
	logText := buf.String()
	if !strings.Contains(logText, "header after the request line is a response header") {
		t.Fatalf("log = %q, want incoming-header-as-response warning", logText)
	}
	if !strings.Contains(logText, "Authorization") || !strings.Contains(logText, "Cookie") {
		t.Fatalf("log = %q, want Authorization and Cookie warnings", logText)
	}
	if strings.Contains(logText, "Content-Type") {
		t.Fatalf("log = %q, Content-Type must not warn", logText)
	}

	matched, err := restclient.Parse("test.http", strings.NewReader(`### Secured
# $header.Authorization=Bearer secret
GET /secure
Authorization: Bearer secret
Content-Type: text/plain

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	buf.Reset()
	_ = New(matched, logger)
	logText = buf.String()
	if strings.Contains(logText, "Authorization") {
		t.Fatalf("log = %q, $header.Authorization should suppress the warning", logText)
	}
}

func TestRequestEventIncludesStartedAt(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	if len(server.events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(server.events))
	}
	if _, err := time.Parse(time.RFC3339Nano, server.events[0].Request.StartedAt); err != nil {
		t.Fatalf("startedAt = %q, want RFC3339", server.events[0].Request.StartedAt)
	}
}
