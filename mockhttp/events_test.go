package mockhttp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mock/restclient"
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

	line, event := readEventLine(t, response.Body.String())
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("event line = %q, want data prefix", line)
	}
	if event.Request.Method != http.MethodGet || event.Request.URL != "/users" {
		t.Fatalf("event request = %#v, want GET /users", event.Request)
	}
	if event.Response.Status != http.StatusOK {
		t.Fatalf("event status = %d, want %d", event.Response.Status, http.StatusOK)
	}
}

func TestPublishRequestBoundsStoredEventsAndDropsFullSubscribers(t *testing.T) {
	server := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, subscriber := server.subscribe()
	defer server.unsubscribe(subscriber)
	for i := 0; i < cap(subscriber); i++ {
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

func readEventLine(t *testing.T, body string) (string, RequestEvent) {
	t.Helper()

	line, err := bufio.NewReader(strings.NewReader(body)).ReadString('\n')
	if err != nil {
		t.Fatalf("ReadString() error = %v", err)
	}
	var event RequestEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &event); err != nil {
		t.Fatalf("Unmarshal() error = %v for line %q", err, line)
	}
	return line, event
}
