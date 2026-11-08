package mockhttp

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestEachBlocks(t *testing.T) {
	s := newTestServer(t, `### Batch
POST /batch
Content-Type: application/json

{"results":[
  {{#each $json.items}}
  {"index":{{$index}},"sku":"{{$item.sku | upper}}","qty":{{$item.qty | default 1}},"tags":[{{#each $item.tags}}"{{$item}}"{{/each}}]}
  {{/each}}
]}

### Names
POST /names

{{#each $json.people.*.name}}{{$item}};{{/each}}
`)
	tests := []struct{ target, body, want string }{
		{"/batch", `{"items":[{"sku":"a1","qty":2,"tags":["x","y"]},{"sku":"b2"}]}`,
			"{\"results\":[\n  {\"index\":0,\"sku\":\"A1\",\"qty\":2,\"tags\":[\"x\",\"y\"]},\n  {\"index\":1,\"sku\":\"B2\",\"qty\":1,\"tags\":[]}\n]}"},
		{"/batch", `{"items":[]}`, "{\"results\":[\n]}"},
		{"/batch", `not json`, "{\"results\":[\n]}"},
		{"/batch", ``, "{\"results\":[\n]}"},
		{"/names", `{"people":[{"name":"Ada"},{"name":"Bob"},{}]}`, "Ada;Bob;"},
	}
	for _, tt := range tests {
		if _, got := serveBody(t, s, tt.target, tt.body); got != tt.want {
			t.Errorf("%s %s:\n got %q\nwant %q", tt.target, tt.body, got, tt.want)
		}
	}
}

func TestEachOverFixture(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.json"), `{"users":[{"id":1,"name":"Ada"},{"id":2,"name":"Bob"}]}`)
	source := filepath.Join(dir, "api.http")
	writeFile(t, source, "### Users\n@fixture = data.json\nPOST /users\nContent-Type: application/json\n\n[{{#each $fixture.users}}{\"id\":{{$item.id}},\"name\":\"{{$item.name}}\"}{{/each}}]\n")
	methods, err := restclient.Load([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s := New(methods, slog.New(slog.NewTextHandler(&logs, nil)))
	if _, got := serveBody(t, s, "/users", ""); got != `[{"id":1,"name":"Ada"},{"id":2,"name":"Bob"}]` {
		t.Errorf("got %q", got)
	}
	if strings.Contains(logs.String(), "WARN") {
		t.Errorf("unexpected warnings: %s", logs.String())
	}
}

func TestEachTemplateProblems(t *testing.T) {
	for input, want := range map[string]string{
		"{{#each items}}x{{/each}}":          "{{#each}} needs a JSON array",
		"{{#each $query.a}}x{{/each}}":       "{{#each}} needs a JSON array",
		"{{#each $json.items}}x":             "{{#each}} has no matching {{/each}}",
		"{{#each $json.items}}x{{/repeat}}":  "{{/repeat}} has no matching {{#repeat}}",
		"{{#repeat 2}}x{{/each}}{{/repeat}}": "{{/each}} has no matching {{#each}}",
	} {
		_, problems := parseTemplate(input)
		found := false
		for _, p := range problems {
			found = found || strings.Contains(p.message, want)
		}
		if !found {
			t.Errorf("parseTemplate(%q) problems = %+v, want %q", input, problems, want)
		}
	}
}
