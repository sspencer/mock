package mockhttp

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestRouteProxyForwardsMatchingRequests(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Backend", "yes")
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, "%s %s host=%s via=%s body=%s", r.Method, r.URL.RequestURI(), r.Host, r.Header.Get("Via"), body)
	}))
	defer backend.Close()

	s := newTestServer(t, fmt.Sprintf(`@backend = %[1]s

### Sandbox payments
@header.X-Env = sandbox
@proxy = {{backend}}/v2
POST /payments/*rest

### Rewritten
@proxy = {{backend}}/api/{{$path.rest}}
GET /legacy/*rest

### Rewritten query
@proxy = {{backend}}/search?q={{$query.term}}
GET /find

### Mocked payments
POST /payments/*rest

mocked
`, backend.URL))
	host := strings.TrimPrefix(backend.URL, "http://")

	req := httptest.NewRequest("POST", "/payments/charge?amount=5", strings.NewReader("card=1"))
	req.Header.Set("X-Env", "sandbox")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	want := "POST /v2/payments/charge?amount=5 host=" + host
	if rec.Code != http.StatusAccepted || !strings.HasPrefix(rec.Body.String(), want) || !strings.HasSuffix(rec.Body.String(), "body=card=1") || rec.Header().Get("X-Backend") != "yes" {
		t.Fatalf("proxied = %d %q, want 202 %q...", rec.Code, rec.Body.String(), want)
	}
	if !strings.Contains(rec.Body.String(), "via=1.1 mock-") {
		t.Errorf("missing Via: %q", rec.Body.String())
	}

	if _, got := serveBody(t, s, "/payments/charge", "card=1"); got != "mocked" {
		t.Errorf("unproxied = %q, want mocked", got)
	}

	for target, want := range map[string]string{
		"/legacy/users/7?page=2": "GET /api/users/7?page=2 ",
		"/find?term=go":          "GET /search?q=go ",
	} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
		if !strings.HasPrefix(rec.Body.String(), want) {
			t.Errorf("%s: got %q, want prefix %q", target, rec.Body.String(), want)
		}
	}

	events := s.Requests(RequestFilter{})
	if len(events) == 0 || events[0].Match.Upstream == "" {
		t.Fatalf("events = %+v, want an upstream marker", events)
	}
}

func TestRouteProxyFailuresAndLoops(t *testing.T) {
	backend := httptest.NewServer(http.NotFoundHandler())
	url := backend.URL
	backend.Close()
	s := newTestServer(t, "### Down\n@proxy = "+url+"\nGET /down\n\n### Missing\n@proxy = "+url+"/{{$query.id}}\nGET /missing\n")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/down", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("down: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/missing", nil))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "placeholder") {
		t.Errorf("missing placeholder: %d %q", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest("GET", "/down", nil)
	req.Header.Set("Via", "1.1 "+s.via())
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusLoopDetected {
		t.Errorf("loop: %d", rec.Code)
	}
}

func TestRouteProxyWarnsAboutIgnoredResponse(t *testing.T) {
	methods, err := restclient.Parse("p.http", strings.NewReader("### P\n@proxy = http://example.com\nGET /p\nX-Ignored: 1\n\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	New(methods, slog.New(slog.NewTextHandler(&logs, nil)))
	if !strings.Contains(logs.String(), "are ignored") {
		t.Errorf("logs = %s", logs.String())
	}
}
