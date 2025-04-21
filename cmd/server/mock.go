package main

import (
	_ "embed"
	"io"
	"io/fs"
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
	addr       string
	logPath    string
	staticFS   fs.FS
	clients    map[chan string]struct{}
	clientsMux sync.Mutex
	sync.Mutex
}

// newServer creates an http mockServer running on given port with handlers based on given routes.
func newServer(cfg config) *mockServer {
	return &mockServer{
		Server: &http.Server{
			Addr:              cfg.mockAddr,
			ReadHeaderTimeout: 5 * time.Second,
		},
		clients:  make(map[chan string]struct{}),
		addr:     cfg.mockAddr,
		logPath:  cfg.logPath,
		staticFS: cfg.staticFS,
	}
}

// serve <stdin> or piped file as mock file input
func newStdinServer(cfg config, reader io.Reader) (*mockServer, error) {
	routes, err := data.GetEndpointsFromReader(reader)
	if err != nil {
		return nil, err
	}

	s := newServer(cfg)
	s.mockRoutes(routes)

	return s, nil
}

// serve all files specified on command line as mock files
func newFileServer(cfg config, fn string) (*mockServer, error) {
	s := newServer(cfg)
	s.parseRoutes(fn)
	err := watchFile(fn, s.parseRoutes)
	if err != nil {
		return nil, err
	}

	return s, nil
}

// newStaticServer serves static directory for convenience, no mocking at all
func newStaticServer(cfg config, fn string) *mockServer {
	fn = strings.ReplaceAll(fn, " ", "\\ ")

	s := newServer(cfg)
	mux := chi.NewRouter()
	mux.Use(s.requestLogger(newLogger()))

	mux.Handle("/*", http.StripPrefix("/", http.FileServer(http.Dir(fn))))
	s.Handler = mux

	return s
}

// mockRoutes reloads all routes handled by the mockServer
func (s *mockServer) mockRoutes(endpoints []*data.Endpoint) {
	mux := chi.NewRouter()
	mux.MethodNotAllowed(methodNotAllowed)
	mux.NotFound(methodNotFound)
	mux.Use(s.requestLogger(newLogger()))
	mux.Handle(s.logPath+"/*", http.StripPrefix("/mock/", http.FileServer(http.FS(s.staticFS))))
	mux.HandleFunc(s.logPath+"/events", s.sseHandler)

	log.Printf("Serving mock routes on %s, logged at http://localhost%s%s/\n", s.addr, s.addr, s.logPath)
	for _, e := range endpoints {
		log.Printf("  => %-6s   %s\n", e.Method, e.Path)
		mux.MethodFunc(e.Method, e.Path, e.Handle())
	}

	log.Println("--------------------------------")
	s.Lock()
	s.Handler = mux
	s.Unlock()
}

func (s *mockServer) parseRoutes(fn string) {
	routes, err := data.GetEndpointsFromFile(fn)
	if err != nil {
		log.Println(err.Error())
		return
	}

	s.mockRoutes(routes)
}
