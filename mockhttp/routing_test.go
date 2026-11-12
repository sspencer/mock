package mockhttp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func routingServer(t *testing.T, source string) *Server {
	t.Helper()
	methods, err := restclient.Parse("test.http", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func serveRoute(server *Server, method, target string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(method, target, nil))
	return response
}

func TestServerMethodListsAndAnyMethod(t *testing.T) {
	server := routingServer(t, `### Health
GET,OPTIONS /health

up

### Maintenance
* /maintenance

down
`)
	for _, tt := range []struct {
		method, target string
		status         int
		body           string
	}{
		{"GET", "/health", 200, "up"},
		{"OPTIONS", "/health", 200, "up"},
		{"HEAD", "/health", 200, ""}, // answered by the GET in the list
		{"POST", "/health", 404, ""},
		{"DELETE", "/maintenance", 200, "down"},
		{"PATCH", "/maintenance", 200, "down"},
	} {
		response := serveRoute(server, tt.method, tt.target)
		if response.Code != tt.status || tt.body != "" && response.Body.String() != tt.body {
			t.Errorf("%s %s = %d %q, want %d %q", tt.method, tt.target, response.Code, response.Body.String(), tt.status, tt.body)
		}
	}
}

func TestServerFallbackRoutesComeLast(t *testing.T) {
	server := routingServer(t, `### [404] Not found
@fallback = true
* /*

missing

### Users
GET /users

users
`)
	if got := serveRoute(server, "GET", "/users"); got.Code != 200 || got.Body.String() != "users" {
		t.Errorf("GET /users = %d %q, want the later non-fallback route", got.Code, got.Body.String())
	}
	if got := serveRoute(server, "POST", "/other"); got.Code != 404 || got.Body.String() != "missing" {
		t.Errorf("POST /other = %d %q, want the fallback", got.Code, got.Body.String())
	}
	routes := server.Methods()
	if info := describeRoute(routes[0], 0, 1); !info.Fallback {
		t.Error("RouteInfo.Fallback not set")
	}
}

func TestServerMatchChance(t *testing.T) {
	const source = `### [500] Flaky
@seed = 7
@match.chance = 30%
GET /orders

boom

### Orders
GET /orders

ok
`
	run := func() string {
		server := routingServer(t, source)
		var pattern strings.Builder
		for range 200 {
			if serveRoute(server, "GET", "/orders").Code == 500 {
				pattern.WriteByte('x')
			} else {
				pattern.WriteByte('.')
			}
		}
		return pattern.String()
	}
	first := run()
	if second := run(); first != second {
		t.Errorf("@seed draws differ between runs:\n%s\n%s", first, second)
	}
	if failures := strings.Count(first, "x"); failures < 30 || failures > 90 {
		t.Errorf("30%% chance chose the route %d times in 200", failures)
	}

	server := routingServer(t, strings.Replace(source, "@seed = 7\n", "", 1))
	failures := 0
	for range 400 {
		if serveRoute(server, "GET", "/orders").Code == 500 {
			failures++
		}
	}
	if failures < 60 || failures > 180 {
		t.Errorf("unseeded 30%% chance chose the route %d times in 400", failures)
	}
	if info := describeRoute(server.Methods()[0], 0, 1); info.Chance != "30%" {
		t.Errorf("RouteInfo.Chance = %q, want 30%%", info.Chance)
	}
}

func TestServerMatchChanceNearMiss(t *testing.T) {
	server := routingServer(t, "### Rare\n@match.chance = 0.0001%\nGET /rare\n\nok\n")
	var event RequestEvent
	server.SetRequestHook(func(e RequestEvent) { event = e })
	if got := serveRoute(server, http.MethodGet, "/rare"); got.Code != 404 {
		t.Fatalf("status = %d, want 404", got.Code)
	}
	if len(event.Match.Candidates) != 1 || !strings.Contains(event.Match.Candidates[0].Reasons[0], "@match.chance = 0.0001%") {
		t.Errorf("candidates = %+v", event.Match.Candidates)
	}
}

func TestWarnShadowedWithFallbackChanceAndMethodLists(t *testing.T) {
	logText := shadowWarnings(t, `### Not found
@fallback = true
* /*

nope

### Users
GET /users

### Flaky
@match.chance = 10%
GET /orders

### Orders
GET /orders

### Any
* /things

### Things
GET,POST /things

### Late fallback
@fallback = true
GET /users
`)
	if strings.Count(logText, "route can never match") != 2 {
		t.Errorf("want 2 shadow warnings:\n%s", logText)
	}
	if !strings.Contains(logText, `method=Things`) || !strings.Contains(logText, `non-fallback route \"Users\"`) {
		t.Errorf("want Things shadowed by Any and Late fallback by Users:\n%s", logText)
	}
	if strings.Contains(logText, "method=Users") || strings.Contains(logText, "method=Orders") {
		t.Errorf("a fallback or @match.chance route shadowed a route:\n%s", logText)
	}
}
