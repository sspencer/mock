package restclient

import (
	"strings"
	"testing"
	"time"
)

func TestParseStatusDirectives(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`@status = 202

### [{{$query.status | default 200}}] Echo
GET /echo

### Accepted
GET /a

### [204] Named
GET /n

### Templated
@status = {{$json.code}}
POST /t
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		status   int
		template string
		name     string
	}{
		{200, "{{$query.status | default 200}}", "Echo"},
		{202, "", "Accepted"},
		{204, "", "Named"},
		{200, "{{$json.code}}", "Templated"},
	}
	for i, w := range want {
		if got := methods[i]; got.Status != w.status || got.StatusTemplate != w.template || got.Name != w.name {
			t.Errorf("%d: status %d, template %q, name %q; want %+v", i, got.Status, got.StatusTemplate, got.Name, w)
		}
	}
	for source, want := range map[string]string{
		"### [201] Both\n@status = 202\nGET /x\n": "sets a status in its name and with @status",
		"### Low\n@status = 199\nGET /x\n":        "@status must be a status between 200 and 999",
	} {
		if _, err := Parse("test.http", strings.NewReader(source)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q", err, want)
		}
	}
}

func TestParseRequireMissingETag(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`### A
@require = $fixture.users[id=$path.id]
@missing = next
@etag = auto
GET /a/:id

### B
@require = {{ $json.user.id }}
@missing = 410
GET /b
`))
	if err != nil {
		t.Fatal(err)
	}
	a, b := methods[0], methods[1]
	if a.Require != "fixture.users[id=$path.id]" || !a.MissingNext || a.Missing != 0 || !a.ETag {
		t.Errorf("A = require %q, missing %d/%v, etag %v", a.Require, a.Missing, a.MissingNext, a.ETag)
	}
	if b.Require != "json.user.id" || b.MissingNext || b.Missing != 410 || b.ETag {
		t.Errorf("B = require %q, missing %d/%v, etag %v", b.Require, b.Missing, b.MissingNext, b.ETag)
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	for _, bad := range []string{"@require = users", "@require = $a $b", "@missing = later", "@missing = 99", "@etag = strong", "@json.items[sku=].qty = 1"} {
		if _, err := Parse("test.http", strings.NewReader("### X\n"+bad+"\nGET /x\n")); err == nil || !strings.Contains(err.Error(), "test.http:2:") {
			t.Errorf("%q error = %v", bad, err)
		}
	}
	methods, err = Parse("test.http", strings.NewReader("### J\n@json.items[sku=A1].qty = 2\nPOST /j\n"))
	if err != nil || methods[0].MatchJSON["items[sku=A1].qty"] != "2" {
		t.Errorf("selector matcher = %v, %v", methods[0].MatchJSON, err)
	}
}

func TestParseJSONPathSelectors(t *testing.T) {
	segments, err := ParseJSONPath("users[id=42].name")
	if err != nil || len(segments) != 3 || !segments[1].Select || segments[1].Name != "id" || segments[1].Value != "42" || segments[2].Name != "name" {
		t.Fatalf("segments = %+v, %v", segments, err)
	}
	if segments, err := ParseJSONPath(`[id=$path.id].tags.["a.b"]`); err != nil || len(segments) != 3 || segments[0].Value != "$path.id" || !segments[2].Quoted {
		t.Errorf("segments = %+v, %v", segments, err)
	}
	for _, bad := range []string{"users[id]", "users[=1]", "users[*=1]", "users[id=]", "users[id=a b]", "users[id=1"} {
		if _, err := ParseJSONPath(bad); err == nil {
			t.Errorf("ParseJSONPath(%q) error = nil", bad)
		}
	}
}

func TestParseNumberedCallbacks(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`@callback.retries = 2

### Pay
@callback.2 = POST https://hooks.example.com/succeeded
@callback.2.delay = 1s
@callback.2.retries = 5
@callback = POST https://hooks.example.com/pending
@callback.1 = POST https://hooks.example.com/processing
@callback.1.header.X-Step = 1
POST /pay
`))
	if err != nil {
		t.Fatal(err)
	}
	callbacks := methods[0].Callbacks
	if len(callbacks) != 3 {
		t.Fatalf("callbacks = %+v", callbacks)
	}
	want := []struct {
		index, retries int
		url            string
		delay          time.Duration
	}{
		{0, 2, "https://hooks.example.com/pending", 0},
		{1, 2, "https://hooks.example.com/processing", 0},
		{2, 5, "https://hooks.example.com/succeeded", time.Second},
	}
	for i, w := range want {
		c := callbacks[i]
		if c.Index != w.index || c.Retries != w.retries || c.URL != w.url || c.Delay != w.delay {
			t.Errorf("callback %d = %+v, want %+v", i, c, w)
		}
	}
	if callbacks[1].Headers.Get("X-Step") != "1" {
		t.Errorf("callback 1 headers = %v", callbacks[1].Headers)
	}
	if _, err := Parse("test.http", strings.NewReader("### X\n@callback.3.body = {}\nPOST /x\n")); err == nil || !strings.Contains(err.Error(), `"@callback.3 = METHOD URL"`) {
		t.Errorf("orphan @callback.3.body error = %v", err)
	}
}
