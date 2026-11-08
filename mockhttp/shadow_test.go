package mockhttp

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestPathCovers(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"/users", "/users", true},
		{"/users/:id", "/users/:userId", true},
		{"/users/:id", "/users/me", true},
		{"/users/me", "/users/:id", false},
		{"/api/*", "/api/users/:id", true},
		{"/api/*", "/api", true},
		{"/api/*rest", "/api/*", true},
		{"/api/*", "/other/x", false},
		{"/api/users/*", "/api/*", false},
		{"/api/:v/*", "/api/users", true},
		{"/api/:v/*", "/api", false},
		{"/*", "/anything/at/all", true},
		{"/users/:id", "/users/:id/posts", false},
		{"/users/:id", "/users/*", false},
		{"/a%2Fb", "/a%2fb", true},
		{"/docs/", "/docs", true},
		{"/docs", "/docs/", false},
	}
	for _, tt := range tests {
		if _, got := pathCovers(tt.a, tt.b); got != tt.want {
			t.Errorf("pathCovers(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func shadowWarnings(t *testing.T, source string) string {
	t.Helper()
	methods, err := restclient.Parse("routes.http", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	New(methods, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return buf.String()
}

func TestWarnShadowedByCatchAllAndParams(t *testing.T) {
	logText := shadowWarnings(t, `### Catch-all
GET /api/*

{}

### User
GET /api/users/:id

{}

### Any user
GET /users/:id

{}

### Me
GET /users/me

{}

### Renamed
GET /users/:userId

{}

### Other method
POST /api/users

{}
`)
	for _, want := range []string{`method=User`, `method=Me`, `method=Renamed`} {
		if !strings.Contains(logText, want) {
			t.Errorf("log missing %s:\n%s", want, logText)
		}
	}
	if strings.Count(logText, "route can never match") != 3 {
		t.Errorf("want 3 shadow warnings:\n%s", logText)
	}
}

func TestShadowRespectsPathMatchers(t *testing.T) {
	logText := shadowWarnings(t, `### Numeric
@path.id = ~[0-9]+
GET /users/:id

{}

### Numeric again, other name
@path.userId = ~[0-9]+
GET /users/:userId

{}

### Literal number
GET /users/42

{}

### Literal word
GET /users/me

{}
`)
	for _, want := range []string{`method="Numeric again, other name"`, `method="Literal number"`} {
		if !strings.Contains(logText, want) {
			t.Errorf("log missing %s:\n%s", want, logText)
		}
	}
	if strings.Contains(logText, `method="Literal word"`) {
		t.Errorf("/users/me is not covered by a numeric @path.id:\n%s", logText)
	}
}

func TestSpecificRoutesBeforeCatchAllDoNotWarn(t *testing.T) {
	logText := shadowWarnings(t, "### User\nGET /api/users/:id\n\n{}\n\n### Me\nGET /users/me\n\n{}\n\n### Any user\nGET /users/:id\n\n{}\n\n### Fallback\nGET /api/*\n\n{}\n")
	if strings.Contains(logText, "route can never match") {
		t.Errorf("unexpected shadow warning:\n%s", logText)
	}
}
