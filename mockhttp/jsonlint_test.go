package mockhttp

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestWarnInvalidJSONBody(t *testing.T) {
	tests := []struct {
		name, source, want string
	}{
		{"trailing comma", "### [200] Bad\nGET /a\nContent-Type: application/json\n\n{\"a\": 1,\n \"b\": 2,\n}\n", "line 3 of the rendered body"},
		{"unquoted literal", "### Bad\nGET /a\nContent-Type: application/json\n\n{\"a\": yes}\n", "invalid character 'y'"},
		{"ends early", "### Bad\nGET /a\nContent-Type: application/vnd.api+json\n\n{\"a\": [1, 2\n", "unexpected end of JSON input"},
		{"template off", "### Bad\n@template = off\nGET /a\nContent-Type: application/json\n\n{\"a\": {{$id}}}\n", "invalid character '{'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logText := shadowWarnings(t, tt.source)
			if !strings.Contains(logText, "response body is not valid JSON") || !strings.Contains(logText, tt.want) {
				t.Fatalf("warnings = %q, want invalid JSON with %q", logText, tt.want)
			}
			if !strings.Contains(logText, "source=routes.http line=1") {
				t.Errorf("warning should name the route's source and line: %q", logText)
			}
		})
	}
}

func TestWarnInvalidJSONBodyAcceptsTemplates(t *testing.T) {
	sources := []string{
		// Request values are sampled, quoted or not.
		"### User\nGET /users/:id\nContent-Type: application/json\n\n{\"id\": {{$id}}, \"q\": \"{{$query.q}}\", \"n\": {{$json.n}}, \"e\": \"{{$env.MOCK_LINT_UNSET}}\"}\n",
		// Repeat copies get commas, generated values render.
		"### List\nGET /list\nContent-Type: application/json\n\n[{{#repeat 1 3}}{\"id\": \"{{$uuid}}\", \"i\": {{$index}}}{{/repeat}}]\n",
		// Defaults and variables expand.
		"### Page\n@who = \"quoted\" name\nGET /page\nContent-Type: application/json\n\n{\"page\": {{$query.page | default 1}}, \"who\": \"{{who}}\"}\n",
		// Not JSON, empty, or no body allowed: not checked.
		"### Text\nGET /t\nContent-Type: text/plain\n\n{oops\n",
		"### Empty\nGET /e\nContent-Type: application/json\n\n",
		"### [204] None\nGET /n\nContent-Type: application/json\n\n{oops\n",
	}
	for _, source := range sources {
		if logText := shadowWarnings(t, source); strings.Contains(logText, "not valid JSON") {
			t.Errorf("unexpected warning for\n%s\n%s", source, logText)
		}
	}
}

func TestWarnInvalidJSONBodyFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("good.json", `{"id": {{$id}}}`)
	write("bad.json", "{\n  \"id\": 1,\n}\n")
	write("routes.http", "### Good\nGET /good/:id\n\n< good.json\n\n### Bad\nGET /bad\n\n< bad.json\n\n### Dynamic\nGET /dyn/:name\n\n< {{$name}}.json\n")

	logText := loadWarnings(t, filepath.Join(dir, "routes.http"))
	if strings.Count(logText, "not valid JSON") != 1 || !strings.Contains(logText, "file=bad.json") || !strings.Contains(logText, "line 3 of the rendered body") {
		t.Fatalf("warnings = %q, want one invalid JSON warning for bad.json", logText)
	}
}

func loadWarnings(t *testing.T, path string) string {
	t.Helper()
	methods, err := restclient.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	New(methods, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return buf.String()
}
