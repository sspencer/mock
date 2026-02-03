package main

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

type fileChanged func(string)

// Watch a file, but instead of watching the file directly watch
// the parent directory. This solves various issues where files are frequently
// renamed (vim) when editors saving them.
func watchFile(fn string, fileChanger fileChanged) (*fsnotify.Watcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Start listening for events.
	go fileLoop(w, fn, fileChanger)

	st, err := os.Lstat(fn)
	if err != nil {
		w.Close() // Clean up on error
		return nil, err
	}

	if st.IsDir() {
		w.Close() // Clean up on error
		return nil, fmt.Errorf("%q is a directory, not a file", fn)
	}

	// Watch the directory, not the fn itself.
	err = w.Add(filepath.Dir(fn))
	if err != nil {
		w.Close() // Clean up on error
		return nil, err
	}

	return w, nil // Return watcher for cleanup
}

func fileLoop(w *fsnotify.Watcher, fn string, fileChanger fileChanged) {
	for {
		select {
		case err, ok := <-w.Errors:
			if !ok { // Channel was closed (i.e. Watcher.Close() was called).
				return
			}
			log.Printf("Error watching file: %s\n", err.Error())

		case e, ok := <-w.Events:
			if !ok { // Channel was closed (i.e. Watcher.Close() was called).
				return
			}

			changed := e.Has(fsnotify.Write) || e.Has(fsnotify.Create)
			if changed && path.Base(fn) == path.Base(e.Name) {
				fileChanger(fn)
			}
		}
	}
}
