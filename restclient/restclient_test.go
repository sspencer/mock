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
