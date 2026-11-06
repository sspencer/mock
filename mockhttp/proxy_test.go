package mockhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func proxyTestServer(t *testing.T, routes string) *Server {
	t.Helper()
	methods, err := restclient.Parse("fixture.http", strings.NewReader(routes))
	if err != nil {
		t.Fatal(err)
	}
	return New(methods, nil)
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUpstreamReceivesUnmatchedRequests(t *testing.T) {
	var got struct {
		method, uri, host, body, forwardedHost, via string
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.method, got.uri, got.host, got.body = r.Method, r.URL.RequestURI(), r.Host, string(body)
		got.forwardedHost, got.via = r.Header.Get("X-Forwarded-Host"), r.Header.Get("Via")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"real":true}`)
	}))
	defer upstream.Close()

	s := proxyTestServer(t, "### Mocked\nGET /mocked\n\nmock")
	s.SetUpstream(mustURL(t, upstream.URL+"/v2"))

	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "http://localhost:8080/mocked", nil))
	if w.Body.String() != "mock" || got.method != "" {
		t.Fatalf("matched route was forwarded: body %q, upstream saw %q", w.Body, got.method)
	}

	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "http://localhost:8080/users?role=admin", strings.NewReader(`{"name":"alex"}`)))
	if w.Code != http.StatusCreated || w.Body.String() != `{"real":true}` || w.Header().Get("X-Upstream") != "yes" {
		t.Fatalf("response not passed through: %d %q %v", w.Code, w.Body, w.Header())
	}
	if got.method != "POST" || got.uri != "/v2/users?role=admin" || got.body != `{"name":"alex"}` {
		t.Fatalf("upstream got %+v", got)
	}
	if got.host != mustURL(t, upstream.URL).Host || got.forwardedHost != "localhost:8080" || !strings.HasPrefix(got.via, "1.1 mock-") {
		t.Fatalf("upstream headers: %+v", got)
	}

	event := s.events[len(s.events)-1]
	if event.Match.Upstream != upstream.URL+"/v2" || event.Match.Route != nil {
		t.Fatalf("event match = %+v", event.Match)
	}
	if event.Response.Status != http.StatusCreated || event.Response.Body.Text != `{"real":true}` || event.Request.Body.Text != `{"name":"alex"}` {
		t.Fatalf("event = %+v", event)
	}
	if len(event.Match.Candidates) == 0 {
		t.Fatal("proxied request should still explain near-miss routes")
	}
}

func TestUpstreamFailureIsBadGateway(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	upstreamURL := upstream.URL
	upstream.Close()

	s := proxyTestServer(t, "### Mocked\nGET /mocked\n\nmock")
	s.SetUpstream(mustURL(t, upstreamURL))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "upstream request failed") {
		t.Fatalf("got %d %q", w.Code, w.Body)
	}
	if event := s.events[0]; event.Response.Status != http.StatusBadGateway || event.Match.Upstream != upstreamURL {
		t.Fatalf("event = %+v", event)
	}
}

func TestUpstreamPointingAtItselfStopsTheLoop(t *testing.T) {
	s := proxyTestServer(t, "### Mocked\nGET /mocked\n\nmock")
	self := httptest.NewServer(s)
	defer self.Close()
	s.SetUpstream(mustURL(t, self.URL))

	resp, err := http.Get(self.URL + "/missing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusLoopDetected {
		t.Fatalf("status = %d, want 508", resp.StatusCode)
	}
	// The outer request is logged after its response is sent, so wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		n := len(s.events)
		s.mu.Unlock()
		if n == 2 {
			break
		}
		if n > 2 || time.Now().After(deadline) {
			t.Fatalf("want the original request and one looped copy, got %d events", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNoUpstreamStillAnswers404(t *testing.T) {
	s := proxyTestServer(t, "### Mocked\nGET /mocked\n\nmock")
	s.SetUpstream(nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
	if w.Code != http.StatusNotFound || s.events[0].Match.Upstream != "" {
		t.Fatalf("got %d, match %+v", w.Code, s.events[0].Match)
	}
}
