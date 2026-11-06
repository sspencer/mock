// Package ui serves the admin request console: the embedded web UI plus the
// mock server's admin API endpoints.
package ui

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/sspencer/mock/mockhttp"
)

// Config is the bootstrap data injected into the UI page. MockPort lets the
// UI build sample requests for routes on the default mock port, which differs
// from the UI port. MultiPort reports more than one mock listener, so the UI
// shows each request's host and port and each route's port.
type Config struct {
	Version   string `json:"version"`
	MockPort  int    `json:"mockPort"`
	MultiPort bool   `json:"multiPort,omitempty"`
}

// NewHandler serves the request console and its API at the root of the
// admin listener.
func NewHandler(mockServer *mockhttp.Server, assets fs.FS, cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/events", mockServer.ServeEvents)
	mux.HandleFunc("/clear", mockServer.ServeClear)
	mux.HandleFunc("/routes", mockServer.ServeRoutes)
	mux.HandleFunc("/state", mockServer.ServeState)
	mux.Handle("/", fileServer(assets, cfg))
	return mux
}

func fileServer(assets fs.FS, cfg Config) http.Handler {
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			files.ServeHTTP(w, r)
			return
		}
		data, err := fs.ReadFile(assets, "index.html")
		if err != nil {
			http.Error(w, "index.html missing", http.StatusInternalServerError)
			return
		}
		payload, err := json.Marshal(cfg)
		if err != nil {
			http.Error(w, "failed to encode UI config", http.StatusInternalServerError)
			return
		}
		data = bytes.Replace(data, []byte(`{"version":"dev"}`), payload, 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}
