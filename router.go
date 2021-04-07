package mock

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/julienschmidt/httprouter"
)

// Server is something
type Server struct {
	*http.Server
	notFound    http.HandlerFunc
	notAllowed  http.HandlerFunc
	logRequests bool
	delay       time.Duration
	logger      responseLogger
	sync.Mutex
}

// NewServer creates a http server running on given port with handlers based on given schema.
func NewServer(port int, logRequests bool, delay time.Duration) *Server {

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
		delay:       delay,
		logRequests: logRequests,
		logger:      logger,
	}
}

// Watch for schema changes (user edits api file)
func (s *Server) Watch(incomingSchemas chan []*Schema) {
	go func() {
		for {
			s.WatchSchema(<-incomingSchemas)
		}
	}()
}

func (s *Server) WatchSchema(schemas []*Schema) {
	s.Lock()
	defer s.Unlock()

	router := httprouter.New()
	router.MethodNotAllowed = s.notAllowed
	router.NotFound = s.notFound

	log.Println("Updating router with new schema:")

	for _, x := range schemas {
		log.Printf("    adding method %-8s %s\n", x.Method, x.Path)
		if s.logRequests {
			router.Handle(x.Method, x.Path, delayer(s.delay, requestLogger(x.Handler(s.logger))))
		} else {
			router.Handle(x.Method, x.Path, delayer(s.delay, x.Handler(s.logger)))
		}
	}

	log.Println("--------------------------------")
	s.Handler = router
}

func delayer(delay time.Duration, next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		if delay > 0 {
			time.Sleep(delay)
		}
		next(w, r, p)
	}
}
