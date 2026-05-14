package main

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mock/restclient"
)

func TestLoadMethodsLoadsFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.http")
	if err := os.WriteFile(path, []byte(`### User
GET /users

ok
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	methods, err := loadMethods([]string{path}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("loadMethods() error = %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(methods))
	}
	if methods[0].Source != path {
		t.Fatalf("Source = %q, want %q", methods[0].Source, path)
	}
	if methods[0].Method != http.MethodGet || methods[0].Path != "/users" {
		t.Fatalf("method = %#v, want GET /users", methods[0])
	}
}

func TestLoadMethodsParsesStdinWhenNoFiles(t *testing.T) {
	methods, err := loadMethods(nil, strings.NewReader(`### User
POST /users

created
`))
	if err != nil {
		t.Fatalf("loadMethods() error = %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(methods))
	}
	if methods[0].Source != "<stdin>" {
		t.Fatalf("Source = %q, want <stdin>", methods[0].Source)
	}
	if methods[0].Method != http.MethodPost || methods[0].Path != "/users" {
		t.Fatalf("method = %#v, want POST /users", methods[0])
	}
}

func TestLoadInputUsesSingleDirectoryAsStaticRoot(t *testing.T) {
	dir := t.TempDir()
	input, err := loadInput([]string{dir}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("loadInput() error = %v", err)
	}
	if input.StaticDir != dir {
		t.Fatalf("StaticDir = %q, want %q", input.StaticDir, dir)
	}
	if len(input.Methods) != 0 {
		t.Fatalf("len(Methods) = %d, want 0", len(input.Methods))
	}
}

func TestLoadInputRejectsDirectoryMixedWithRequestFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadInput([]string{dir, path}, strings.NewReader(""))
	if err == nil {
		t.Fatal("loadInput() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cannot mix static directory") {
		t.Fatalf("error = %q, want mixed directory error", err)
	}
}

func TestListenAddress(t *testing.T) {
	tests := map[int]string{
		8080: ":8080",
		3000: ":3000",
	}

	for port, want := range tests {
		if got := listenAddress(port); got != want {
			t.Fatalf("listenAddress(%d) = %q, want %q", port, got, want)
		}
	}
}

func TestValidateMethodsAllowsParsedRequests(t *testing.T) {
	err := validateMethods([]restclient.Method{{Name: "User"}}, nil)
	if err != nil {
		t.Fatalf("validateMethods() error = %v", err)
	}
}

func TestValidateMethodsExplainsEmptyFileInput(t *testing.T) {
	err := validateMethods(nil, []string{"empty.http"})
	if err == nil {
		t.Fatal("validateMethods() error = nil, want error")
	}
	for _, want := range []string{"no mock requests found", "empty.http", "###", "HTTP request line"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want to contain %q", err, want)
		}
	}
}

func TestValidateMethodsExplainsEmptyStdinInput(t *testing.T) {
	err := validateMethods(nil, nil)
	if err == nil {
		t.Fatal("validateMethods() error = nil, want error")
	}
	for _, want := range []string{"no mock requests found", "stdin", "###", "HTTP request line"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want to contain %q", err, want)
		}
	}
}

func TestPrintMethods(t *testing.T) {
	methods := []restclient.Method{
		{
			Name:   "Create User",
			Method: http.MethodPost,
			Path:   "/users",
		},
		{
			Name:   "Get Cats",
			Method: http.MethodGet,
			Path:   "/names",
			Query:  url.Values{"type": []string{"cat"}},
		},
	}

	var output bytes.Buffer
	printMethods(&output, methods)

	got := output.String()
	for _, want := range []string{
		"Available mock methods:",
		"POST    /users",
		"Create User",
		"GET     /names?type=cat",
		"Get Cats",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("printMethods() output = %q, want to contain %q", got, want)
		}
	}
}

func TestNormalizeMountPath(t *testing.T) {
	tests := map[string]string{
		"":          "/mock",
		"mock":      "/mock",
		"/mock/":    "/mock",
		"admin":     "/admin",
		"/admin/ui": "/admin/ui",
	}

	for input, want := range tests {
		if got := normalizeMountPath(input); got != want {
			t.Fatalf("normalizeMountPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStaticFileHandlerServesDirectoryFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "about.txt"), []byte("about"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	handler := newStaticFileHandler(dir)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("index status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "home" {
		t.Fatalf("index body = %q, want home", body)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/about.txt", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("file status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "about" {
		t.Fatalf("file body = %q, want about", body)
	}
}

func TestHandlerServesStaticFilesUnderConfiguredMount(t *testing.T) {
	staticDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("dashboard"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	handler := newHandler(methods, slog.New(slog.NewTextHandler(io.Discard, nil)), "admin", os.DirFS(staticDir))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("static status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "dashboard" {
		t.Fatalf("static body = %q, want dashboard", body)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("mock status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "ok" {
		t.Fatalf("mock body = %q, want ok", body)
	}
}

func TestHandlerServesEmbeddedStaticFiles(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	staticFS, err := staticFileSystem()
	if err != nil {
		t.Fatalf("staticFileSystem() error = %v", err)
	}
	handler := newHandler(methods, slog.New(slog.NewTextHandler(io.Discard, nil)), "mock", staticFS)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/mock/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("static status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); !strings.Contains(body, "<title>Mock Server</title>") {
		t.Fatalf("static body = %q, want embedded dashboard HTML", body)
	}
}

func TestHandlerStreamsRequestEventsUnderConfiguredMount(t *testing.T) {
	staticDir := t.TempDir()
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	handler := newHandler(methods, slog.New(slog.NewTextHandler(io.Discard, nil)), "admin", os.DirFS(staticDir))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/admin/events", nil)
	events := httptest.NewRecorder()
	handler.ServeHTTP(events, request)

	line, err := bufio.NewReader(events.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("event line = %q, want data prefix", line)
	}
	for _, want := range []string{`"method":"GET"`, `"url":"/users"`, `"status":200`, `"statusText":"OK"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("event line = %q, want to contain %q", line, want)
		}
	}
}
