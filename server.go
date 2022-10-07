package mock

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/julienschmidt/httprouter"
)

// Server is something
type Server struct {
	*http.Server
	notFound    http.HandlerFunc
	notAllowed  http.HandlerFunc
	logRequests bool
	logger      responseLogger
	sync.Mutex
}

// NewServer creates a http server running on given port with handlers based on given routes.
func NewServer(port int, logRequests bool) *Server {

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})

	logger := newResponseLogger()
	notAllowed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger(http.StatusMethodNotAllowed, r)
		http.Error(w, "405 Method Not Allowed", http.StatusNotImplemented)
	})

	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger(http.StatusNotFound, r)
		http.Error(w, "404 Page Not Found", http.StatusNotFound)
	})

	return &Server{
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

// WatchRoutes sets all routes handled by the server
func (s *Server) WatchRoutes(routes []*Route) {
	s.Lock()
	defer s.Unlock()

	router := httprouter.New()
	router.MethodNotAllowed = s.notAllowed
	router.NotFound = s.notFound

	log.Println("Updating server with new routes:")

	for _, x := range routes {
		log.Printf("    adding method %-8s %s\n", x.Method, x.Path)
		if s.logRequests {
			router.Handle(x.Method, x.Path, requestLogger(x.Handler(s.logger)))
		} else {
			router.Handle(x.Method, x.Path, x.Handler(s.logger))
		}
	}

	log.Println("--------------------------------")
	s.Handler = router
}

// WatchFiles watches the API file(s) for changes, reloading routes upon save
func (s *Server) WatchFiles(files []string, delay time.Duration) {
	routesCh := make(chan []*Route)

	s.Watch(routesCh)
	watchFiles(files, routesParser(files, delay, routesCh))
}

// Watch for route changes (user edits api file)
func (s *Server) Watch(incomingRoutes chan []*Route) {
	go func() {
		for {
			s.WatchRoutes(<-incomingRoutes)
		}
	}()
}

// watchFile monitors specified file, calling the parser function when file changes
func watchFiles(files []string, parser func()) {

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
func routesParser(files []string, delay time.Duration, ch chan []*Route) func() {
	return func() {
		routes, err := RoutesFiles(files, delay)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		} else {
			ch <- routes
		}
	}
}
