package mockhttp

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentPublicationIsOrdered(t *testing.T) {
	s := New(nil, nil)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { s.publishRequest(RequestEvent{}) })
	}
	wg.Wait()
	for i, event := range s.events {
		if event.ID != uint64(i+1) {
			t.Fatalf("event %d has id %d", i, event.ID)
		}
	}
}
func TestOverflowDisconnectsAndReconnectResetsGap(t *testing.T) {
	s := New(nil, nil)
	_, ch := s.subscribe()
	for range maxRequestEvents + 50 {
		s.publishRequest(RequestEvent{})
	}
	for range ch {
	} // Overflow closes the subscription instead of silently dropping.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(ctx, "GET", "/events", nil)
	r.Header.Set("Last-Event-ID", fmt.Sprintf("%s/1", s.session))
	w := httptest.NewRecorder()
	s.ServeEvents(w, r)
	if !strings.Contains(w.Body.String(), "event: reset") || !strings.Contains(w.Body.String(), fmt.Sprintf("%s/%d", s.session, maxRequestEvents+50)) {
		t.Fatal("missing reset or replay")
	}
}
func TestRestartCursorAndClearBoundary(t *testing.T) {
	s := New(nil, nil)
	s.publishRequest(RequestEvent{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	r := httptest.NewRequestWithContext(ctx, "GET", "/events", nil)
	r.Header.Set("Last-Event-ID", "old-session/1")
	w := httptest.NewRecorder()
	s.ServeEvents(w, r)
	if !strings.Contains(w.Body.String(), "event: reset") {
		t.Fatal("missing session reset")
	}
	_, ch := s.subscribe()
	s.publishRequest(RequestEvent{})
	s.ClearEvents()
	event := <-ch
	if event.Kind != "clear" || len(ch) != 0 {
		t.Fatal("clear failed to replace queued history")
	}
}

func TestSSESurvivesServerWriteTimeout(t *testing.T) {
	s := New(nil, nil)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(s.ServeEvents))
	ts.Config.WriteTimeout = 250 * time.Millisecond
	ts.Start()
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	got := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "/after-timeout") {
				got <- scanner.Text()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			got <- "err:" + err.Error()
			return
		}
		got <- "eof"
	}()

	time.Sleep(400 * time.Millisecond)
	s.publishRequest(RequestEvent{Request: EventRequest{URL: "/after-timeout"}})
	select {
	case msg := <-got:
		if !strings.Contains(msg, "/after-timeout") {
			t.Fatalf("SSE died or missed event: %s", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SSE event after WriteTimeout window")
	}
}
