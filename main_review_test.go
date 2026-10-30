package main

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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

func TestParseConfigAdminPort(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		args      []string
		port      int
		adminPort int
	}{
		{"defaults", "", nil, 8080, 8081},
		{"p flag picks random admin", "", []string{"-p", "9000"}, 9000, 0},
		{"MOCK_PORT picks random admin", "9091", nil, 9091, 0},
		{"explicit admin with p", "", []string{"-p", "9000", "-a", "9100"}, 9000, 9100},
		{"explicit admin only", "", []string{"-a", "9100"}, 8080, 9100},
		{"admin zero", "", []string{"-a", "0"}, 8080, 0},
		{"explicit admin 8081 with p", "", []string{"-p", "9000", "-a", "8081"}, 9000, 8081},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MOCK_PORT", tt.env)
			cfg, err := parseConfig(append(tt.args, "api.http"))
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			if cfg.Port != tt.port || cfg.AdminPort != tt.adminPort {
				t.Fatalf("Port, AdminPort = %d, %d; want %d, %d", cfg.Port, cfg.AdminPort, tt.port, tt.adminPort)
			}
		})
	}
}

func TestParseConfigMockUIPort(t *testing.T) {
	tests := []struct {
		name      string
		portEnv   string
		uiEnv     string
		args      []string
		port      int
		adminPort int
	}{
		{"env sets admin", "", "9200", nil, 8080, 9200},
		{"env admin survives -p", "", "9200", []string{"-p", "9000"}, 9000, 9200},
		{"env admin survives MOCK_PORT", "9091", "9200", nil, 9091, 9200},
		{"flag overrides env", "", "9200", []string{"-a", "9300"}, 8080, 9300},
		{"env zero is random", "", "0", nil, 8080, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MOCK_PORT", tt.portEnv)
			t.Setenv("MOCK_UI_PORT", tt.uiEnv)
			cfg, err := parseConfig(append(tt.args, "api.http"))
			if err != nil {
				t.Fatalf("parseConfig() error = %v", err)
			}
			if cfg.Port != tt.port || cfg.AdminPort != tt.adminPort {
				t.Fatalf("Port, AdminPort = %d, %d; want %d, %d", cfg.Port, cfg.AdminPort, tt.port, tt.adminPort)
			}
		})
	}
}

func TestParseConfigRejectsPortCollisionsAndBadMockUIPort(t *testing.T) {
	tests := []struct {
		name    string
		portEnv string
		uiEnv   string
		args    []string
	}{
		{"env admin equals default mock port", "", "8080", nil},
		{"env admin equals -p", "", "9000", []string{"-p", "9000"}},
		{"env admin equals MOCK_PORT", "9000", "9000", nil},
		{"-a equals MOCK_PORT", "9000", "", []string{"-a", "9000"}},
		{"non-numeric", "", "nope", nil},
		{"out of range", "", "65536", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MOCK_PORT", tt.portEnv)
			t.Setenv("MOCK_UI_PORT", tt.uiEnv)
			if _, err := parseConfig(append(tt.args, "api.http")); err == nil {
				t.Fatal("parseConfig() error = nil, want error")
			}
		})
	}
}

func TestParseConfigNoAdmin(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	t.Setenv("MOCK_UI_PORT", "")
	cfg, err := parseConfig([]string{"-x", "api.http"})
	if err != nil {
		t.Fatalf("parseConfig() error = %v", err)
	}
	if !cfg.NoAdmin {
		t.Fatal("NoAdmin = false, want true")
	}
	// The admin port is unused, so a matching MOCK_UI_PORT is not a collision.
	t.Setenv("MOCK_UI_PORT", "8080")
	if _, err := parseConfig([]string{"-x", "api.http"}); err != nil {
		t.Fatalf("parseConfig(-x) with colliding MOCK_UI_PORT error = %v", err)
	}
	if _, err := parseConfig([]string{"-x", "-a", "9100", "api.http"}); err == nil {
		t.Fatal("parseConfig() accepted -x with -a")
	}
}

func TestRunNoAdminOmitsAdminUI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	for _, file := range []string{cert, key} {
		if err := os.WriteFile(file, []byte("not valid"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}
	// The invalid certificate makes ServeTLS fail right after the banner prints.
	var out, errOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := run([]string{"-x", "-p", "0", "-b", "0.0.0.0", "-cert", cert, "-key", key, path}, strings.NewReader(""), &out, &errOut, logger)
	if err == nil {
		t.Fatal("run() error = nil, want TLS failure")
	}
	if !strings.Contains(out.String(), "starting mock HTTP server on") {
		t.Fatalf("stdout = %q, want mock banner", out.String())
	}
	if strings.Contains(out.String(), "admin UI") || strings.Contains(errOut.String(), "admin UI") {
		t.Fatalf("stdout = %q stderr = %q, want no admin UI mention", out.String(), errOut.String())
	}
}

func TestParseConfigRejectsMountFlag(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	if _, err := parseConfig([]string{"-l", "inspect", "api.http"}); err == nil {
		t.Fatal("parseConfig() accepted removed -l flag")
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

	// The invalid certificate makes ServeTLS fail right after the banner prints.
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := run([]string{
		"-p", "0",
		"-a", "0",
		"-b", "127.0.0.1",
		"-cert", cert,
		"-key", key,
		path,
	}, strings.NewReader(""), &out, io.Discard, logger)
	if err == nil {
		t.Fatal("run() error = nil, want TLS failure")
	}
	got := out.String()
	if !regexp.MustCompile(`admin UI at https://127\.0\.0\.1:\d+/\n`).MatchString(got) {
		t.Fatalf("stdout = %q, want https admin URL on the actual admin address", got)
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

	// The invalid certificate makes ServeTLS fail right after the banner prints.
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	for _, file := range []string{cert, key} {
		if err := os.WriteFile(file, []byte("not valid"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	var out, errOut bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := run([]string{"-p", "0", "-a", "0", "-b", "0.0.0.0", "-cert", cert, "-key", key, path}, strings.NewReader(""), &out, &errOut, logger)
	if err == nil {
		t.Fatal("run() error = nil, want TLS failure")
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
	handler := newAdminHandler(mockhttp.New(methods, logger), staticFS, uiConfig{Version: "1.2.3", MockScheme: "http", MockPort: 8080})

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, `id="mock-config"`) {
		t.Fatalf("body missing mock-config bootstrap")
	}
	for _, want := range []string{`"version":"1.2.3"`, `"mockScheme":"http"`, `"mockPort":8080`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %s in mock-config", want)
		}
	}
}
