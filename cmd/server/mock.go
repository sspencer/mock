package main

import (
	_ "embed"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sspencer/mock/internal/data"
)

// mockServer is the http server struct
type mockServer struct {
	*http.Server
	//logger loggerFunc
	addr string
	sync.Mutex
}

// newServer creates an http mockServer running on given port with handlers based on given routes.
func newServer(cfg config) *mockServer {
	//logger := newLogger()

	notImplemented := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})

	return &mockServer{
		Server: &http.Server{
			Addr:              cfg.mockAddr,
			Handler:           notImplemented,
			ReadHeaderTimeout: 5 * time.Second,
		},
		//logger: logger,
		addr: cfg.mockAddr,
	}
}

// serve <stdin> or piped file as mock file input
func newStdinServer(cfg config, reader io.Reader) *mockServer {
	routes, err := data.GetEndpointsFromReader(reader)
	if err != nil {
		log.Fatalln(err.Error())
	}

	s := newServer(cfg)
	s.loadRoutes(routes)

	return s
}

// serve all files specified on command line as mock files
func newFileServer(cfg config, fn string) *mockServer {
	s := newServer(cfg)
	s.parseRoutes(fn)
	watchFile(fn, s.parseRoutes)
	return s
}

func newStaticServer(cfg config, fn string) *mockServer {
	fn = strings.ReplaceAll(fn, " ", "\\ ")

	s := newServer(cfg)
	mux := chi.NewRouter()
	mux.Use(s.requestLogger(newLogger()))

	mux.Handle("/*", http.StripPrefix("/", http.FileServer(http.Dir(fn))))
	s.Handler = mux

	return s
}

// loadRoutes reloads all routes handled by the mockServer
func (s *mockServer) loadRoutes(endpoints []*data.Endpoint) {
	s.Lock()
	defer s.Unlock()

	mux := chi.NewRouter()
	mux.Use(s.requestLogger(newLogger()))
	mux.MethodNotAllowed(methodNotAllowed)
	mux.NotFound(methodNotFound)

	log.Printf("Serving mock routes on %s\n", s.addr)

	for _, e := range endpoints {
		log.Printf("  => %-6s   %s\n", e.Method, e.Path)
		mux.MethodFunc(e.Method, e.Path, e.Handle())
	}

	log.Println("--------------------------------")
	s.Handler = mux
}

func (s *mockServer) parseRoutes(fn string) {
	routes, err := data.GetEndpointsFromFile(fn)
	if err != nil {
		log.Println(err.Error())
		return
	}

	s.loadRoutes(routes)
}
