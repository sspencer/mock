package mockhttp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestServerDefaultContentTypeAfterGuessing(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"page.html":         "<p>hi</p>",
		"logo.blob":         "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR",
		"page.blob":         "<!DOCTYPE html><p>hi</p>",
		"data.blob":         `{"id":1}`,
		"users/1.json":      `{"id":1}`,
		"users/2.unknownxt": `{"id":2}`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	methods, err := restclient.Parse(filepath.Join(dir, "api.http"), strings.NewReader(`@default.header.Content-Type = application/json

### Page
GET /page

< page.html

### Sniffed PNG
GET /logo

< logo.blob

### Sniffed HTML
GET /sniffed

< page.blob

### Unknown text
GET /data

< data.blob

### User
GET /users/:id

< users/{{$path.id}}.json

### Inline JSON
GET /inline

{"name":"{{$query.name}}"}

### Inline HTML
GET /inline.html

<!DOCTYPE html><p>hi</p>

### Own type
GET /csv
Content-Type: text/csv

a,b
`))
	if err != nil {
		t.Fatal(err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for target, want := range map[string]string{
		"/page":              "text/html",
		"/logo":              "image/png",
		"/sniffed":           "text/html",
		"/data":              "application/json",
		"/users/1":           "application/json",
		"/inline?name=a%22b": "application/json",
		"/inline.html":       "text/html",
		"/csv":               "text/csv",
	} {
		response := serveRoute(server, "GET", target)
		if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, want) {
			t.Errorf("GET %s Content-Type = %q, want %s", target, got, want)
		}
		if target == "/inline?name=a%22b" && response.Body.String() != `{"name":"a\"b"}` {
			t.Errorf("inline JSON body = %s; the default type should render it in JSON mode", response.Body.String())
		}
	}

	// Without a default, unknown files are sniffed as before.
	plain, err := restclient.Parse(filepath.Join(dir, "plain.http"), strings.NewReader("### Data\nGET /data\n\n< data.blob\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := serveRoute(New(plain, slog.New(slog.NewTextHandler(io.Discard, nil))), "GET", "/data").Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("no default: Content-Type = %q, want text/plain", got)
	}
}
