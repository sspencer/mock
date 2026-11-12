package mockhttp

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const retryRoutes = `### [503] Fail twice
@times = 2
GET /orders

down

### [429] Throttled once
@times = 1
@rate-limit = 1/1m
GET /search

slow down

### Orders
GET /orders

ok

### Search
GET /search

results
`

func TestTimesFallsThroughAfterN(t *testing.T) {
	s := newTestServer(t, retryRoutes)
	var got []int
	for range 4 {
		code, _ := serveRequest(t, s, "GET", "/orders", "", "")
		got = append(got, code)
	}
	if want := []int{503, 503, 200, 200}; !equalInts(got, want) {
		t.Fatalf("statuses = %v, want %v", got, want)
	}

	rec := httptest.NewRecorder()
	s.ServeRoutes(rec, httptest.NewRequest("GET", "/routes", nil))
	var routes []RouteInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &routes); err != nil {
		t.Fatal(err)
	}
	if routes[0].Times != 2 || routes[0].TimesLeft == nil || *routes[0].TimesLeft != 0 || routes[2].TimesLeft != nil {
		t.Errorf("routes = times %d left %v / %v", routes[0].Times, routes[0].TimesLeft, routes[2].TimesLeft)
	}

	// Reset starts the count over; a reload that keeps the route keeps it.
	s.SetMethods(s.Methods())
	if code, _ := serveRequest(t, s, "GET", "/orders", "", ""); code != 200 {
		t.Errorf("after reload = %d, want the count kept", code)
	}
	s.Reset()
	if code, _ := serveRequest(t, s, "GET", "/orders", "", ""); code != 503 {
		t.Errorf("after reset = %d, want 503", code)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTimesIsExactUnderConcurrency(t *testing.T) {
	s := newTestServer(t, "### [503] Once each\n@times = 5\nGET /x\n\n\n### OK\nGET /x\n\nok\n")
	var mu sync.Mutex
	failures := 0
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if code, _ := serveRequest(t, s, "GET", "/x", "", ""); code == 503 {
				mu.Lock()
				failures++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if failures != 5 {
		t.Errorf("@times = 5 answered %d of 50 concurrent requests", failures)
	}
}

func TestResetEndpoint(t *testing.T) {
	s := newTestServer(t, retryRoutes)
	serveRequest(t, s, "GET", "/search", "", "")
	if code, _ := serveRequest(t, s, "GET", "/search", "", ""); code != 200 {
		t.Fatalf("second search = %d, want the fallback 200", code)
	}
	if _, err := s.SetFault("Orders", FaultOverride{Fault: "reset"}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/reset", nil)
	req.Header.Set("X-Requested-With", "test")
	rec := httptest.NewRecorder()
	s.ServeReset(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"revision"`) {
		t.Fatalf("POST /reset = %d %s", rec.Code, rec.Body.String())
	}
	if code, _ := serveRequest(t, s, "GET", "/search", "", ""); code != 429 {
		t.Errorf("search after reset = %d, want 429 again", code)
	}
	if len(s.Faults()) != 0 {
		t.Errorf("faults after reset = %v", s.Faults())
	}
	cross := httptest.NewRequest("POST", "/reset", nil)
	cross.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	s.ServeReset(rec, cross)
	if rec.Code != 403 {
		t.Errorf("cross-origin reset = %d", rec.Code)
	}
}

func TestTimesNearMissAndShadows(t *testing.T) {
	s := newTestServer(t, "### [503] Flaky\n@times = 1\nGET /only\n\ndown\n")
	var event RequestEvent
	s.SetRequestHook(func(e RequestEvent) { event = e })
	serveRequest(t, s, "GET", "/only", "", "")
	if code, _ := serveRequest(t, s, "GET", "/only", "", ""); code != 404 {
		t.Fatalf("second request = %d, want 404", code)
	}
	if len(event.Match.Candidates) != 1 || !strings.Contains(event.Match.Candidates[0].Reasons[0], "@times = 1 already") {
		t.Errorf("candidates = %+v", event.Match.Candidates)
	}
	if logText := shadowWarnings(t, "### A\n@times = 2\nGET /a\n\n### B\nGET /a\n"); strings.Contains(logText, "can never match") {
		t.Errorf("a @times route shadowed the route after it:\n%s", logText)
	}
}
