package mockhttp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func TestServeEventsRejectsUnsupportedMethods(t *testing.T) {
	server := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/events", nil)

	server.ServeEvents(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if allow := response.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", allow)
	}
}

func TestServeEventsReplaysStoredEvents(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	response := httptest.NewRecorder()

	server.ServeEvents(response, request)

	event := readSSEEvent(t, response.Body.String())
	if event.ID == 0 {
		t.Fatal("event id = 0, want non-zero")
	}
	if event.Request.Method != http.MethodGet || event.Request.URL != "/users" {
		t.Fatalf("event request = %#v, want GET /users", event.Request)
	}
	if event.Response.Status != http.StatusOK {
		t.Fatalf("event status = %d, want %d", event.Response.Status, http.StatusOK)
	}
}

func TestServeEventsSkipsEventsAtOrBeforeLastEventID(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	first := server.events[0]
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", nil)
	request.Header.Set("Last-Event-ID", strconv.FormatUint(first.ID, 10))
	response := httptest.NewRecorder()
	server.ServeEvents(response, request)

	event := readSSEEvent(t, response.Body.String())
	if event.ID != server.events[1].ID {
		t.Fatalf("event id = %d, want %d (second event only)", event.ID, server.events[1].ID)
	}
}

func TestServeClearAndRoutes(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	if len(server.events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(server.events))
	}

	clear := httptest.NewRecorder()
	clearReq := httptest.NewRequest(http.MethodPost, "/clear", nil)
	clearReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	server.ServeClear(clear, clearReq)
	if clear.Code != http.StatusNoContent {
		t.Fatalf("clear status = %d, want %d", clear.Code, http.StatusNoContent)
	}
	if len(server.events) != 0 {
		t.Fatalf("len(events) after clear = %d, want 0", len(server.events))
	}

	routes := httptest.NewRecorder()
	server.ServeRoutes(routes, httptest.NewRequest(http.MethodGet, "/routes", nil))
	if routes.Code != http.StatusOK {
		t.Fatalf("routes status = %d, want %d", routes.Code, http.StatusOK)
	}
	if body := routes.Body.String(); !strings.Contains(body, `"path":"/users"`) {
		t.Fatalf("routes body = %q, want /users", body)
	}
}

func TestPublishRequestBoundsStoredEventsAndDropsFullSubscribers(t *testing.T) {
	server := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, subscriber := server.subscribe()
	defer server.unsubscribe(subscriber)
	for range cap(subscriber) {
		subscriber <- RequestEvent{}
	}

	for i := 0; i < maxRequestEvents+5; i++ {
		server.publishRequest(RequestEvent{Request: EventRequest{URL: "/" + string(rune('a'+i%26))}})
	}

	if len(server.events) != maxRequestEvents {
		t.Fatalf("len(events) = %d, want %d", len(server.events), maxRequestEvents)
	}
	if len(subscriber) != cap(subscriber) {
		t.Fatalf("len(subscriber) = %d, want still full %d", len(subscriber), cap(subscriber))
	}
	if server.events[0].ID == 0 {
		t.Fatal("stored event missing id")
	}
	if _, ok := server.subscribers[subscriber]; ok {
		t.Fatal("overflow left subscriber registered")
	}
}

func TestSubscribeReturnsSnapshotAndUnsubscribeRemovesSubscriber(t *testing.T) {
	server := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.publishRequest(RequestEvent{Request: EventRequest{URL: "/first"}})

	events, subscriber := server.subscribe()
	if len(events) != 1 || events[0].Request.URL != "/first" {
		t.Fatalf("events = %#v, want snapshot with /first", events)
	}
	if len(server.subscribers) != 1 {
		t.Fatalf("len(subscribers) = %d, want 1", len(server.subscribers))
	}

	server.unsubscribe(subscriber)

	if len(server.subscribers) != 0 {
		t.Fatalf("len(subscribers) = %d, want 0", len(server.subscribers))
	}
}

func TestServeClearRejectsBareFormPOST(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	server.counters["GET /users"] = 3

	clear := httptest.NewRecorder()
	server.ServeClear(clear, httptest.NewRequest(http.MethodPost, "/clear", strings.NewReader("")))
	if clear.Code != http.StatusForbidden {
		t.Fatalf("bare POST status = %d, want %d", clear.Code, http.StatusForbidden)
	}
	if len(server.events) != 1 {
		t.Fatalf("len(events) after rejected clear = %d, want 1", len(server.events))
	}
	if server.counters["GET /users"] != 3 {
		t.Fatalf("counters reset by rejected clear: %#v", server.counters)
	}
}

func TestServeClearAcceptsUIHeaders(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	server.counters["GET /users"] = 3

	xhr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/clear", nil)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	server.ServeClear(xhr, req)
	if xhr.Code != http.StatusNoContent {
		t.Fatalf("X-Requested-With status = %d, want %d", xhr.Code, http.StatusNoContent)
	}
	if len(server.events) != 0 {
		t.Fatalf("len(events) after UI clear = %d, want 0", len(server.events))
	}
	if server.counters["GET /users"] != 3 {
		t.Fatalf("clear unexpectedly reset counters: %#v", server.counters)
	}

	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))
	server.counters["GET /users"] = 1
	jsonClear := httptest.NewRecorder()
	jsonReq := httptest.NewRequest(http.MethodPost, "/clear", strings.NewReader("{}"))
	jsonReq.Header.Set("Content-Type", "application/json")
	server.ServeClear(jsonClear, jsonReq)
	if jsonClear.Code != http.StatusNoContent {
		t.Fatalf("JSON Content-Type status = %d, want %d", jsonClear.Code, http.StatusNoContent)
	}
	if len(server.events) != 0 || server.counters["GET /users"] != 1 {
		t.Fatalf("JSON clear left events=%d counters=%#v", len(server.events), server.counters)
	}
}

func readSSEEvent(t *testing.T, body string) RequestEvent {
	t.Helper()
	var dataLine string
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			dataLine = after
			break
		}
	}
	if dataLine == "" {
		t.Fatalf("no data line in SSE body %q", body)
	}
	var event RequestEvent
	if err := json.Unmarshal([]byte(dataLine), &event); err != nil {
		t.Fatalf("Unmarshal() error = %v for %q", err, dataLine)
	}
	return event
}

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
