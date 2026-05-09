package mockhttp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mock/restclient"
)

func TestServerServesPathAndQueryValues(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Return Cat
GET /names/:id?type=cat
Content-Type: application/json

{"id":"{{$id}}","type":"{{$type}}","name":"{{$name}}"}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/names/42?type=cat", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	body := response.Body.String()
	if !strings.Contains(body, `"id":"42"`) || !strings.Contains(body, `"type":"cat"`) {
		t.Fatalf("body = %q, want substituted path and query values", body)
	}
}

func TestServerUsesStatusVariableAndSuppressesNoContentBody(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Delete User
# $status=204
DELETE /users/:id

ignored
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodDelete, "/users/7", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if body := response.Body.String(); body != "" {
		t.Fatalf("body = %q, want empty body for 204", body)
	}
}

func TestServerAlternatesDuplicateMethodAndURL(t *testing.T) {
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
	wants := []struct {
		status int
		body   string
	}{
		{status: http.StatusCreated, body: `"success":true`},
		{status: http.StatusBadRequest, body: `"success":false`},
		{status: http.StatusCreated, body: `"success":true`},
	}

	for i, want := range wants {
		request := httptest.NewRequest(http.MethodPost, "/users", nil)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, request)

		if response.Code != want.status {
			t.Fatalf("request %d status = %d, want %d", i+1, response.Code, want.status)
		}
		if body := response.Body.String(); !strings.Contains(body, want.body) {
			t.Fatalf("request %d body = %q, want to contain %q", i+1, body, want.body)
		}
	}
}

func TestServerRequestEventIncludesRequestAndResponseBodies(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Create User
# $status=201
POST /users
Content-Type: application/json

{
    "success": true,
    "id": 12542
}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(`{"user":"penny","email":5}`))
	request.Host = "localhost:8080"
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "curl/8.7.1")
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}
	if body := response.Body.String(); !strings.Contains(body, `"success": true`) {
		t.Fatalf("body = %q, want response body to reach client", body)
	}
	if len(server.events) != 1 {
		t.Fatalf("events length = %d, want 1", len(server.events))
	}

	event := server.events[0]
	if _, err := time.Parse("15:04:05", event.Request.Time); err != nil {
		t.Fatalf("request time = %q, want HH:MM:SS", event.Request.Time)
	}
	for _, want := range []string{
		"POST /users HTTP/1.1",
		"Host: localhost:8080",
		"Accept: */*",
		"Content-Length: 26",
		"Content-Type: application/x-www-form-urlencoded",
		"User-Agent: curl/8.7.1",
		`{"user":"penny","email":5}`,
	} {
		if !strings.Contains(event.Request.Details, want) {
			t.Fatalf("request details = %q, want to contain %q", event.Request.Details, want)
		}
	}
	for _, want := range []string{
		"HTTP/1.1 201 Created",
		"Content-Type: application/json",
		"Content-Length: 40",
		`"success": true`,
		`"id": 12542`,
	} {
		if !strings.Contains(event.Response.Details, want) {
			t.Fatalf("response details = %q, want to contain %q", event.Response.Details, want)
		}
	}
}

func TestServerTruncatesLargeRequestBodiesInEvents(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Upload
POST /upload
Content-Type: text/plain

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	body := strings.Repeat("a", maxLoggedBodyBytes+10)

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(body))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if len(server.events) != 1 {
		t.Fatalf("events length = %d, want 1", len(server.events))
	}
	details := server.events[0].Request.Details
	if !strings.Contains(details, truncatedBodyMarker) {
		t.Fatalf("request details = %q, want truncation marker", details)
	}
	if strings.Contains(details, body) {
		t.Fatalf("request details contain the full request body")
	}
}

func TestServerTruncatesLargeResponseBodiesInEventsOnly(t *testing.T) {
	body := strings.Repeat("b", maxLoggedBodyBytes+10)
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Download
GET /download
Content-Type: text/plain

`+body+`
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/download", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != body {
		t.Fatalf("client body length = %d, want full body length %d", len(got), len(body))
	}
	if len(server.events) != 1 {
		t.Fatalf("events length = %d, want 1", len(server.events))
	}
	details := server.events[0].Response.Details
	if !strings.Contains(details, truncatedBodyMarker) {
		t.Fatalf("response details = %q, want truncation marker", details)
	}
	if !strings.Contains(details, "Content-Length: "+strconv.Itoa(len(body))) {
		t.Fatalf("response details = %q, want full content length", details)
	}
	if strings.Contains(details, body) {
		t.Fatalf("response details contain the full response body")
	}
}

func TestServerDelaysResponse(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Slow Response
# $delay=20ms
GET /slow
Content-Type: text/plain

done
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/slow", nil)
	response := httptest.NewRecorder()

	start := time.Now()
	server.ServeHTTP(response, request)
	elapsed := time.Since(start)

	if elapsed < 20*time.Millisecond {
		t.Fatalf("elapsed = %s, want at least 20ms", elapsed)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "done" {
		t.Fatalf("body = %q, want done", body)
	}
}

func TestServerStopsDelayWhenRequestContextIsCanceled(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Slow Response
# $delay=1s
GET /slow
Content-Type: text/plain

done
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/slow", nil)
	response := httptest.NewRecorder()

	start := time.Now()
	server.ServeHTTP(response, request)
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Fatalf("elapsed = %s, want canceled delay to return quickly", elapsed)
	}
	if body := response.Body.String(); body != "" {
		t.Fatalf("body = %q, want no response body after cancellation", body)
	}
	if len(server.events) != 0 {
		t.Fatalf("events length = %d, want no logged response after cancellation", len(server.events))
	}
}

func TestServerServesFileRelativeToRestClientFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "user.http")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>Hello</h1>"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	methods, err := restclient.Parse(source, strings.NewReader(`### Home Page
# $file=index.html
GET /
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, target := range []string{"/", "/index.html"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()

		server.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", target, response.Code, http.StatusOK)
		}
		if body := response.Body.String(); body != "<h1>Hello</h1>" {
			t.Fatalf("%s body = %q, want file contents", target, body)
		}
		if contentType := response.Header().Get("Content-Type"); contentType != "text/html; charset=utf-8" {
			t.Fatalf("%s content type = %q, want text/html; charset=utf-8", target, contentType)
		}
	}
}

func TestServerRejectsFilePathTraversal(t *testing.T) {
	dir := t.TempDir()
	restClientDir := filepath.Join(dir, "requests")
	if err := os.Mkdir(restClientDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.html"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	methods, err := restclient.Parse(filepath.Join(restClientDir, "user.http"), strings.NewReader(`### Unsafe File
# $file=../secret.html
GET /unsafe
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequest(http.MethodGet, "/unsafe", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "" {
		t.Fatalf("body = %q, want empty body for unsafe file path", body)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "" {
		t.Fatalf("content type = %q, want none for unsafe file path", contentType)
	}
}
