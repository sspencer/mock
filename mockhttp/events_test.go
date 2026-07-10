package mockhttp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
	server.ServeClear(clear, httptest.NewRequest(http.MethodPost, "/clear", nil))
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
