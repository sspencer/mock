package mockhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestRequestHook(t *testing.T) {
	methods, err := restclient.Parse("hook.http", strings.NewReader("### Hi\nGET /hi\n\nhello\n"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(methods, nil)
	defer server.Close()
	var mu sync.Mutex
	var events []RequestEvent
	server.SetRequestHook(func(event RequestEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hi", nil))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))
	server.SetRequestHook(nil)
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/hi", nil))

	if len(events) != 2 {
		t.Fatalf("got %d events", len(events))
	}
	if events[0].ID == 0 || events[0].Session == "" || events[0].Match.Route == nil || events[0].Response.Body.Text != "hello" {
		t.Fatalf("first event %+v", events[0])
	}
	if events[1].Match.Route != nil || events[1].Response.Status != http.StatusNotFound {
		t.Fatalf("second event %+v", events[1])
	}
}
