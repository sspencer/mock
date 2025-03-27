package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

//go:embed static/index.html
var indexPage []byte

// EventServer manages SSE connections and message broadcasting
type EventServer struct {
	clients    map[chan string]struct{}
	clientsMux sync.Mutex
}

func NewEventServer() *EventServer {
	return &EventServer{
		clients: make(map[chan string]struct{}),
	}
}

// RegisterClient adds a new client channel
func (es *EventServer) RegisterClient() chan string {
	es.clientsMux.Lock()
	defer es.clientsMux.Unlock()

	clientChan := make(chan string)
	es.clients[clientChan] = struct{}{}
	return clientChan
}

// UnregisterClient removes a client channel
func (es *EventServer) UnregisterClient(clientChan chan string) {
	es.clientsMux.Lock()
	defer es.clientsMux.Unlock()

	if _, ok := es.clients[clientChan]; ok {
		close(clientChan)
		delete(es.clients, clientChan)
	}
}

// Broadcast sends a message to all connected clients
func (es *EventServer) Broadcast(message string) {
	es.clientsMux.Lock()
	defer es.clientsMux.Unlock()

	for clientChan := range es.clients {
		select {
		case clientChan <- message:
		default:
			// Skip if channel is blocked
		}
	}
}

func (es *EventServer) indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, err := w.Write(indexPage)
	if err != nil {
		log.Println(err)
		return
	}
}

func (es *EventServer) sseHandler(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create client channel
	clientChan := es.RegisterClient()
	defer es.UnregisterClient(clientChan)

	// Handle client disconnection
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Keep connection alive and send events
	for {
		select {
		case message := <-clientChan:
			fmt.Fprintf(w, "data: %s\n\n", message)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (es *EventServer) startServer(cfg config) {

	mux := chi.NewRouter()
	mux.MethodNotAllowed(methodNotAllowed)
	mux.NotFound(methodNotFound)
	mux.HandleFunc("/", es.indexHandler)
	mux.HandleFunc("/events", es.sseHandler)

	serve := &http.Server{
		Addr:              cfg.eventsAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("Serving admin on %s\n", cfg.eventsAddr)
	log.Fatal(serve.ListenAndServe())
}
