package mockhttp

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func coverageServer(t *testing.T, methods ...restclient.Method) *Server {
	t.Helper()
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func hit(s *Server, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func hitsByName(c Coverage) map[string]uint64 {
	out := map[string]uint64{}
	for _, route := range c.Routes {
		out[route.Name] = route.Hits
	}
	return out
}

func TestCoverageCountsHitsAndUnmatchedRequests(t *testing.T) {
	s := coverageServer(t,
		restclient.Method{Name: "users", Method: "GET", Path: "/users", Source: "a.http", Line: 1},
		restclient.Method{Name: "user", Method: "GET", Path: "/users/:id", Source: "a.http", Line: 5},
		restclient.Method{Name: "unused", Method: "DELETE", Path: "/users/:id", Query: map[string][]string{"force": {"1"}}, Source: "a.http", Line: 9},
	)
	hit(s, "GET", "/users")
	hit(s, "GET", "/users?page=2")
	hit(s, "GET", "/users/7")
	hit(s, "GET", "/missing")
	hit(s, "GET", "/missing?x=1")
	hit(s, "POST", "/users")

	c := s.Coverage()
	if got := hitsByName(c); got["users"] != 2 || got["user"] != 1 || got["unused"] != 0 {
		t.Fatalf("hits = %v", got)
	}
	if c.Routes[2].Path != "/users/:id?force=1" || c.Routes[2].Source != "a.http" || c.Routes[2].Line != 9 {
		t.Fatalf("unused route = %+v", c.Routes[2])
	}
	want := []UnmatchedRequest{{Method: "GET", Path: "/missing", Count: 2}, {Method: "POST", Path: "/users", Count: 1}}
	if c.UnmatchedTotal != 3 || fmt.Sprint(c.Unmatched) != fmt.Sprint(want) {
		t.Fatalf("unmatched = %d %+v, want 3 %+v", c.UnmatchedTotal, c.Unmatched, want)
	}

	// Clearing the request log keeps coverage.
	s.ClearEvents()
	if s.Coverage().UnmatchedTotal != 3 {
		t.Fatal("ClearEvents reset coverage")
	}
}

func TestCoverageMarksProxiedRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(upstream.Close)
	s := coverageServer(t)
	s.SetUpstream(mustURL(t, upstream.URL))
	hit(s, "GET", "/real")
	c := s.Coverage()
	if c.UnmatchedTotal != 1 || len(c.Unmatched) != 1 || !c.Unmatched[0].Proxied {
		t.Fatalf("coverage = %+v, want one proxied request", c)
	}
}

func TestCoverageCapsDistinctUnmatchedRequests(t *testing.T) {
	s := coverageServer(t)
	for i := range maxUnmatchedKinds + 5 {
		hit(s, "GET", fmt.Sprintf("/p%d", i))
	}
	hit(s, "GET", "/p0")
	c := s.Coverage()
	if len(c.Unmatched) != maxUnmatchedKinds || c.UnmatchedTotal != maxUnmatchedKinds+6 || c.Unmatched[0].Count != 2 {
		t.Fatalf("len = %d total = %d first = %+v", len(c.Unmatched), c.UnmatchedTotal, c.Unmatched[0])
	}
}

func TestHitsSurviveReloadForUnchangedRoutes(t *testing.T) {
	a := restclient.Method{Name: "a", Method: "GET", Path: "/a", Source: "x.http", Line: 1}
	dup := restclient.Method{Name: "dup", Method: "GET", Path: "/d", Source: "x.http", Line: 4}
	s := coverageServer(t, a, dup, dup)
	hit(s, "GET", "/a")
	hit(s, "GET", "/d")

	// Lines move and a route is added; the identical duplicates pair up in order.
	a.Line, dup.Line = 3, 6
	b := restclient.Method{Name: "b", Method: "GET", Path: "/b", Source: "x.http", Line: 1}
	s.SetMethods([]restclient.Method{b, a, dup, dup})
	c := s.Coverage()
	got := []uint64{c.Routes[0].Hits, c.Routes[1].Hits, c.Routes[2].Hits, c.Routes[3].Hits}
	if fmt.Sprint(got) != "[0 1 1 0]" {
		t.Fatalf("hits after reload = %v, want [0 1 1 0]", got)
	}

	// A changed path is a different route.
	a.Path = "/a2"
	s.SetMethods([]restclient.Method{a})
	if hits := s.Coverage().Routes[0].Hits; hits != 0 {
		t.Fatalf("changed route hits = %d, want 0", hits)
	}
}

func TestRoutesAndEventsReportHits(t *testing.T) {
	s := coverageServer(t, restclient.Method{Name: "a", Method: "GET", Path: "/a", Source: "x.http", Line: 1})
	hit(s, "GET", "/a")
	hit(s, "GET", "/a")
	events, _ := s.subscribe()
	if route := events[1].Match.Route; route == nil || route.Hits != 2 {
		t.Fatalf("event route = %+v, want hits 2", route)
	}
	w := httptest.NewRecorder()
	s.ServeRoutes(w, httptest.NewRequest("GET", "/routes", nil))
	var routes []RouteInfo
	if err := json.Unmarshal(w.Body.Bytes(), &routes); err != nil || len(routes) != 1 || routes[0].Hits != 2 {
		t.Fatalf("routes = %+v err = %v, want hits 2", routes, err)
	}
}
