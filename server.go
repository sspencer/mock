package mock

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/julienschmidt/httprouter"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"
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

// Watch for route changes (user edits api file)
func (s *Server) Watch(incomingRoutes chan []*Route) {
	go func() {
		for {
			s.WatchRoutes(<-incomingRoutes)
		}
	}()
}

func (s *Server) WatchRoutes(routes []*Route) {
	s.Lock()
	defer s.Unlock()

	router := httprouter.New()
	router.MethodNotAllowed = s.notAllowed
	router.NotFound = s.notFound

	log.Println("Updating router with new routes:")

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

func (s *Server) WatchFile(fn string, delay time.Duration) {
	routesCh := make(chan []*Route)
	s.Watch(routesCh)

	watchFile(fn, routesParser(fn, delay, routesCh))
}

func watchFile(fn string, parser func()) {

	// parse file at start
	parser()

	watcher, err := fsnotify.NewWatcher()
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

				if event.Op&fsnotify.Write == fsnotify.Write && path.Base(fn) == path.Base(event.Name) {
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

	err = watcher.Add(filepath.Dir(fn))
	if err != nil {
		log.Fatal(err)
	}
}

func routesParser(fn string, delay time.Duration, ch chan []*Route) func() {
	return func() {
		routes, err := RoutesFile(fn, delay)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %s\n", err.Error())
		} else {
			ch <- routes
		}
	}
}
