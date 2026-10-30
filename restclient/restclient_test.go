package restclient

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	input := `### [201] Create User
# creates a user
POST /users
Content-Type: application/json
X-Trace: yes

{
  "success": true
}

### [204] Delete User
DELETE /users/:id
`

	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("len(methods) = %d, want 2", len(methods))
	}

	create := methods[0]
	if create.Name != "Create User" || create.Method != http.MethodPost || create.Path != "/users" {
		t.Fatalf("parsed request = %#v", create)
	}
	if create.Status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", create.Status)
	}
	if got := create.Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	if !strings.Contains(create.Body, `"success": true`) {
		t.Fatalf("body = %q, want success payload", create.Body)
	}

	deleteMethod := methods[1]
	if deleteMethod.Method != http.MethodDelete || deleteMethod.Path != "/users/:id" || deleteMethod.Status != http.StatusNoContent {
		t.Fatalf("delete route = %#v", deleteMethod)
	}
}

func TestParseStatusInSectionName(t *testing.T) {
	input := `### [204] Delete
DELETE /users/:id

### [201] Create
post /users

### [202] Accepted
GET /accepted

### Default
GET /default

### [beta] Bracketed label
GET /beta

### [ 404 ]  Spaced status
GET /missing
`
	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []struct {
		name   string
		method string
		path   string
		status int
	}{
		{"Delete", http.MethodDelete, "/users/:id", 204},
		{"Create", http.MethodPost, "/users", 201},
		{"Accepted", http.MethodGet, "/accepted", 202},
		{"Default", http.MethodGet, "/default", 200},
		{"[beta] Bracketed label", http.MethodGet, "/beta", 200},
		{"Spaced status", http.MethodGet, "/missing", 404},
	}
	if len(methods) != len(want) {
		t.Fatalf("len(methods) = %d, want %d", len(methods), len(want))
	}
	for i, w := range want {
		m := methods[i]
		if m.Name != w.name || m.Method != w.method || m.Path != w.path || m.Status != w.status {
			t.Errorf("methods[%d] = %q %s %s status %d, want %q %s %s status %d",
				i, m.Name, m.Method, m.Path, m.Status, w.name, w.method, w.path, w.status)
		}
	}
}

func TestParseOnlyTreatsWholeCommentsAsVariables(t *testing.T) {
	input := `### User
# this mentions $delay=1s but is not a variable
@delay = 250ms
POST /users

created
`

	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(methods))
	}
	if got := methods[0].Variables["delay"]; got != "250ms" {
		t.Fatalf("delay variable = %q, want 250ms", got)
	}
	if len(methods[0].Variables) != 1 {
		t.Fatalf("variables = %#v, want only explicit delay variable", methods[0].Variables)
	}
}

func TestParseMatchHeadersFromDollarHeaderComments(t *testing.T) {
	input := `### Secured
@header.Authorization = Bearer secret
@header.X-Trace = *
GET /secure
Content-Type: application/json

{"ok":true}
`
	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(methods))
	}
	if got := methods[0].MatchHeaders.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("Authorization match = %q, want Bearer secret", got)
	}
	if got := methods[0].MatchHeaders.Get("X-Trace"); got != "*" {
		t.Fatalf("X-Trace match = %q, want *", got)
	}
	if got := methods[0].Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("response Content-Type = %q", got)
	}
}

func TestFileDependencies(t *testing.T) {
	methods := []Method{
		{File: "users.json"},
		{File: "users.json"},
		{File: "index.html"},
		{},
	}
	deps := FileDependencies(methods)
	if len(deps) != 2 {
		t.Fatalf("deps = %#v, want 2 unique paths", deps)
	}
}

func TestParseExternalBodyFile(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("### Spaced\nPOST /a\nContent-Type: application/xml\n\n<   my dir/demo file.xml \n\n### Inline\nPOST /b\n\n<not a file\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := methods[0].File, "  my dir/demo file.xml "; got != want {
		t.Errorf("File = %q, want %q (whitespace preserved)", got, want)
	}
	if methods[0].Body != "" {
		t.Errorf("Body = %q, want empty for an external body", methods[0].Body)
	}
	if methods[1].File != "" || methods[1].Body != "<not a file" {
		t.Errorf("inline body = %q, file = %q; only \"< \" starts a file reference", methods[1].Body, methods[1].File)
	}
}

