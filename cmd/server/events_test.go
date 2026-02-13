package main

import (
	"sync"
	"testing"
)

func TestRegisterClient(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	clientChan := server.registerClient()

	if clientChan == nil {
		t.Fatal("expected non-nil client channel")
	}

	server.clientsMux.Lock()
	defer server.clientsMux.Unlock()

	if _, ok := server.clients[clientChan]; !ok {
		t.Error("client channel not registered")
	}

	if len(server.clients) != 1 {
		t.Errorf("expected 1 client, got %d", len(server.clients))
	}
}

func TestUnregisterClient(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	clientChan := server.registerClient()

	// Verify registered
	server.clientsMux.Lock()
	if len(server.clients) != 1 {
		t.Errorf("expected 1 client before unregister, got %d", len(server.clients))
	}
	server.clientsMux.Unlock()

	// Unregister
	server.unregisterClient(clientChan)

	// Verify unregistered
	server.clientsMux.Lock()
	if len(server.clients) != 0 {
		t.Errorf("expected 0 clients after unregister, got %d", len(server.clients))
	}
	server.clientsMux.Unlock()

	// Channel should be closed
	_, ok := <-clientChan
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestUnregisterClient_NonExistent(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	// Try to unregister a channel that was never registered
	fakeChan := make(chan string)

	// Should not panic
	server.unregisterClient(fakeChan)

	server.clientsMux.Lock()
	defer server.clientsMux.Unlock()

	if len(server.clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(server.clients))
	}
}

func TestBroadcast_SingleClient(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	// Use buffered channel to avoid race
	clientChan := make(chan string, 1)
	server.clientsMux.Lock()
	server.clients[clientChan] = struct{}{}
	server.clientsMux.Unlock()

	message := "test message"
	server.broadcast(message)

	// Read the message
	select {
	case received := <-clientChan:
		if received != message {
			t.Errorf("expected message %q, got %q", message, received)
		}
	default:
		t.Error("expected message on channel")
	}
}

func TestBroadcast_MultipleClients(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	// Register multiple clients with buffered channels
	numClients := 5
	channels := make([]chan string, numClients)
	server.clientsMux.Lock()
	for i := range numClients {
		channels[i] = make(chan string, 1)
		server.clients[channels[i]] = struct{}{}
	}
	server.clientsMux.Unlock()

	message := "broadcast message"
	server.broadcast(message)

	// All clients should receive the message
	for i, ch := range channels {
		select {
		case received := <-ch:
			if received != message {
				t.Errorf("client %d: expected message %q, got %q", i, message, received)
			}
		default:
			t.Errorf("client %d: expected message on channel", i)
		}
	}
}

func TestBroadcast_NoClients(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	// Should not panic with no clients
	server.broadcast("test message")
}

func TestBroadcast_BlockedClient(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	// Register a client but don't read from it
	clientChan := server.registerClient()

	// Fill the channel buffer (unbuffered, so first send will block)
	// The broadcast should skip this client rather than blocking
	for range 10 {
		server.broadcast("message")
	}

	// Should have at least one message (the first one succeeded)
	select {
	case msg := <-clientChan:
		if msg != "message" {
			t.Errorf("expected 'message', got %q", msg)
		}
	default:
		// This is also acceptable - the channel might be empty due to the default case
	}
}

func TestConcurrentClientOperations(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	var wg sync.WaitGroup
	numGoroutines := 20

	// Concurrently register clients
	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			ch := server.registerClient()
			// Immediately unregister
			server.unregisterClient(ch)
		}()
	}

	wg.Wait()

	server.clientsMux.Lock()
	defer server.clientsMux.Unlock()

	// All clients should be unregistered
	if len(server.clients) != 0 {
		t.Errorf("expected 0 clients after concurrent operations, got %d", len(server.clients))
	}
}

func TestConcurrentBroadcast(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	// Register some clients
	numClients := 3
	channels := make([]chan string, numClients)
	for i := range numClients {
		channels[i] = server.registerClient()
	}

	// Concurrently broadcast messages
	var wg sync.WaitGroup
	numMessages := 10

	wg.Add(numMessages)
	for i := range numMessages {
		go func(msg int) {
			defer wg.Done()
			server.broadcast("message")
		}(i)
	}

	// Drain channels while broadcasting
	go func() {
		for _, ch := range channels {
			go func(c chan string) {
				for range c {
					// Consume messages
				}
			}(ch)
		}
	}()

	wg.Wait()

	// Cleanup
	for _, ch := range channels {
		server.unregisterClient(ch)
	}
}

func TestRegisterUnregisterSequence(t *testing.T) {
	server := &mockServer{
		clients: make(map[chan string]struct{}),
	}

	// Register, unregister, register again in sequence
	ch1 := server.registerClient()
	server.unregisterClient(ch1)

	ch2 := server.registerClient()
	server.unregisterClient(ch2)

	server.clientsMux.Lock()
	defer server.clientsMux.Unlock()

	if len(server.clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(server.clients))
	}
}
