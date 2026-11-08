package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func newTestReloader(t *testing.T, input inputSource) *reloader {
	t.Helper()
	loaded, err := loadInput(input.Files, input.Dirs, nil, restclient.Options{})
	if err != nil {
		t.Fatal(err)
	}
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	r := &reloader{server: mockhttp.New(loaded.Methods, discard), input: loaded, logger: discard, out: io.Discard, err: io.Discard}
	r.snapshot = snapshotFiles(r.watchPaths())
	return r
}

func TestPollReloadsOnlyWhenFilesChange(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.http": route("A", "/a")})
	r := newTestReloader(t, inputSource{Dirs: []string{root}})
	r.pollOnce()
	if got := len(r.server.Methods()); got != 1 {
		t.Fatalf("routes = %d, want 1", got)
	}
	writeTree(t, root, map[string]string{"b.http": route("B", "/b")})
	r.pollOnce()
	if got := len(r.server.Methods()); got != 2 {
		t.Fatalf("routes after adding b.http = %d, want 2", got)
	}

	// Rewriting a file with new content and a later mtime is noticed.
	path := filepath.Join(root, "a.http")
	writeTree(t, root, map[string]string{"a.http": route("A2", "/a") + route("A3", "/a3")})
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	r.pollOnce()
	if got := len(r.server.Methods()); got != 3 {
		t.Fatalf("routes after editing a.http = %d, want 3", got)
	}
	before := r.snapshot
	r.pollOnce()
	if r.snapshot != before {
		t.Fatal("an unchanged tree should not reload")
	}
}

func TestPollTracksBodyFiles(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"api.http":  "### Body\nGET /a\n\n< body.json\n",
		"body.json": `{"v":1}`,
	})
	r := newTestReloader(t, inputSource{Files: []string{filepath.Join(root, "api.http")}})
	if !strings.Contains(r.snapshot, "body.json") {
		t.Fatalf("snapshot should cover body files:\n%s", r.snapshot)
	}
	before := r.snapshot
	writeTree(t, root, map[string]string{"body.json": `{"v":22}`})
	r.pollOnce()
	if r.snapshot == before {
		t.Fatal("changing a body file should reload")
	}
}

func TestReloadOnSignal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal)
	reloaded := make(chan struct{})
	done := make(chan struct{})
	go func() {
		reloadOnSignal(ctx, signals, func() { reloaded <- struct{}{} }, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	signals <- syscall.SIGHUP
	<-reloaded
	cancel()
	<-done
}

func TestParseConfigPoll(t *testing.T) {
	cfg, err := parseConfig([]string{"--poll", "1s", "api.http"})
	if err != nil || cfg.Poll != time.Second {
		t.Fatalf("Poll = %v, err %v", cfg.Poll, err)
	}
	for _, bad := range []string{"10ms", "soon"} {
		if _, err := parseConfig([]string{"-i", bad, "api.http"}); ExitCode(err) != 2 {
			t.Errorf("-i %s error = %v, want usage error", bad, err)
		}
	}
}

func TestRunRejectsPollWithoutFiles(t *testing.T) {
	err := Run(context.Background(), []string{"-i", "1s", "-x"}, strings.NewReader("### A\nGET /a\n\nok\n"), io.Discard, io.Discard, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if ExitCode(err) != 2 || !strings.Contains(err.Error(), "--poll") {
		t.Fatalf("Run(-i with stdin) = %v, want usage error", err)
	}
}
