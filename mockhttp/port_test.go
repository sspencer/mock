package mockhttp

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

// requestOnPort builds a request as if it arrived on a listener bound to port.
func requestOnPort(method, target string, port int) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	return r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, addr))
}

func TestRoutesMatchOnlyTheirPort(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Users health
GET http://localhost:8080/health

users

### Auth health
GET http://localhost:9090/health

auth

### Token
POST http://localhost:9090/token

token

### Anywhere
GET /any

any
`))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		method, path string
		port         int
		code         int
		body         string
	}{
		{"GET", "/health", 8080, 200, "users"},
		{"GET", "/health", 9090, 200, "auth"},
		{"POST", "/token", 9090, 200, "token"},
		{"POST", "/token", 8080, 404, ""},
		// A route without a port matches any listener.
		{"GET", "/any", 8080, 200, "any"},
		{"GET", "/any", 9090, 200, "any"},
	} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, requestOnPort(tc.method, tc.path, tc.port))
		if w.Code != tc.code || (tc.body != "" && w.Body.String() != tc.body) {
			t.Errorf("%s %s on :%d = %d %q, want %d %q", tc.method, tc.path, tc.port, w.Code, w.Body.String(), tc.code, tc.body)
		}
	}
}

func TestMismatchExplainsWrongPort(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader("### Token\nPOST http://localhost:9090/token\n\ntoken\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, _, match, ok := s.findMethod(requestOnPort("POST", "/token", 8080), requestBody{})
	if ok {
		t.Fatal("findMethod() matched a route on another port")
	}
	if len(match.Candidates) != 1 {
		t.Fatalf("candidates = %#v, want one", match.Candidates)
	}
	got := match.Candidates[0]
	if got.Port != 9090 || len(got.Reasons) != 1 || got.Reasons[0] != "Served on port 9090; request arrived on port 8080" {
		t.Fatalf("candidate = %#v, want a port mismatch reason", got)
	}
}

func TestRouteInfoIncludesPort(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader("### A\n@port = 7070\nGET /a\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := describeRoute(methods[0], 0, 1).Port; got != 7070 {
		t.Fatalf("RouteInfo.Port = %d, want 7070", got)
	}
}
