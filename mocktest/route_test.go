package mocktest

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAddRoutesFromGoValues(t *testing.T) {
	api := StartSource(t, "### fallback\nGET /users/:id\n\nfrom file\n")
	names := api.Add(t,
		Route{Path: "/users/:id", Status: 503, JSON: map[string]any{"error": "down", "raw": "{{$path.id}}"}, MatchHeader: map[string]string{"X-Down": "*"}},
		Route{Name: "hello", Method: "POST", Path: "/hello/:name", Body: "hi {{$path.name}}", Template: true, Headers: map[string]string{"X-Mock": "yes"}, Times: 1},
	)
	if len(names) != 2 || names[0] != "GET /users/:id" || names[1] != "hello" {
		t.Fatalf("names = %q", names)
	}

	get := func(method, path string, header http.Header) (int, string, http.Header) {
		t.Helper()
		req, _ := http.NewRequest(method, api.URL+path, nil)
		if header != nil {
			req.Header = header
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data), resp.Header
	}

	status, body, header := get("GET", "/users/7", http.Header{"X-Down": {"1"}})
	if status != 503 || body != `{"error":"down","raw":"{{$path.id}}"}` || header.Get("Content-Type") != "application/json" {
		t.Fatalf("typed JSON route: %d %q %v", status, body, header)
	}
	if status, body, _ := get("GET", "/users/7", nil); status != 200 || !strings.Contains(body, "from file") {
		t.Fatalf("matcher should fall through to the file route: %d %q", status, body)
	}
	status, body, header = get("POST", "/hello/ada", nil)
	if status != 200 || body != "hi ada" || header.Get("X-Mock") != "yes" {
		t.Fatalf("template route: %d %q %v", status, body, header)
	}
	if status, _, _ := get("POST", "/hello/ada", nil); status == 200 {
		t.Fatalf("Times: 1 route answered twice")
	}
	api.AssertCalled(t, "hello", 1)
}

func TestRouteRejectsLineBreaks(t *testing.T) {
	if _, err := (Route{Path: "/x", Headers: map[string]string{"X": "a\nb"}}).method(); err == nil {
		t.Fatal("expected an error for a header with a line break")
	}
	if _, err := (Route{Path: "x"}).method(); err == nil {
		t.Fatal("expected an error for a relative path")
	}
}
