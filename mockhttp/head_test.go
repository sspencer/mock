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

func newHeadTestServer(t *testing.T, source string) *Server {
	t.Helper()
	methods, err := restclient.Parse("head.http", strings.NewReader(source))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestHeadAnsweredByGetRoute(t *testing.T) {
	server := newHeadTestServer(t, "### [201] User\nGET /users/:id\nX-User: {{$id}}\nContent-Type: application/json\n\n{\"id\":{{$id}}}\n")

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/users/7", nil))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", response.Code)
	}
	if got := response.Header().Get("X-User"); got != "7" {
		t.Errorf("X-User = %q, want 7", got)
	}
	if got := response.Header().Get("Content-Length"); got != "8" {
		t.Errorf("Content-Length = %q, want the GET body length 8", got)
	}
	if response.Body.Len() != 0 {
		t.Errorf("HEAD response has a body: %q", response.Body.String())
	}
	event := lastEvent(t, server)
	if event.Match.Route == nil || event.Match.Route.Name != "User" {
		t.Fatalf("route = %+v, want User", event.Match.Route)
	}
}

func TestHeadPrefersExplicitHeadRoute(t *testing.T) {
	server := newHeadTestServer(t, "### Get\nGET /users\n\n[]\n\n### [204] Head\nHEAD /users\n")

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/users", nil))

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want the explicit HEAD route's 204", response.Code)
	}
}

func TestOtherMethodsDoNotFallBackToGet(t *testing.T) {
	server := newHeadTestServer(t, "### Get\nGET /users\n\n[]\n")

	for _, method := range []string{http.MethodPost, http.MethodOptions} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(method, "/users", nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", method, response.Code)
		}
	}
}

func TestHeadMismatchDoesNotBlameGetMethod(t *testing.T) {
	server := newHeadTestServer(t, "### Get\n@header.X-Key = secret\nGET /users\n\n[]\n")

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodHead, "/users", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	candidates := lastEvent(t, server).Match.Candidates
	if len(candidates) != 1 {
		t.Fatalf("candidates = %+v", candidates)
	}
	for _, reason := range candidates[0].Reasons {
		if strings.Contains(reason, "Expected method") {
			t.Errorf("unexpected method reason %q", reason)
		}
	}
}
