package mock

import (
	"fmt"
	"log"
	"net/http"
	"sync"

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

// NewServer creates a http server running on given port with handlers based on given schema.
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

// Watch for schema changes (user edits api file)
func (s *Server) Watch(incomingSchemas chan []*Schema) {
	go func() {
		for {
			s.updateSchema(<-incomingSchemas)
		}
	}()
}

func (s *Server) updateSchema(schemas []*Schema) {
	s.Lock()
	defer s.Unlock()

	router := httprouter.New()
	router.MethodNotAllowed = s.notAllowed
	router.NotFound = s.notFound

	log.Println("Updating router with new schema:")

	for _, x := range schemas {
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