func TestRejectInvalidExternalBody(t *testing.T) {
	for input, want := range map[string]string{
		"### r\nGET /x\n\n< \n":             `"< " must be followed by a file path`,
		"### r\nGET /x\n\n< a.json\nmore\n": "must be only the file path line",
		"### r\n@file = a.json\nGET /x\n":   `start the response body with "< path"`,
		"### r\n# $file=a.json\nGET /x\n":   `start the response body with "< path"`,
	} {
		_, err := Parse("test.http", strings.NewReader(input))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want %q", input, err, want)
		}
	}
}

func TestRejectUnsafeExternalBodyPaths(t *testing.T) {
	for path, want := range map[string]string{
		"/etc/passwd":    "must be relative",
		"../secret.json": "cannot contain ..",
		"a/../../b.json": "cannot contain ..",
		"a/./..":         "cannot contain ..",
		"./":             "must name a file",
	} {
		_, err := Parse("test.http", strings.NewReader("### r\nGET /x\n\n< "+path+"\n"))
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "test.http:4:") {
			t.Errorf("Parse(< %s) error = %v, want %q at line 4", path, err, want)
		}
	}
	if _, err := Parse("test.http", strings.NewReader("### r\nGET /x\n\n< sub/dir/a..b.json\n")); err != nil {
		t.Errorf("dots inside a name rejected: %v", err)
	}
}

func TestResolveFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "requests"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	source := filepath.Join(dir, "requests", "a.http")
	// Neither exists: the path next to the .http file is reported.
	if got, want := ResolveFile(source, "body.json"), filepath.Join(dir, "requests", "body.json"); got != want {
		t.Errorf("missing = %q, want %q", got, want)
	}
	// Only the workspace-root copy exists.
	if err := os.WriteFile("body.json", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := ResolveFile(source, "body.json"), filepath.Join(dir, "body.json"); got != want {
		t.Errorf("workspace root = %q, want %q", got, want)
	}
	// The copy next to the .http file wins.
	if err := os.WriteFile(filepath.Join(dir, "requests", "body.json"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := ResolveFile(source, "body.json"), filepath.Join(dir, "requests", "body.json"); got != want {
		t.Errorf("next to http file = %q, want %q", got, want)
	}
}

func TestParseRejectsRequestLinesWithExtraTokens(t *testing.T) {
	input := `### User
GET /users HTTP/1.1
`

	_, err := Parse("test.http", strings.NewReader(input))
	if err == nil {
		t.Fatal("Parse() error = nil, want error")
	}
	msg := err.Error()
	for _, want := range []string{"test.http:2:", "invalid HTTP request line", "METHOD /path"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want to contain %q", msg, want)
		}
	}
}

func TestParseRejectsUnknownHTTPMethods(t *testing.T) {
	input := `### Broken
DOPLGET /users/1

ok
`
	_, err := Parse("debug.http", strings.NewReader(input))
	if err == nil {
		t.Fatal("Parse() error = nil, want error")
	}
	msg := err.Error()
	for _, want := range []string{"debug.http:2:", "unrecognized HTTP method", "DOPLGET", "GET, POST"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want to contain %q", msg, want)
		}
	}
}

func TestUnusedVariables(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`@shared = hi
@orphan = nobody

### Used
@greeting = hello
@unused = 1000ms
GET /hi
X-Greeting: {{greeting}}

{{greeting}} {{shared}}

### Overrides
@shared = bye
GET /bye

{{shared}}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var got []string
	for _, unused := range UnusedVariables(methods) {
		got = append(got, fmt.Sprintf("%s:%d", unused.Name, unused.Line))
	}
	if want := []string{"orphan:2", "unused:6"}; !slices.Equal(got, want) {
		t.Fatalf("UnusedVariables() = %v, want %v", got, want)
	}

	// Control variables and variables referenced by other variables are not reported.
	methods, err = Parse("test.http", strings.NewReader(`### Ok
@delay = 1s
@name = Ada
@greeting = hi {{name}}
GET /hi

{{greeting}}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := UnusedVariables(methods); len(got) != 0 {
		t.Fatalf("UnusedVariables() = %#v, want none", got)
	}
}

