package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
)

//go:embed static/index.html
var indexPage []byte

//go:embed static/favicon.ico
var favIcon []byte

// registerClient adds a new client channel
func (s *mockServer) registerClient() chan string {
	s.clientsMux.Lock()
	defer s.clientsMux.Unlock()

	clientChan := make(chan string)
	s.clients[clientChan] = struct{}{}
	return clientChan
}

// unregisterClient removes a client channel
func (s *mockServer) unregisterClient(clientChan chan string) {
	s.clientsMux.Lock()
	defer s.clientsMux.Unlock()

	if _, ok := s.clients[clientChan]; ok {
		close(clientChan)
		delete(s.clients, clientChan)
	}
}

// broadcast sends a message to all connected clients
func (s *mockServer) broadcast(message string) {
	s.clientsMux.Lock()
	defer s.clientsMux.Unlock()

	for clientChan := range s.clients {
		select {
		case clientChan <- message:
		default:
			// Skip if channel is blocked
		}
	}
}

func (s *mockServer) indexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, err := w.Write(indexPage)
	if err != nil {
		log.Println(err)
		return
	}
}

func (s *mockServer) iconHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/x-icon")
	_, err := w.Write(favIcon)
	if err != nil {
		log.Println(err)
		return
	}
}

func (s *mockServer) sseHandler(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.ProtoMajor == 1 {
		w.Header().Set("Connection", "keep-alive")
	}

	// Create client channel
	clientChan := s.registerClient()
	defer s.unregisterClient(clientChan)

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
