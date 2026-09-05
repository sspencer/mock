package main

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/sspencer/mock/restclient"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const reloadDebounce = 250 * time.Millisecond

// watchFiles serializes debounced callbacks. Close waits for any active reload.
func watchFiles(paths []string, onChange func(), logger *slog.Logger) (*fileWatcher, error) {
	if onChange == nil {
		return nil, fmt.Errorf("onChange callback is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &fileWatcher{watcher: watcher, onChange: onChange, logger: logger, stop: make(chan struct{}), done: make(chan struct{}), dirs: make(map[string]struct{})}
	if err := w.Reconcile(paths); err != nil {
		watcher.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

type fileWatcher struct {
	watcher    *fsnotify.Watcher
	watched    map[string]struct{}
	dirs       map[string]struct{}
	onChange   func()
	logger     *slog.Logger
	mu         sync.Mutex
	once       sync.Once
	stop, done chan struct{}
}

// Reconcile preserves each dependency's source ownership and watches the nearest
// existing ancestor, allowing missing nested dependency directories to appear.
func (w *fileWatcher) Reconcile(paths []string) error {
	watched, dirs := make(map[string]struct{}), make(map[string]struct{})
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return fmt.Errorf("%q is a directory, not a file", path)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		watched[abs] = struct{}{}
		dir := filepath.Dir(abs)
		for {
			info, err := os.Stat(dir)
			if err == nil && info.IsDir() {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return fmt.Errorf("no watchable parent for %s", path)
			}
			dir = parent
		}
		dirs[dir] = struct{}{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var added []string
	for dir := range dirs {
		if _, ok := w.dirs[dir]; ok {
			continue
		}
		if err := w.watcher.Add(dir); err != nil {
			for _, d := range added {
				_ = w.watcher.Remove(d)
			}
			return fmt.Errorf("watch %s: %w", dir, err)
		}
		added = append(added, dir)
	}
	for dir := range w.dirs {
		if _, ok := dirs[dir]; !ok {
			_ = w.watcher.Remove(dir)
		}
	}
	w.watched, w.dirs = watched, dirs
	return nil
}

func (w *fileWatcher) Close() error {
	w.once.Do(func() { close(w.stop) })
	<-w.done
	return nil
}

func (w *fileWatcher) loop() {
	defer close(w.done)
	defer w.watcher.Close()
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var tick <-chan time.Time
	for {
		select {
		case <-w.stop:
			return
		case <-tick:
			tick = nil
			select {
			case <-w.stop:
				return
			default:
			}
			w.onChange()
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("file watcher error", "error", err)
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) && !event.Has(fsnotify.Remove) {
				continue
			}
			abs, err := filepath.Abs(event.Name)
			if err != nil {
				continue
			}
			relevant := false
			w.mu.Lock()
			for path := range w.watched {
				for candidate := path; ; candidate = filepath.Dir(candidate) {
					if candidate == abs {
						relevant = true
						break
					}
					if candidate == filepath.Dir(candidate) {
						break
					}
				}
				if relevant {
					break
				}
			}
			w.mu.Unlock()
			if relevant {
				timer.Reset(reloadDebounce)
				tick = timer.C
			}
		}
	}
}

func resolveWatchPaths(httpFiles []string, methods []restclient.Method) []string {
	seen := make(map[string]bool)
	var paths []string
	add := func(path string) {
		abs, err := filepath.Abs(path)
		if err == nil && !seen[abs] {
			seen[abs] = true
			paths = append(paths, abs)
		}
	}
	for _, file := range httpFiles {
		add(file)
	}
	for _, method := range methods {
		if raw, ok := method.Variables["file"]; ok {
			if path, err := restclient.ResolveFile(method.Source, raw); err == nil {
				add(path)
			}
		}
	}
	return paths
}
