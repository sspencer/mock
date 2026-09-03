package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func TestListenScheme(t *testing.T) {
	if got := listenScheme("", ""); got != "http" {
		t.Fatalf("listenScheme empty = %q, want http", got)
	}
	if got := listenScheme("cert.pem", ""); got != "http" {
		t.Fatalf("listenScheme cert without key = %q, want http", got)
	}
	if got := listenScheme("cert.pem", "key.pem"); got != "https" {
		t.Fatalf("listenScheme(cert, key) = %q, want https", got)
	}
}

func TestBindsAllInterfaces(t *testing.T) {
	tests := map[string]bool{
		"":          true,
		"0.0.0.0":   true,
		"::":        true,
		"[::]":      true,
		"*":         true,
		"127.0.0.1": false,
		"localhost": false,
	}
	for bind, want := range tests {
		if got := bindsAllInterfaces(bind); got != want {
			t.Fatalf("bindsAllInterfaces(%q) = %v, want %v", bind, got, want)
		}
	}
}

func TestParseConfigDefaultBind(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	cfg, err := parseConfig([]string{"api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if cfg.Bind != "127.0.0.1" {
		t.Fatalf("Bind = %q, want 127.0.0.1", cfg.Bind)
	}
}

func TestRunPrintsHTTPSAdminURLWhenTLSFlagsSet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cert, []byte("not a cert"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(key, []byte("not a key"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() { _ = ln.Close() })

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err = run([]string{
		"-p", strconv.Itoa(port),
		"-b", "127.0.0.1",
		"-cert", cert,
		"-key", key,
		path,
	}, strings.NewReader(""), &out, io.Discard, logger)
	if err == nil {
		t.Fatal("run() error = nil, want listen failure")
	}
	got := out.String()
	want := fmt.Sprintf("admin UI at https://127.0.0.1:%d/mock/", port)
	if !strings.Contains(got, want) {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if strings.Contains(got, "admin UI at http://") {
		t.Fatalf("stdout = %q, TLS banner should not print http:// admin URL", got)
	}
}

func TestRunWarnsWhenBoundToAllInterfaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	t.Cleanup(func() { _ = ln.Close() })

	var out, errOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err = run([]string{"-p", strconv.Itoa(port), "-b", "0.0.0.0", path}, strings.NewReader(""), &out, &errOut, logger)
	if err == nil {
		t.Fatal("run() error = nil, want listen failure")
	}
	warning := errOut.String()
	for _, want := range []string{"warning: bound to all interfaces", "unauthenticated", "Authorization"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("stderr = %q, want to contain %q", warning, want)
		}
	}
	if !strings.Contains(out.String(), "starting mock HTTP server on 0.0.0.0:"+strconv.Itoa(port)) {
		t.Fatalf("stdout = %q, want actual 0.0.0.0 listen address", out.String())
	}
}

func TestWithCORSPreflightAndMockOPTIONS(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Probe
OPTIONS /users
Content-Type: text/plain

mock-options
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := withCORS(mockhttp.New(methods, logger), "*")

	preflight := httptest.NewRequest(http.MethodOptions, "/users", nil)
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflight.Header.Set("Access-Control-Request-Headers", "X-Custom, Authorization")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, preflight)
	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("CORS origin = %q, want *", got)
	}
	if got := response.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "X-Custom, Authorization" {
		t.Fatalf("Allow-Headers = %q, want reflected custom headers", got)
	}

	mockOpts := httptest.NewRecorder()
	handler.ServeHTTP(mockOpts, httptest.NewRequest(http.MethodOptions, "/users", nil))
	if mockOpts.Code != http.StatusOK {
		t.Fatalf("non-preflight OPTIONS status = %d, want %d", mockOpts.Code, http.StatusOK)
	}
	if body := mockOpts.Body.String(); body != "mock-options" {
		t.Fatalf("non-preflight OPTIONS body = %q, want mock-options", body)
	}
}

func TestHandlerClearRequiresCSRFHeader(t *testing.T) {
	staticDir := t.TempDir()
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok\n`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newHandler(mockhttp.New(methods, logger), "admin", os.DirFS(staticDir))

	bare := httptest.NewRecorder()
	handler.ServeHTTP(bare, httptest.NewRequest(http.MethodPost, "/admin/clear", nil))
	if bare.Code != http.StatusForbidden {
		t.Fatalf("bare clear status = %d, want %d", bare.Code, http.StatusForbidden)
	}

	ok := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/clear", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	handler.ServeHTTP(ok, req)
	if ok.Code != http.StatusNoContent {
		t.Fatalf("UI clear status = %d, want %d", ok.Code, http.StatusNoContent)
	}
}

func TestHandlerInjectsMockConfigVersion(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok\n`))
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
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, `id="mock-config"`) {
		t.Fatalf("body missing mock-config bootstrap")
	}
	if !strings.Contains(body, `"version"`) {
		t.Fatalf("body missing version in mock-config")
	}
}
