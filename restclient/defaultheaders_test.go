package restclient

import (
	"net/http"
	"strings"
	"testing"
)

func TestDefaultHeaders(t *testing.T) {
	opts := Options{Headers: http.Header{"Content-Type": {"text/plain"}, "X-Served-By": {"mock"}, "Cache-Control": {"no-store"}}}
	methods, err := opts.Parse("test.http", strings.NewReader(`@default.header.content-type = application/json
@default.header.X-Request-Id = {{$uuid}}

### Users
GET /users

[]

### Page
GET /page
Content-Type: text/html
x-served-by: page

<p>hi</p>

### Plain
@default.header.Content-Type = text/csv
@default.header.Cache-Control = max-age=60
GET /report.csv

a,b

### Forwarded
@proxy = https://api.example.com
GET /api/*
`))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []map[string]string{
		{"X-Request-Id": "{{$uuid}}", "X-Served-By": "mock", "Cache-Control": "no-store"},
		{"Content-Type": "text/html", "X-Request-Id": "{{$uuid}}", "X-Served-By": "page", "Cache-Control": "no-store"},
		{"X-Request-Id": "{{$uuid}}", "X-Served-By": "mock", "Cache-Control": "max-age=60"},
	} {
		headers := methods[i].Headers
		if len(headers) != len(want) {
			t.Errorf("%s headers = %v, want %v", methods[i].Name, headers, want)
		}
		for name, value := range want {
			if got := headers.Values(name); len(got) != 1 || got[0] != value {
				t.Errorf("%s %s = %v, want %q", methods[i].Name, name, got, value)
			}
		}
	}
	for i, want := range []string{"application/json", "", "text/csv", ""} {
		if got := methods[i].DefaultContentType; got != want {
			t.Errorf("%s DefaultContentType = %q, want %q", methods[i].Name, got, want)
		}
	}
	if len(methods[3].Headers) != 0 {
		t.Errorf("@proxy route got default headers: %v", methods[3].Headers)
	}
	for _, method := range methods {
		for name := range method.Variables {
			if strings.HasPrefix(name, "default.") {
				t.Errorf("%s has variable %q", method.Name, name)
			}
		}
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	if IsDefaultVariable("default.header.Content-Type") {
		t.Error("an environment may not set @default.header.*")
	}

	for _, source := range []string{
		"@default.header.Bad Name = x\n### A\nGET /a\n",
		"@default.header. = x\n### A\nGET /a\n",
	} {
		if _, err := Parse("test.http", strings.NewReader(source)); err == nil || !strings.Contains(err.Error(), "test.http:1:") {
			t.Errorf("Parse(%q) error = %v", source, err)
		}
	}
}

func TestDefaultContentTypeFromOptions(t *testing.T) {
	opts := Options{Headers: http.Header{"Content-Type": {"text/plain"}, "X-Env": {"dev"}}}
	methods, err := opts.Parse("test.http", strings.NewReader("### A\nGET /a\n\n< a.html\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0]; got.DefaultContentType != "text/plain" || got.Headers.Get("Content-Type") != "" || got.Headers.Get("X-Env") != "dev" {
		t.Errorf("DefaultContentType = %q, headers = %v", got.DefaultContentType, got.Headers)
	}
}
