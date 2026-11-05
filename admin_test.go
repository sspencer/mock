package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func TestHandlerClearRequiresCSRFHeader(t *testing.T) {
	staticDir := t.TempDir()
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newAdminHandler(mockhttp.New(methods, logger), os.DirFS(staticDir), uiConfig{})

	bare := httptest.NewRecorder()
	handler.ServeHTTP(bare, httptest.NewRequest(http.MethodPost, "/clear", nil))
	if bare.Code != http.StatusForbidden {
		t.Fatalf("bare clear status = %d, want %d", bare.Code, http.StatusForbidden)
	}

	ok := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/clear", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	handler.ServeHTTP(ok, req)
	if ok.Code != http.StatusNoContent {
		t.Fatalf("UI clear status = %d, want %d", ok.Code, http.StatusNoContent)
	}
}

func TestHandlerInjectsMockConfigVersion(t *testing.T) {
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
	handler := newAdminHandler(mockhttp.New(methods, logger), staticFS, uiConfig{Version: "1.2.3", MockPort: 8080, MultiPort: true})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, `id="mock-config"`) {
		t.Fatalf("body missing mock-config bootstrap")
	}
	for _, want := range []string{`"version":"1.2.3"`, `"mockPort":8080`, `"multiPort":true`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s in mock-config", want)
		}
	}
}

func TestAdminHandlerServesStaticFilesAtRoot(t *testing.T) {
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
	handler := newAdminHandler(mockhttp.New(methods, logger), os.DirFS(staticDir), uiConfig{})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("static status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "dashboard" {
		t.Fatalf("static body = %q, want dashboard", body)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("mock route on admin handler status = %d, want %d", response.Code, http.StatusNotFound)
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
	handler := newAdminHandler(mockhttp.New(methods, logger), staticFS, uiConfig{})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("static status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, want := range []string{
		"<title>Mock Server</title>",
		`id="helpButton"`,
		`id="helpDialog"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("static body missing %q", want)
		}
	}
}

func TestAdminHandlerStreamsRequestEvents(t *testing.T) {
	staticDir := t.TempDir()
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mock := mockhttp.New(methods, logger)
	handler := newAdminHandler(mock, os.DirFS(staticDir), uiConfig{})

	response := httptest.NewRecorder()
	mock.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
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

	clear := httptest.NewRecorder()
	clearReq := httptest.NewRequest(http.MethodPost, "/clear", nil)
	clearReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	handler.ServeHTTP(clear, clearReq)
	if clear.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d, want %d", clear.Code, http.StatusNoContent)
	}
	routes := httptest.NewRecorder()
	handler.ServeHTTP(routes, httptest.NewRequest(http.MethodGet, "/routes", nil))
	if routes.Code != http.StatusOK || !strings.Contains(routes.Body.String(), "/users") {
		t.Fatalf("routes status = %d body = %q", routes.Code, routes.Body.String())
	}
}
