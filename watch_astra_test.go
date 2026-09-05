package main

import (
	"github.com/sspencer/mock/restclient"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDependencyOwnership(t *testing.T) {
	paths := resolveWatchPaths([]string{"a/one.http", "b/two.http"}, []restclient.Method{{Source: "a/one.http", Variables: map[string]string{"file": "nested/body.json"}}})
	if len(paths) != 3 || paths[2] != absPath("a/nested/body.json") {
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
