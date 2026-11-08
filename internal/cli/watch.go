package cli

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sspencer/mock/restclient"
)

const reloadDebounce = 250 * time.Millisecond

// watchFiles serializes debounced callbacks. Close waits for any active reload.
// Trees are directories watched recursively for .http files appearing,
// changing or disappearing.
func watchFiles(paths []string, onChange func(), logger *slog.Logger, trees ...string) (*fileWatcher, error) {
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
	if err := w.Reconcile(paths, trees...); err != nil {
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
	trees      []string
	onChange   func()
	logger     *slog.Logger
	mu         sync.Mutex
	once       sync.Once
	stop, done chan struct{}
}

// Reconcile preserves each dependency's source ownership and watches the nearest
// existing ancestor, allowing missing nested dependency directories to appear.
// Every directory under each tree is watched too, so new subdirectories are
// picked up on the next Reconcile.
func (w *fileWatcher) Reconcile(paths []string, trees ...string) error {
	watched, dirs := make(map[string]struct{}), make(map[string]struct{})
	var absTrees []string
	for _, tree := range trees {
		abs, err := filepath.Abs(tree)
		if err != nil {
			return err
		}
		absTrees = append(absTrees, abs)
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != abs && hiddenName(d.Name()) {
					return filepath.SkipDir
				}
				dirs[path] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
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
	w.watched, w.dirs, w.trees = watched, dirs, absTrees
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
			if !relevant {
				relevant = w.treeEventLocked(abs, event)
			}
			w.mu.Unlock()
			if relevant {
				timer.Reset(reloadDebounce)
				tick = timer.C
			}
		}
	}
}

// treeEventLocked reports whether an event under a watched tree can change
// the set of request files: a .http file, or a directory appearing or going.
func (w *fileWatcher) treeEventLocked(abs string, event fsnotify.Event) bool {
	for _, tree := range w.trees {
		rel, err := filepath.Rel(tree, abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if slices.ContainsFunc(strings.Split(rel, string(filepath.Separator)), hiddenName) {
			return false
		}
		if isRequestFile(abs) {
			return true
		}
		if _, watched := w.dirs[abs]; watched {
			return true
		}
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return true
		}
		return false
	}
	return false
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
		if method.File != "" {
			for _, path := range restclient.BodyFilePaths(method.Source, method.File) {
				add(path)
			}
		}
	}
	return paths
}
