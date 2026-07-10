package main

import (
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
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
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
	input, err := loadInput([]string{dir}, strings.NewReader(""), "")
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

	_, err := loadInput([]string{dir, path}, strings.NewReader(""), "")
	if err == nil {
		t.Fatal("loadInput() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cannot mix static directory") {
		t.Fatalf("error = %q, want mixed directory error", err)
	}
}

func TestListenAddress(t *testing.T) {
	tests := []struct {
		bind string
		port int
		want string
	}{
		{bind: "", port: 8080, want: ":8080"},
		{bind: "", port: 3000, want: ":3000"},
		{bind: "127.0.0.1", port: 9090, want: "127.0.0.1:9090"},
	}

	for _, tt := range tests {
		if got := listenAddress(tt.bind, tt.port); got != tt.want {
			t.Fatalf("listenAddress(%q, %d) = %q, want %q", tt.bind, tt.port, got, tt.want)
		}
	}
}

func TestParseConfigAndVersion(t *testing.T) {
	cfg, err := parseConfig([]string{"-p", "9090", "-b", "127.0.0.1", "-cors", "*", "api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Port != 9090 || cfg.Bind != "127.0.0.1" || cfg.CORS != "*" || len(cfg.Args) != 1 {
		t.Fatalf("config = %#v", cfg)
	}

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run([]string{"-version"}, strings.NewReader(""), &out, logger); err != nil {
		t.Fatalf("run(-version) error = %v", err)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Fatalf("version output = %q, want dev", out.String())
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
	nestedDir := filepath.Join(dir, "docs")
	if err := os.Mkdir(nestedDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "about.txt"), []byte("about"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "guide.txt"), []byte("guide"), 0o600); err != nil {
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

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/docs/guide.txt", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("nested file status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "guide" {
		t.Fatalf("nested file body = %q, want guide", body)
	}
}

func TestStaticFileHandlerRejectsMissingAndTraversalPaths(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "public")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	handler := newStaticFileHandler(dir)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing.txt", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d", response.Code, http.StatusNotFound)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/../secret.txt", nil))

	if response.Code == http.StatusOK || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("traversal response status = %d body = %q, want secret not served", response.Code, response.Body.String())
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newHandler(mockhttp.New(methods, logger), "admin", os.DirFS(staticDir))

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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newHandler(mockhttp.New(methods, logger), "mock", staticFS)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/mock/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("static status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); !strings.Contains(body, "<title>Mock Server</title>") {
		t.Fatalf("static body = %q, want embedded dashboard HTML", body)
	}
}

func TestReloadMockFilesUpdatesRoutesAndPrintsSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte(`### User
GET /users

ok
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := mockhttp.New(nil, logger)

	var output bytes.Buffer
	reloadMockFiles(server, []string{path}, "", logger, &output)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}

	got := output.String()
	for _, want := range []string{"Available mock methods:", "GET", "/users", "User"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reload output = %q, want to contain %q", got, want)
		}
	}
}

func TestReloadMockFilesKeepsPreviousRoutesOnParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("not a valid request file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	initial, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := mockhttp.New(initial, logger)

	var output bytes.Buffer
	reloadMockFiles(server, []string{path}, "", logger, &output)
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty on failed reload", output.String())
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("status = %d body = %q, want previous route preserved", response.Code, response.Body.String())
	}
}

func TestWatchFilesInvokesCallbackOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("initial\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	changed := make(chan struct{}, 1)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	closer, err := watchFiles([]string{path}, func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}, logger)
	if err != nil {
		t.Fatalf("watchFiles() error = %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	// Allow the watcher to attach before writing.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("updated\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for watchFiles callback")
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newHandler(mockhttp.New(methods, logger), "admin", os.DirFS(staticDir))

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/admin/events", nil)
	events := httptest.NewRecorder()
	handler.ServeHTTP(events, request)

	body := events.Body.String()
	if !strings.Contains(body, "data: ") {
		t.Fatalf("events body = %q, want data prefix", body)
	}
	for _, want := range []string{`"method":"GET"`, `"url":"/users"`, `"status":200`, `"statusText":"OK"`, `"id":`} {
		if !strings.Contains(body, want) {
			t.Fatalf("events body = %q, want to contain %q", body, want)
		}
	}

	// Admin routes are mounted under the UI path.
	clear := httptest.NewRecorder()
	handler.ServeHTTP(clear, httptest.NewRequest(http.MethodPost, "/admin/clear", nil))
	if clear.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d, want %d", clear.Code, http.StatusNoContent)
	}
	routes := httptest.NewRecorder()
	handler.ServeHTTP(routes, httptest.NewRequest(http.MethodGet, "/admin/routes", nil))
	if routes.Code != http.StatusOK || !strings.Contains(routes.Body.String(), "/users") {
		t.Fatalf("routes status = %d body = %q", routes.Code, routes.Body.String())
	}
}

func TestWithCORS(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := withCORS(inner, "*")

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodOptions, "/users", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("options status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS origin = %q, want *", got)
	}
}

func TestResolveWatchPathsIncludesDependencies(t *testing.T) {
	paths := resolveWatchPaths([]string{"examples/user.http"}, []string{"users.json", "index.html"})
	if len(paths) < 3 {
		t.Fatalf("paths = %#v, want http file plus dependencies", paths)
	}
}

func TestEndToEndReloadServesNewRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### A\nGET /a\n\na\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	methods, err := restclient.Load([]string{path})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := mockhttp.New(methods, logger)

	changed := make(chan struct{}, 1)
	closer, err := watchFiles([]string{path}, func() {
		reloadMockFiles(server, []string{path}, "", logger, io.Discard)
		select {
		case changed <- struct{}{}:
		default:
		}
	}, logger)
	if err != nil {
		t.Fatalf("watchFiles() error = %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("### B\nGET /b\n\nb\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	select {
	case <-changed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reload")
	}

	// Allow reload callback to finish SetMethods.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/b", nil))
		if response.Code == http.StatusOK && response.Body.String() == "b" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("reloaded route /b not served")
}
