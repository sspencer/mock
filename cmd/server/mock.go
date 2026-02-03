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

	"github.com/fsnotify/fsnotify"
	"github.com/go-chi/chi/v5"

	"github.com/sspencer/mock/internal/data"
)

// mockServer is the HTTP server struct.
// It manages routes, clients for SSE, and static files.
type mockServer struct {
	*http.Server
	addr       string
	logPath    string
	staticFS   fs.FS
	clients    map[chan string]struct{}
	clientsMux sync.Mutex

	// Handler routing indirection for safe hot-swap
	handler    http.Handler
	handlerMux sync.RWMutex

	// Watcher for cleanup on shutdown
	watcher *fsnotify.Watcher
}

// newServer creates a new mock server instance.
// It initializes the HTTP server with configuration.
func newServer(cfg Config) *mockServer {
	s := &mockServer{
		Server: &http.Server{
			Addr:              cfg.mockAddr,
			ReadHeaderTimeout: 5 * time.Second,
		},
		clients:  make(map[chan string]struct{}),
		addr:     cfg.mockAddr,
		logPath:  cfg.logPath,
		staticFS: cfg.staticFS,
	}

	// Set up handler indirection wrapper for safe hot-swap
	s.Server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.handlerMux.RLock()
		handler := s.handler
		s.handlerMux.RUnlock()

		if handler != nil {
			handler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
	})

	return s
}

// newStdinServer creates a server that reads routes from stdin.
// It parses the input and sets up routes.
func newStdinServer(cfg Config, reader io.Reader) (*mockServer, error) {
	routes, err := data.GetEndpointsFromReader(reader)
	if err != nil {
		return nil, err
	}

	s := newServer(cfg)
	s.mockRoutes(routes)
	return s, nil
}

// newFileServer creates a server that reads routes from a file.
// It watches the file for changes and reloads routes.
func newFileServer(cfg Config, fn string) (*mockServer, error) {
	s := newServer(cfg)
	s.parseRoutes(fn)

	watcher, err := watchFile(fn, s.parseRoutes)
	if err != nil {
		return nil, err
	}
	s.watcher = watcher // Store for cleanup

	return s, nil
}

// newStaticServer creates a server that serves static files.
// It does not support mocking; only file serving.
func newStaticServer(cfg Config, fn string) *mockServer {
	fn = strings.ReplaceAll(fn, " ", "\\ ")
	s := newServer(cfg)
	mux := chi.NewRouter()
	mux.Use(s.requestLogger(newLogger()))
	mux.Handle("/*", http.StripPrefix("/", http.FileServer(http.Dir(fn))))
	s.handler = mux
	return s
}

// mockRoutes sets up the routes on the server.
// It adds logging, static file serving, and SSE endpoints.
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
		mux.MethodFunc(e.Method, e.Path, e.Handle)
	}

	log.Println("--------------------------------")
	s.handlerMux.Lock()
	s.handler = mux
	s.handlerMux.Unlock()
}

// parseRoutes parses routes from a file and updates the server.
// It is called on file changes.
func (s *mockServer) parseRoutes(fn string) {
	routes, err := data.GetEndpointsFromFile(fn)
	if err != nil {
		log.Printf("failed to parse routes from %q: %v", fn, err)
		return
	}
	s.mockRoutes(routes)
}

// Close cleans up resources, including the file watcher.
// This enables graceful shutdown.
func (s *mockServer) Close() error {
	if s.watcher != nil {
		return s.watcher.Close()
	}
	return nil
}