func TestFullURLRequestTarget(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`@host = https://api.example.com
@token = abc123

### Get all users
GET {{host}}/users?page=2
Authorization: Bearer {{token}}

### Create a user
@host = http://localhost:9000/ignored
POST {{host}}/users

### Literal URL
GET https://api.example.com/users/42

### Bare host
GET {{host}}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	want := []struct{ method, path, page string }{
		{"GET", "/users", "2"},
		{"POST", "/ignored/users", ""},
		{"GET", "/users/42", ""},
		{"GET", "/", ""},
	}
	if len(methods) != len(want) {
		t.Fatalf("got %d methods, want %d", len(methods), len(want))
	}
	for i, w := range want {
		m := methods[i]
		if m.Method != w.method || m.Path != w.path || m.Query.Get("page") != w.page {
			t.Errorf("methods[%d] = %s %s ?page=%q, want %s %s ?page=%q",
				i, m.Method, m.Path, m.Query.Get("page"), w.method, w.path, w.page)
		}
	}
	if methods[0].Target != "{{host}}/users?page=2" {
		t.Errorf("Target = %q, want the unexpanded request target", methods[0].Target)
	}
	// host is referenced only from request targets; token only from a header.
	if got := UnusedVariables(methods); len(got) != 0 {
		t.Errorf("UnusedVariables() = %#v, want none", got)
	}
}

func TestFileAndMethodScopedVariables(t *testing.T) {
	input := `@host = example.com
@delay = 100ms
@header.X-Tenant = {{host}}

### Inherits
GET /a

### Variables-only block
@token = abc

### Overrides
@delay = 2s
@host = other.test
@header.x-tenant = {{host}}
GET /b

{{host}} {{token}}
`
	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("len(methods) = %d, want 2 (variables-only block is not a route)", len(methods))
	}
	inherits, overrides := methods[0], methods[1]
	if inherits.Delay != 100*time.Millisecond || overrides.Delay != 2*time.Second {
		t.Fatalf("delays = %v, %v", inherits.Delay, overrides.Delay)
	}
	if got := inherits.MatchHeaders.Get("X-Tenant"); got != "example.com" {
		t.Fatalf("inherited X-Tenant = %q", got)
	}
	if got := overrides.MatchHeaders["X-Tenant"]; !slices.Equal(got, []string{"other.test"}) {
		t.Fatalf("overridden X-Tenant = %q", got)
	}
	if got := inherits.Variables["token"]; got != "abc" {
		t.Fatalf("file variable from a later variables-only block = %q, want abc", got)
	}
	if got, _ := overrides.Variable("host"); got != "other.test" {
		t.Fatalf("host = %q", got)
	}
}

func TestVariableDefinitionSyntax(t *testing.T) {
	input := "@  spaced.name-1  =   a b  c  \n" +
		`@escaped = one\ntwo\tthree\\n` + "\n" +
		"@base = http://x\n" +
		"@url = {{base}}/a b\n\n" +
		"### r\nGET /x\n"
	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	vars := methods[0].Variables
	if got := vars["spaced.name-1"]; got != "a b  c" {
		t.Errorf("trimmed value = %q", got)
	}
	if got, want := vars["escaped"], "one\ntwo\tthree\\n"; got != want {
		t.Errorf("escaped value = %q, want %q", got, want)
	}
	if got, _ := methods[0].Variable("url"); got != "http://x/a b" {
		t.Errorf("referenced value = %q", got)
	}
}

func TestRejectInvalidVariableDefinitions(t *testing.T) {
	for input, want := range map[string]string{
		"@no-equals\n### r\nGET /x":                   `expected "@name = value"`,
		"@two words = 1\n### r\nGET /x":               "cannot contain spaces",
		"@ = 1\n### r\nGET /x":                        "variable name is required",
		"@1bad = 1\n### r\nGET /x":                    "invalid variable name",
		"### r\n@status = 201\nGET /x":                "@status is not supported",
		"### r\n# $delay=1s\nGET /x":                  `use "@delay = 1s"`,
		"# comment before any section\n### r\nGET /x": "content before first ### section",
	} {
		_, err := Parse("test.http", strings.NewReader(input))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want %q", input, err, want)
		}
	}
}

func TestExpand(t *testing.T) {
	vars := map[string]string{"a": "x y&z", "b": "[{{a}}]", "loop": "{{loop}}", "enc": "{{%a}}"}
	dollar := func(name string, args []string) (string, bool) {
		return strings.ToUpper(name) + strings.Join(args, ","), name != "missing"
	}
	for input, want := range map[string]string{
		"{{a}}":              "x y&z",
		"{{%a}}":             "x%20y%26z",
		"{{b}}":              "[x y&z]",
		"{{enc}}":            "x%20y%26z",
		"{{%b}}":             "%5Bx%20y%26z%5D",
		"{{loop}}":           "{{loop}}",
		"{{nope}} {{$nope}}": "{{nope}} NOPE",
		"{{$missing}}":       "{{$missing}}",
		"{{ a }} {a} {{a}":   "{{ a }} {a} {{a}",
		"{{$fn 1  -2 d }}":   "FN1,-2,d",
		"{{a extra}}":        "{{a extra}}",
	} {
		if got := Expand(input, vars, dollar); got != want {
			t.Errorf("Expand(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name: "content before first section",
			input: `GET /users
`,
			want: []string{"test.http:1:", "content before first ###", "GET /users"},
		},
		{
			name: "missing method name",
			input: `###
GET /users
`,
			want: []string{"test.http:1:", "method name is required after ###"},
		},
		{
			name: "missing request line",
			input: `### User
# comment only
`,
			want: []string{"test.http:2:", `section "User" is missing an HTTP request line`, "METHOD /path"},
		},
		{
			name: "missing request line empty section",
			input: `### User
`,
			want: []string{"test.http:1:", `section "User" is missing an HTTP request line`},
		},
		{
			name: "incomplete request line",
			input: `### User
GET
`,
			want: []string{"test.http:2:", "incomplete HTTP request line", `"GET"`},
		},
		{
			name: "invalid request target",
			input: `### User
GET http://[::1
`,
			want: []string{"test.http:2:", `section "User" has an invalid request target`, "path like"},
		},
		{
			name: "invalid header line",
			input: `### User
GET /users
Content-Type application/json
`,
			want: []string{
				"test.http:3:",
				`section "User" has an invalid response header line`,
				"Content-Type application/json",
				"Name: value",
			},
		},
		{
			name: "empty header name",
			input: `### User
GET /users
: missing-name
`,
			want: []string{"test.http:3:", "header name is required"},
		},
		{
			name: "empty header matcher name",
			input: `### User
@header. = value
GET /users
`,
			want: []string{"test.http:2:", "@header. requires a header name"},
		},
		{
			name: "status below final range",
			input: `### [100] User
GET /users
`,
			want: []string{"test.http:1:", "invalid response status [100]"},
		},
		{
			name: "status above final range",
			input: `### Good
GET /ok

### [1000] User
GET /users
`,
			want: []string{"test.http:4:", "invalid response status [1000]"},
		},
		{
			name: "status without name",
			input: `### [204]
GET /users
`,
			want: []string{"test.http:1:", "method name is required after the status"},
		},
		{
			name: "error line in later section",
			input: `### Good
GET /ok

ok

### Bad
POST /users HTTP/1.1
`,
			want: []string{"test.http:7:", `section "Bad" has an invalid HTTP request line`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("test.http", strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			msg := err.Error()
			for _, want := range tt.want {
				if !strings.Contains(msg, want) {
					t.Fatalf("error = %q, want to contain %q", msg, want)
				}
			}
		})
	}
}

func TestParseAllowsBlankLinesBeforeFirstSection(t *testing.T) {
	input := `

### User
GET /users

ok
`
	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(methods) != 1 || methods[0].Path != "/users" {
		t.Fatalf("methods = %#v", methods)
	}
}
