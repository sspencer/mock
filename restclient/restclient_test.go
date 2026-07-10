package restclient

import (
	"net/http"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	input := `### Create User
# creates a user
# $status=201
POST /users
Content-Type: application/json
X-Trace: yes

{
  "success": true
}

### Delete User
# $status=204
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
	if got := create.Variables["status"]; got != "201" {
		t.Fatalf("status variable = %q, want 201", got)
	}
	if got := create.Headers.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	if !strings.Contains(create.Body, `"success": true`) {
		t.Fatalf("body = %q, want success payload", create.Body)
	}

	deleteMethod := methods[1]
	if deleteMethod.Method != http.MethodDelete || deleteMethod.Path != "/users/:id" {
		t.Fatalf("delete route = %#v", deleteMethod)
	}
}

func TestParseOnlyTreatsWholeCommentsAsVariables(t *testing.T) {
	input := `### User
# this mentions $status=500 but is not a variable
# $status=201
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
	if got := methods[0].Variables["status"]; got != "201" {
		t.Fatalf("status variable = %q, want 201", got)
	}
	if len(methods[0].Variables) != 1 {
		t.Fatalf("variables = %#v, want only explicit status variable", methods[0].Variables)
	}
}

func TestParseMatchHeadersFromDollarHeaderComments(t *testing.T) {
	input := `### Secured
# $header.Authorization=Bearer secret
# $header.X-Trace=*
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
		{Variables: map[string]string{"file": "users.json"}},
		{Variables: map[string]string{"file": "users.json"}},
		{Variables: map[string]string{"file": "index.html"}},
		{Variables: map[string]string{}},
	}
	deps := FileDependencies(methods)
	if len(deps) != 2 {
		t.Fatalf("deps = %#v, want 2 unique paths", deps)
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
	if !strings.Contains(err.Error(), "invalid HTTP request line") {
		t.Fatalf("error = %q, want invalid request line", err)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "content before first section",
			input: `GET /users
`,
			want: "content before first ###",
		},
		{
			name: "missing method name",
			input: `###
GET /users
`,
			want: "method name is required after ###",
		},
		{
			name: "missing request line",
			input: `### User
# comment only
`,
			want: "missing an HTTP request line",
		},
		{
			name: "invalid request target",
			input: `### User
GET http://[::1
`,
			want: "invalid request target",
		},
		{
			name: "invalid header line",
			input: `### User
GET /users
Content-Type application/json
`,
			want: "invalid header line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("test.http", strings.NewReader(tt.input))
			if err == nil {
				t.Fatal("Parse() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want to contain %q", err, tt.want)
			}
		})
	}
}
