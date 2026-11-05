package cli

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

// freePort returns a port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func parseMethods(t *testing.T, input string) []restclient.Method {
	t.Helper()
	methods, err := restclient.Parse("api.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return methods
}

func closeListeners(listeners []net.Listener) {
	for _, ln := range listeners {
		_ = ln.Close()
	}
}

func TestListenMockServesEachDeclaredPort(t *testing.T) {
	users, auth := freePort(t), freePort(t)
	methods := parseMethods(t, fmt.Sprintf(`@users = http://localhost:%d
@auth = http://localhost:%d

### Users health
GET {{users}}/health

users

### Auth health
GET {{auth}}/health

auth

### Default route
GET /default

default
`, users, auth))
	cfg := config{Bind: "127.0.0.1", Port: 0, NoAdmin: true}
	listeners, plan, err := listenMock(cfg, methods)
	if err != nil {
		t.Fatalf("listenMock() error = %v", err)
	}
	defer closeListeners(listeners)
	if len(listeners) != 3 {
		t.Fatalf("got %d listeners, want default plus two declared", len(listeners))
	}
	resolved, err := plan.resolve(methods)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if resolved[2].Port != plan.defaultPort || plan.defaultPort == 0 {
		t.Fatalf("default route port = %d, want default port %d", resolved[2].Port, plan.defaultPort)
	}
	if plan.uiPort() != plan.defaultPort {
		t.Fatalf("uiPort() = %d, want default port %d", plan.uiPort(), plan.defaultPort)
	}

	handler := mockhttp.New(resolved, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, ln := range listeners {
		server := newHTTPServer(handler)
		go func() { _ = server.Serve(ln) }()
		defer server.Close()
	}
	get := func(port int, path string) (int, string) {
		t.Helper()
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err != nil {
			t.Fatalf("GET :%d%s error = %v", port, path, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}
	for _, tc := range []struct {
		port int
		path string
		code int
		body string
	}{
		{users, "/health", 200, "users"},
		{auth, "/health", 200, "auth"},
		{plan.defaultPort, "/default", 200, "default"},
		{users, "/default", 404, ""},
		{plan.defaultPort, "/health", 404, ""},
	} {
		code, body := get(tc.port, tc.path)
		if code != tc.code || (tc.body != "" && body != tc.body) {
			t.Errorf("GET :%d%s = %d %q, want %d %q", tc.port, tc.path, code, body, tc.code, tc.body)
		}
	}
}

func TestListenMockSkipsUnusedDefaultPort(t *testing.T) {
	port := freePort(t)
	methods := parseMethods(t, fmt.Sprintf("### A\nGET http://localhost:%d/a\n", port))
	listeners, plan, err := listenMock(config{Bind: "127.0.0.1", Port: 0, NoAdmin: true}, methods)
	if err != nil {
		t.Fatalf("listenMock() error = %v", err)
	}
	defer closeListeners(listeners)
	if len(listeners) != 1 || plan.defaultPort != 0 || plan.uiPort() != port {
		t.Fatalf("listeners = %d, defaultPort = %d, uiPort = %d; want only port %d", len(listeners), plan.defaultPort, plan.uiPort(), port)
	}
}

func TestListenMockMergesDefaultWithDeclaredPort(t *testing.T) {
	port := freePort(t)
	methods := parseMethods(t, fmt.Sprintf("### A\nGET http://localhost:%d/a\n\n### B\nGET /b\n", port))
	listeners, plan, err := listenMock(config{Bind: "127.0.0.1", Port: port, NoAdmin: true}, methods)
	if err != nil {
		t.Fatalf("listenMock() error = %v", err)
	}
	defer closeListeners(listeners)
	if len(listeners) != 1 || plan.defaultPort != port {
		t.Fatalf("listeners = %d, defaultPort = %d; want one listener on %d", len(listeners), plan.defaultPort, port)
	}
}

func TestListenMockReportsBusyPortAndClosesOthers(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	busyPort := busy.Addr().(*net.TCPAddr).Port
	free := freePort(t)
	methods := parseMethods(t, fmt.Sprintf("### Free\nGET http://localhost:%d/a\n\n### Busy\nGET http://localhost:%d/b\n", free, busyPort))
	_, _, err = listenMock(config{Bind: "127.0.0.1", NoAdmin: true}, methods)
	if err == nil {
		t.Fatal("listenMock() error = nil, want busy port error")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf(`api.http:4: cannot serve section "Busy" on port %d`, busyPort)) {
		t.Fatalf("error = %q, want the busy section and port", err)
	}
	// The listener bound before the failure must be released.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", free))
	if err != nil {
		t.Fatalf("port %d still bound after failure: %v", free, err)
	}
	_ = ln.Close()
}

func TestListenMockRejectsAdminPort(t *testing.T) {
	methods := parseMethods(t, "### A\nGET http://localhost:8081/a\n")
	_, _, err := listenMock(config{Bind: "127.0.0.1", AdminPort: 8081}, methods)
	if err == nil || !strings.Contains(err.Error(), "port 8081, which is the admin UI port") {
		t.Fatalf("error = %v, want admin port conflict", err)
	}
}

func TestPortPlanResolveRejectsUnservedPorts(t *testing.T) {
	plan := &portPlan{defaultPort: 8080, ports: map[int]bool{8080: true, 9090: true}}
	if _, err := plan.resolve(parseMethods(t, "### A\nGET http://localhost:9090/a\n\n### B\nGET /b\n")); err != nil {
		t.Fatalf("resolve() error = %v, want served ports accepted", err)
	}
	_, err := plan.resolve(parseMethods(t, "### New\nGET http://localhost:7070/a\n"))
	if err == nil || !strings.Contains(err.Error(), `api.http:1: section "New" uses port 7070, which mock is not listening on`) {
		t.Fatalf("error = %v, want unserved port error", err)
	}
	noDefault := &portPlan{ports: map[int]bool{9090: true}}
	_, err = noDefault.resolve(parseMethods(t, "### B\nGET /b\n"))
	if err == nil || !strings.Contains(err.Error(), "not serving the default port") {
		t.Fatalf("error = %v, want default port error", err)
	}
}

func TestReloadRejectsNewPortAndKeepsRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("### A\nGET /a\n\na\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan := &portPlan{defaultPort: 8080, ports: map[int]bool{8080: true}}
	methods, err := plan.resolve(parseMethods(t, "### A\nGET /a\n\na\n"))
	if err != nil {
		t.Fatal(err)
	}
	server := mockhttp.New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.WriteFile(path, []byte("### B\nGET http://localhost:7070/b\n\nb\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	reloadMockFiles(server, []string{path}, plan, slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, &errOut)
	if !strings.Contains(errOut.String(), "restart mock to serve new ports") {
		t.Fatalf("stderr = %q, want restart hint", errOut.String())
	}
	if got := server.Methods(); len(got) != 1 || got[0].Name != "A" {
		t.Fatalf("routes = %#v, want previous routes kept", got)
	}
}

func TestPrintMethodsGroupsByPort(t *testing.T) {
	methods := parseMethods(t, "### Auth\nGET http://localhost:9090/token\n\n### Users\nGET http://localhost:8080/users\n")
	var out bytes.Buffer
	printMethods(&out, methods)
	got := out.String()
	first := strings.Index(got, "Available mock methods on port 8080:")
	second := strings.Index(got, "Available mock methods on port 9090:")
	if first < 0 || second < first || !strings.Contains(got[first:second], "/users") || !strings.Contains(got[second:], "/token") {
		t.Fatalf("printMethods() = %q, want routes grouped by ascending port", got)
	}
}
