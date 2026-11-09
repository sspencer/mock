package mockhttp

import (
	"encoding/json"
	"fmt"
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

func fixtureServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "users.json"), []byte(`{"users":[{"name":"Ada"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "api.http")
	methods := []restclient.Method{
		{Name: "plain", Method: http.MethodGet, Path: "/plain", Status: 200, Source: source},
		{Name: "users", Method: http.MethodGet, Path: "/users", Status: 200, Source: source, Fixture: "users.json", Body: `{{$fixture.users.0.name}}`},
	}
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil))), dir
}

func getFixture(t *testing.T, server *Server, route string) (*httptest.ResponseRecorder, FixtureFile) {
	t.Helper()
	response := httptest.NewRecorder()
	server.ServeFixture(response, httptest.NewRequest(http.MethodGet, "/fixture?route="+route, nil))
	var file FixtureFile
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &file); err != nil {
			t.Fatal(err)
		}
	}
	return response, file
}

func putFixture(t *testing.T, server *Server, edit FixtureFile) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(edit)
	request := httptest.NewRequest(http.MethodPut, "/fixture", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.ServeFixture(response, request)
	return response
}

func TestServeFixtureReadsAndWritesARoutesFixture(t *testing.T) {
	server, dir := fixtureServer(t)
	revision := server.config.Revision
	route := func(index int) string { return fmt.Sprintf("%d:%d", revision, index) }

	response, file := getFixture(t, server, route(1))
	if response.Code != http.StatusOK || file.Content != `{"users":[{"name":"Ada"}]}` || file.Fixture != "users.json" || file.Version == "" {
		t.Fatalf("GET = %d %+v", response.Code, file)
	}
	if want, _ := filepath.Abs(filepath.Join(dir, "users.json")); file.File != want {
		t.Errorf("file = %q, want %q", file.File, want)
	}
	for _, id := range []string{route(0), route(9), "x", "99:1"} {
		if response, _ := getFixture(t, server, id); response.Code != http.StatusNotFound {
			t.Errorf("GET route %q = %d, want 404", id, response.Code)
		}
	}

	if response := putFixture(t, server, FixtureFile{File: file.File, Content: `{"users": [`, Version: file.Version}); response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "line 1, column 12: unexpected end") {
		t.Errorf("invalid JSON = %d %s", response.Code, response.Body.String())
	}
	if response := putFixture(t, server, FixtureFile{File: file.File, Content: `{} {}`, Version: file.Version}); response.Code != http.StatusBadRequest {
		t.Errorf("two JSON values = %d", response.Code)
	}
	if response := putFixture(t, server, FixtureFile{File: filepath.Join(dir, "other.json"), Content: `{}`, Version: file.Version}); response.Code != http.StatusForbidden {
		t.Errorf("a file that is no route's fixture = %d, want 403", response.Code)
	}
	if response := putFixture(t, server, FixtureFile{File: file.File, Content: `{}`, Version: "stale"}); response.Code != http.StatusConflict {
		t.Errorf("stale version = %d, want 409", response.Code)
	}

	updated := "{\n  \"users\": [{\"name\": \"Grace\"}]\n}\n"
	response = putFixture(t, server, FixtureFile{File: file.File, Content: updated, Version: file.Version})
	if response.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", response.Code, response.Body.String())
	}
	data, _ := os.ReadFile(file.File)
	if string(data) != updated {
		t.Errorf("file = %q", data)
	}
	if info, _ := os.Stat(file.File); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want the original 0600", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("a temporary file was left behind: %v", entries)
	}
	var saved map[string]string
	_ = json.Unmarshal(response.Body.Bytes(), &saved)
	if _, reread := getFixture(t, server, route(1)); reread.Version != saved["version"] {
		t.Errorf("saved version %q != reread %q", saved["version"], reread.Version)
	}

	// The next response reads the new fixture.
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users", nil))
	if recorder.Body.String() != "Grace" {
		t.Errorf("response = %q, want Grace", recorder.Body.String())
	}
}

func TestServeFixtureRefusesCrossSiteWrites(t *testing.T) {
	server, _ := fixtureServer(t)
	request := httptest.NewRequest(http.MethodPut, "/fixture", strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	server.ServeFixture(response, request)
	if response.Code != http.StatusForbidden {
		t.Errorf("PUT without JSON content type or X-Requested-With = %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodDelete, "/fixture", nil)
	response = httptest.NewRecorder()
	server.ServeFixture(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, PUT" {
		t.Errorf("DELETE = %d %q", response.Code, response.Header().Get("Allow"))
	}
}
