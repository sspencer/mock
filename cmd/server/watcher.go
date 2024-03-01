package main

import (
	"log"
	"path"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
	"github.com/sspencer/mock/internal/data"
)

func newWatcher(fn string) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	err = watcher.Add(filepath.Dir(fn))
	if err != nil {
		watcher.Close()
		return nil, err
	}

	return watcher, nil
}

// Watch for route changes (user edits routes file)
func (s *MockServer) routesWatcher(incomingRoutes chan []*data.Endpoint) {
	go func() {
		for {
			s.loadRoutes(<-incomingRoutes)
		}
	}()
}

// watchFile monitors routes file, restarting MockServer on change
func (s *MockServer) watchFile(fn string) {
	ch := make(chan []*data.Endpoint)
	go s.routesWatcher(ch)

	routesParser := func() {
		routes, err := data.GetEndpointsFromFile(fn)
		if err != nil {
			log.Println(err.Error())
		} else {
			ch <- routes
		}
	}

	// parse file at start up, then watch file for changes so it can be reparsed
	routesParser()
	watchForChanges(fn, routesParser)
}

// watchForChanges monitors specified file, calling the parser function when file changes
func watchForChanges(fn string, routesParser func()) {
	// parse routes file every time file is saved
	watcher, err := newWatcher(fn)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Has(fsnotify.Write) && path.Base(fn) == path.Base(event.Name) {
					routesParser()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Error watching file:", err)
			}
		}
	}()
}
