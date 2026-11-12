package cli

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func TestDependencyOwnership(t *testing.T) {
	paths := resolveWatchPaths([]string{"a/one.http", "b/two.http"}, []restclient.Method{{Source: "a/one.http", File: "nested/body.json"}})
	// The http file's directory is checked first, then the workspace root.
	if len(paths) != 4 || paths[2] != absPath("a/nested/body.json") || paths[3] != absPath("nested/body.json") {
		t.Fatalf("unexpected dependencies: %v", paths)
	}
}
func TestWatcherCloseWaitsForReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "route.http")
	os.WriteFile(path, []byte("old"), 0600)
	started, release := make(chan struct{}), make(chan struct{})
	w, err := watchFiles([]string{path}, func() { close(started); <-release }, nil)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("new"), 0600)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("no callback")
	}
	closed := make(chan struct{})
	go func() { w.Close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("Close returned during callback")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close stuck")
	}
}
func TestWatcherReconcilesNewDependencies(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "nested", "second")
	os.WriteFile(first, []byte("a"), 0600)
	events := make(chan struct{}, 10)
	w, err := watchFiles([]string{first}, func() { events <- struct{}{} }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Reconcile([]string{second}); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Dir(second), 0700)
	os.WriteFile(second, []byte("b"), 0600)
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("new dependency not watched")
	}
	if err := w.Reconcile([]string{second}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(second, []byte("c"), 0600)
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("nested file changes not watched")
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

func TestResolveWatchPathsIncludesDependencies(t *testing.T) {
	paths := resolveWatchPaths([]string{"examples/user.http"}, []restclient.Method{{Source: "examples/user.http", File: "users.json"}, {Source: "examples/user.http", File: "index.html"}})
	if len(paths) < 3 {
		t.Fatalf("paths = %#v, want http file plus dependencies", paths)
	}
}

func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func TestResolveWatchPathsIncludesImports(t *testing.T) {
	paths := resolveWatchPaths([]string{"api.http"}, []restclient.Method{{Source: "api.http", Imports: []string{"shared/common.http"}}})
	if len(paths) != 2 || paths[1] != absPath("shared/common.http") {
		t.Fatalf("paths = %v", paths)
	}
}
