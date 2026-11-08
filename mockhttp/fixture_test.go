package mockhttp

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestFixturePlaceholders(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "users.json"), `{"users":{"7":{"name":"Ada","roles":["admin"],"age":36}},"site.name":"Example"}`)
	writeFile(t, filepath.Join(dir, "other.json"), `{"greeting":"hi"}`)
	writeFile(t, filepath.Join(dir, "body.json"), `{"user":"{{$fixture.users.7.name}}"}`)
	source := filepath.Join(dir, "api.http")
	writeFile(t, source, `@fixture = users.json

### User
GET /users/:id
Content-Type: application/json
X-Site: {{$fixture["site.name"]}}

{"name":"{{$fixture.users.7.name}}","roles":{{$fixture.users.7.roles}},"age":{{$fixture.users.7.age}},"missing":"{{$fixture.users.9.name | default none}}"}

### Greeting
@fixture = other.json
GET /hello

{{$fixture.greeting}}

### From file
GET /file

< body.json
`)
	methods, err := restclient.Load([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	tests := []struct{ target, want string }{
		{"/users/7", `{"name":"Ada","roles":["admin"],"age":36,"missing":"none"}`},
		{"/hello", "hi"},
		{"/file", `{"user":"Ada"}`},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest("GET", tt.target, nil))
		if got := rec.Body.String(); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.target, got, tt.want)
		}
		if tt.target == "/users/7" && rec.Header().Get("X-Site") != "Example" {
			t.Errorf("X-Site = %q", rec.Header().Get("X-Site"))
		}
	}

	// Edits apply to the next request without a reload.
	writeFile(t, filepath.Join(dir, "other.json"), `{"greeting":"hello again"}`)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/hello", nil))
	if rec.Body.String() != "hello again" {
		t.Errorf("after edit = %q", rec.Body.String())
	}
}

func TestFixtureWarnings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.json"), `{"a":`)
	writeFile(t, filepath.Join(dir, "good.json"), `{"a":1}`)
	source := filepath.Join(dir, "api.http")
	writeFile(t, source, "### Bad\n@fixture = bad.json\nGET /bad\n\n{{$fixture.a}}\n\n### Missing key\n@fixture = good.json\nGET /good\n\n{{$fixture.b}}\n\n### No fixture\nGET /none\n\n{{$fixture.a}}\n")
	methods, err := restclient.Load([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	New(methods, slog.New(slog.NewTextHandler(&logs, nil)))
	for _, want := range []string{"bad.json is not valid JSON", "good.json has no value at b", "no @fixture file is set"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs missing %q:\n%s", want, logs.String())
		}
	}
	if _, err := restclient.Parse("x.http", strings.NewReader("### X\n@fixture =\nGET /x\n")); err == nil {
		t.Error("empty @fixture parsed")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
