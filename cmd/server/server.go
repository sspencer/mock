package main

import (
	"fmt"
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
	port   int
	sync.Mutex
}

// newServer creates a http MockServer running on given port with handlers based on given routes.
func newServer(port int, logRequest, logResponse bool) *MockServer {
	logger := colorlog.New(logRequest, logResponse)

	notImplemented := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})

	return &MockServer{
		Server: &http.Server{
			Addr:              fmt.Sprintf(":%d", port),
			Handler:           notImplemented,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger: logger,
		port:   port,
	}
}

// serve <stdin> or piped file as mock file input
func newMockServerReader(port int, reader io.Reader, logRequest, logResponse bool) *MockServer {
	routes, err := data.GetEndpointsFromReader(reader)
	if err != nil {
		log.Fatalln(err.Error())
	}

	server := newServer(port, logRequest, logResponse)
	server.loadRoutes(routes)

	return server
}

// serve all files specified on command line as mock files
func newMockServerFile(port int, fn string, logRequest, logResponse bool) *MockServer {
	server := newServer(port, logRequest, logResponse)
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

	log.Printf("Updating MockServer with new routes on :%d\n", s.port)

	for _, e := range endpoints {
		log.Printf("    adding method %-8s %s\n", e.Method, e.Path)
		mux.MethodFunc(e.Method, e.Path, e.Handle())
	}

	log.Println("--------------------------------")
	s.Handler = mux
}
