package mockhttp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func TestRateLimiterSlidingWindow(t *testing.T) {
	var l rateLimiter
	start := time.Unix(1000, 0)
	for i := range 3 {
		if ok, _ := l.allow(start.Add(time.Duration(i)*100*time.Millisecond), 3, time.Second); !ok {
			t.Fatalf("request %d rejected", i)
		}
	}
	ok, wait := l.allow(start.Add(500*time.Millisecond), 3, time.Second)
	if ok || wait != 500*time.Millisecond {
		t.Fatalf("4th request = %v, wait %s; want rejected, 500ms", ok, wait)
	}
	// Rejected requests do not use the budget: the first slot frees at 1s.
	if ok, _ := l.allow(start.Add(time.Second), 3, time.Second); !ok {
		t.Fatal("request after the first expired was rejected")
	}
	// Lowering the limit keeps only the newest timestamps.
	if ok, _ := l.allow(start.Add(time.Second), 1, time.Second); ok {
		t.Fatal("request over a lowered limit was accepted")
	}
}

func TestRateLimitAnswers429(t *testing.T) {
	load := func(source string) []restclient.Method {
		t.Helper()
		methods, err := restclient.Parse("test.http", strings.NewReader(source))
		if err != nil {
			t.Fatal(err)
		}
		return methods
	}
	limited := "### Search\n@rate-limit = 2/1m\nGET /search\n\nok\n"
	server := New(load(limited), slog.New(slog.NewTextHandler(io.Discard, nil)))
	get := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/search", nil))
		return response
	}
	for i := range 2 {
		if code := get().Code; code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i, code)
		}
	}
	response := get()
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request = %d, want 429", response.Code)
	}
	if retry := response.Header().Get("Retry-After"); retry != "60" {
		t.Errorf("Retry-After = %q, want 60", retry)
	}
	if !strings.Contains(response.Body.String(), "2/1m0s") {
		t.Errorf("body = %q", response.Body.String())
	}
	events := server.Requests(RequestFilter{})
	if last := events[len(events)-1]; last.Response.Status != http.StatusTooManyRequests || last.Match.Route == nil {
		t.Errorf("logged event = status %d route %v, want a matched 429", last.Response.Status, last.Match.Route)
	}

	// Reloading the same route keeps its budget; removing the limit clears it.
	server.SetMethods(load(limited))
	if code := get().Code; code != http.StatusTooManyRequests {
		t.Errorf("after reload = %d, want 429", code)
	}
	server.SetMethods(load("### Search\nGET /search\n\nok\n"))
	server.SetMethods(load(limited))
	if code := get().Code; code != http.StatusOK {
		t.Errorf("after removing and restoring the limit = %d, want 200", code)
	}
}
