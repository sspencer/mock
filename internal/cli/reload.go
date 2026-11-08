package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

// reloadMockFiles re-reads the input's request files, rescanning its
// directories, and swaps in the new routes. On failure the old routes stay.
func reloadMockFiles(mockServer *mockhttp.Server, input inputSource, opts restclient.Options, plan *portPlan, logger *slog.Logger, out, errOut io.Writer) {
	mockServer.BeginReload()
	load := func() ([]restclient.Method, error) { return input.load(nil, opts) }
	methods, err := load()
	if err != nil {
		time.Sleep(50 * time.Millisecond)
		methods, err = load()
	}
	if err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	if err := validateMethods(methods, input.describe()); err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	if methods, err = plan.resolve(methods); err != nil {
		fmt.Fprintln(errOut, err.Error())
		mockServer.ReloadFailed(err)
		return
	}
	mockServer.SetMethods(methods)
	logger.Info("reloaded request files", "input", input.describe(), "methods", len(methods))
	printMethods(out, methods)
}

// reloader serializes reloads from every trigger: file events, POST /reload,
// SIGHUP and polling. After each reload it re-points the watcher at the
// current dependencies and records their state for polling.
type reloader struct {
	mu       sync.Mutex
	server   *mockhttp.Server
	input    inputSource
	opts     restclient.Options
	plan     *portPlan
	logger   *slog.Logger
	out, err io.Writer
	watcher  *fileWatcher
	// snapshot is the size and modification time of every dependency after
	// the last reload.
	snapshot string
}

// watchPaths returns the request files and their < path dependencies.
func (r *reloader) watchPaths() []string {
	files, err := r.input.requestFiles()
	if err != nil {
		files = r.input.Files
	}
	return resolveWatchPaths(files, r.server.Methods())
}

// reload re-reads the request files now.
func (r *reloader) reload() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reloadLocked()
}

func (r *reloader) reloadLocked() {
	reloadMockFiles(r.server, r.input, r.opts, r.plan, r.logger, r.out, r.err)
	paths := r.watchPaths()
	if r.watcher != nil {
		if err := r.watcher.Reconcile(paths, r.input.Dirs...); err != nil {
			r.logger.Error("failed to update dependency watches", "error", err)
		}
	}
	r.snapshot = snapshotFiles(paths)
}

// pollOnce reloads when any dependency changed since the last reload.
func (r *reloader) pollOnce() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if snapshotFiles(r.watchPaths()) != r.snapshot {
		r.reloadLocked()
	}
}

// poll checks the dependencies every interval until ctx is done, for
// filesystems where change events do not arrive.
func (r *reloader) poll(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pollOnce()
		}
	}
}

// snapshotFiles describes each path's size and modification time, or that
// it is missing, so polling can tell when any of them changed.
func snapshotFiles(paths []string) string {
	var b strings.Builder
	for _, path := range paths {
		b.WriteString(path)
		if info, err := os.Stat(path); err == nil {
			fmt.Fprintf(&b, "\x00%d\x00%d", info.Size(), info.ModTime().UnixNano())
		} else {
			b.WriteString("\x00missing")
		}
		b.WriteByte('\n')
	}
	return b.String()
}
