package main

import (
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sspencer/mock/internal/colorlog"
	"github.com/sspencer/mock/internal/data"
)

// MockServer is the http server struct
type MockServer struct {
	*http.Server
	logger colorlog.LoggerFunc
	addr   string
	sync.Mutex
}

// newServer creates a http MockServer running on given port with handlers based on given routes.
func newServer(cfg config) *MockServer {
	logger := colorlog.New(cfg.logRequest, cfg.logResponse)

	notImplemented := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})

	return &MockServer{
		Server: &http.Server{
			Addr:              cfg.addr,
			Handler:           notImplemented,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger: logger,
		addr:   cfg.addr,
	}
}

// serve <stdin> or piped file as mock file input
func newMockReader(cfg config, reader io.Reader) *MockServer {
	routes, err := data.GetEndpointsFromReader(reader)
	if err != nil {
		log.Fatalln(err.Error())
	}

	server := newServer(cfg)
	server.loadRoutes(routes)

	return server
}

// serve all files specified on command line as mock files
func newMockFile(cfg config, fn string) *MockServer {
	server := newServer(cfg)
	server.watchFile(fn)
	return server
}

// loadRoutes reloads all routes handled by the MockServer
func (s *MockServer) loadRoutes(endpoints []*data.Endpoint) {
	s.Lock()
	defer s.Unlock()

	mux := chi.NewRouter()
	mux.Use(s.ColorLogger())
	mux.MethodNotAllowed(methodNotAllowed)
	mux.NotFound(methodNotFound)

	log.Printf("Updating MockServer with new routes on %s\n", s.addr)

	for _, e := range endpoints {
		log.Printf("    adding method %-8s %s\n", e.Method, e.Path)
		mux.MethodFunc(e.Method, e.Path, e.Handle())
	}

	log.Println("--------------------------------")
	s.Handler = mux
}
