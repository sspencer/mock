package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

func TestShutdownHTTPServerHonorsShortGrace(t *testing.T) {
	// A handler that never returns until the connection is closed would stall a
	// long Shutdown; with a short grace + Close, exit stays bounded.
	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	server := &http.Server{Handler: mux}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ln) }()

	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}

	begin := time.Now()
	if err := shutdownHTTPServer(server, 200*time.Millisecond); err != nil {
		t.Fatalf("shutdownHTTPServer() error = %v", err)
	}
	elapsed := time.Since(begin)
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("shutdown took %s, want roughly the short grace period", elapsed)
	}

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after shutdown")
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
