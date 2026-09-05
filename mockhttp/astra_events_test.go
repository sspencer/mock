package mockhttp

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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
	for range 250 {
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
	if !strings.Contains(w.Body.String(), "event: reset") || !strings.Contains(w.Body.String(), fmt.Sprintf("%s/250", s.session)) {
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
