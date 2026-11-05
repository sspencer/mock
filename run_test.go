package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func TestRunPrintsParseErrorsDirectly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.http")
	if err := os.WriteFile(path, []byte("### Bad\nDOPLGET /x\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := run(context.Background(), []string{path}, strings.NewReader(""), io.Discard, io.Discard, logger)
	if err == nil {
		t.Fatal("run() error = nil, want parse error")
	}
	msg := err.Error()
	if strings.Contains(msg, "level=ERROR") || strings.Contains(msg, "msg=") {
		t.Fatalf("error uses slog format: %q", msg)
	}
	for _, want := range []string{path + ":2:", "DOPLGET", "unrecognized HTTP method"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want to contain %q", msg, want)
		}
	}
}

func TestRunPrintsStartupBannersBeforeMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out bytes.Buffer
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	if err := run(cancelledContext(), []string{"-p", "0", "-a", "0", "-b", "127.0.0.1", path}, strings.NewReader(""), &out, io.Discard, logger); err != nil {
		t.Fatalf("run() error = %v", err)
	}

	got := out.String()
	logged := logs.String()
	if !regexp.MustCompile(`admin UI at http://127\.0\.0\.1:\d+/\n`).MatchString(got) {
		t.Fatalf("stdout = %q, want http admin URL on the actual admin address", got)
	}
	if strings.Contains(got, "level=INFO") || strings.Contains(got, "msg=") {
		t.Fatalf("stdout uses slog format: %q", got)
	}
	for _, msg := range []string{"starting mock HTTP server", "watching request files"} {
		if strings.Contains(logged, msg) {
			t.Fatalf("logs = %q, want %q on stdout not slog", logged, msg)
		}
	}

	start := strings.Index(got, "starting mock HTTP server")
	watch := strings.Index(got, "watching request files")
	methods := strings.Index(got, "Available mock methods:")
	if start < 0 || watch < 0 || methods < 0 {
		t.Fatalf("stdout = %q, want starting, watching, and method list", got)
	}
	if start > watch || watch > methods {
		t.Fatalf("stdout order = %q, want starting, then watching, then methods", got)
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
		reloadMockFiles(server, []string{path}, nil, logger, io.Discard, io.Discard)
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

// cancelledContext makes run shut down right after binding and printing its
// startup banners.
func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestRunNoAdminOmitsAdminUI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	var out, errOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(cancelledContext(), []string{"-x", "-p", "0", "-b", "0.0.0.0", path}, strings.NewReader(""), &out, &errOut, logger); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(out.String(), "starting mock HTTP server on") {
		t.Fatalf("stdout = %q, want mock banner", out.String())
	}
	if strings.Contains(out.String(), "admin UI") || strings.Contains(errOut.String(), "admin UI") {
		t.Fatalf("stdout = %q stderr = %q, want no admin UI mention", out.String(), errOut.String())
	}
}

func TestRunWarnsWhenBoundToAllInterfaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var out, errOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := run(cancelledContext(), []string{"-p", "0", "-a", "0", "-b", "0.0.0.0", path}, strings.NewReader(""), &out, &errOut, logger); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	warning := errOut.String()
	for _, want := range []string{"warning: bound to all interfaces", "unauthenticated", "Authorization"} {
		if !strings.Contains(warning, want) {
			t.Fatalf("stderr = %q, want to contain %q", warning, want)
		}
	}
	if !regexp.MustCompile(`starting mock HTTP server on 0\.0\.0\.0:[1-9]\d*\n`).MatchString(out.String()) {
		t.Fatalf("stdout = %q, want actual 0.0.0.0 listen address", out.String())
	}
}
