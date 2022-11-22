package main

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/julienschmidt/httprouter"
	"github.com/sspencer/mock/internal/colorlog"
	"github.com/sspencer/mock/internal/data"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
)

// server is something
type server struct {
	*http.Server
	notFound    http.HandlerFunc
	notAllowed  http.HandlerFunc
	logRequests bool
	logger      colorlog.ResponseLoggerFunc
	sync.Mutex
}

// newServer creates a http server running on given port with handlers based on given routes.
func newServer(port int, logRequests bool) *server {

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})

	logger := colorlog.NewResponseLoggerFunc()
	notAllowed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger(http.StatusMethodNotAllowed, r)
		http.Error(w, "405 method Not Allowed", http.StatusNotImplemented)
	})

	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger(http.StatusNotFound, r)
		http.Error(w, "404 Page Not Found", http.StatusNotFound)
	})

	return &server{
		Server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: h,
		},
		notAllowed:  notAllowed,
		notFound:    notFound,
		logRequests: logRequests,
		logger:      logger,
	}
}

// loadRoutes reloads all routes handled by the server
func (s *server) loadRoutes(endpoints []*data.Endpoint) {
	s.Lock()
	defer s.Unlock()

	router := httprouter.New()
	router.MethodNotAllowed = s.notAllowed
	router.NotFound = s.notFound

	log.Println("Updating server with new routes:")

	for _, e := range endpoints {
		log.Printf("    adding method %-8s %s\n", e.Method, e.Path)
		if s.logRequests {
			router.Handle(e.Method, e.Path, requestLogger(e.Handle(s.logger)))
		} else {
			router.Handle(e.Method, e.Path, e.Handle(s.logger))
		}
	}

	log.Println("--------------------------------")
	s.Handler = router
}

// watchFiles watches the API file(s) for changes, reloading routes upon save
func (s *server) watchFiles(files []string) {
	routesCh := make(chan []*data.Endpoint)

	s.Watch(routesCh)
	doWatchFiles(files, routesParser(files, routesCh))
}

// Watch for route changes (user edits api file)
func (s *server) Watch(incomingRoutes chan []*data.Endpoint) {
	go func() {
		for {
			s.loadRoutes(<-incomingRoutes)
		}
	}()
}

// watchFile monitors specified file, calling the parser function when file changes
func doWatchFiles(files []string, parser func()) {

	// initially parse file at start up
	parser()

	// watch file for changes calling parser() again on change
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	fileMap := make(map[string]bool)
	for _, fn := range files {
		fileMap[path.Base(fn)] = true
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Write == fsnotify.Write && fileMap[path.Base(event.Name)] == true {
					parser()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("Error watching file:", err)
			}
		}
	}()

	for _, fn := range files {
		err = watcher.Add(filepath.Dir(fn))
		if err != nil {
			log.Fatal(err)
		}
	}
}

// routesParser is the "parser()" function passed into watchFile()
func routesParser(files []string, ch chan []*data.Endpoint) func() {
	return func() {
		routes, err := data.GetEndpointsFromFiles(files)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		} else {
			ch <- routes
		}
	}
}
