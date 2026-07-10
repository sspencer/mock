package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// reloadDebounce coalesces rapid editor save events (Write + Create, atomic renames).
const reloadDebounce = 100 * time.Millisecond

// watchFiles watches the parent directories of the given files and invokes onChange
// when any of those files is written or recreated. The parent directory is watched
// instead of the file itself so atomic editor saves (rename) are observed.
//
// The returned closer stops the watcher. onChange may be invoked from a background
// goroutine and should be safe for concurrent use with the HTTP server.
func watchFiles(paths []string, onChange func(), logger *slog.Logger) (io.Closer, error) {
	if len(paths) == 0 {
		return io.NopCloser(nil), nil
	}
	if onChange == nil {
		return nil, fmt.Errorf("onChange callback is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	watched := make(map[string]struct{}, len(paths))
	dirs := make(map[string]struct{})
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%q is a directory, not a file", path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		abs = filepath.Clean(abs)
		watched[abs] = struct{}{}
		dirs[filepath.Dir(abs)] = struct{}{}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	for dir := range dirs {
		if err := watcher.Add(dir); err != nil {
			_ = watcher.Close()
			return nil, fmt.Errorf("watch %s: %w", dir, err)
		}
	}

	w := &fileWatcher{
		watcher:  watcher,
		watched:  watched,
		onChange: onChange,
		logger:   logger,
	}
	go w.loop()
	return w, nil
}

type fileWatcher struct {
	watcher  *fsnotify.Watcher
	watched  map[string]struct{}
	onChange func()
	logger   *slog.Logger

	mu    sync.Mutex
	timer *time.Timer
}

func (w *fileWatcher) Close() error {
	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()
	return w.watcher.Close()
}

func (w *fileWatcher) loop() {
	for {
		select {
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("file watcher error", "error", err)
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) {
				continue
			}
			abs, err := filepath.Abs(event.Name)
			if err != nil {
				continue
			}
			abs = filepath.Clean(abs)
			if _, ok := w.watched[abs]; !ok {
				continue
			}
			w.schedule()
		}
	}
}

func (w *fileWatcher) schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(reloadDebounce, w.onChange)
}
