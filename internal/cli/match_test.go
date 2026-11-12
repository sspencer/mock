package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const matchRoutes = `### Admin users
@query.role = admin
@header.X-Tenant = *
GET /users

admins

### Create user
@json.name = *
POST /users

created

### Metrics
GET http://localhost:9100/metrics

ok
`

func runMatchArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := runMatch(args, &out)
	return out.String(), err
}

func TestMatchCommand(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "api.http")
	if err := os.WriteFile(file, []byte(matchRoutes), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "body.json"), []byte(`{"name": "Ada"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Flags may come after the arguments.
	out, err := runMatchArgs(t, file, "get", "/users?role=admin", "-H", "X-Tenant: acme")
	if err != nil || !strings.Contains(out, `answered by "Admin users"`) || !strings.Contains(out, "status 200") {
		t.Errorf("admin users: %v\n%s", err, out)
	}
	out, err = runMatchArgs(t, file, "GET", "/users?role=guest")
	if ExitCode(err) != 1 || !strings.Contains(out, "no route matches") || !strings.Contains(out, "Query role") || !strings.Contains(out, "Header X-Tenant") {
		t.Errorf("guest: %v\n%s", err, out)
	}
	out, err = runMatchArgs(t, "-d", "@"+filepath.Join(dir, "body.json"), "-response", file, "POST", "/users")
	if err != nil || !strings.Contains(out, `"Create user"`) || !strings.Contains(out, "HTTP 200 OK") || !strings.HasSuffix(out, "created\n") {
		t.Errorf("create: %v\n%s", err, out)
	}
	// A route with a port matches requests that arrive on it, or that name
	// no port at all.
	for _, args := range [][]string{{file, "GET", "http://localhost:9100/metrics"}, {file, "GET", "/metrics"}, {"-port", "9100", file, "GET", "/metrics"}} {
		if out, err := runMatchArgs(t, args...); err != nil || !strings.Contains(out, `"Metrics"`) {
			t.Errorf("%v: %v\n%s", args, err, out)
		}
	}
	if out, err := runMatchArgs(t, "-port", "8080", file, "GET", "/metrics"); ExitCode(err) != 1 || !strings.Contains(out, "Served on port 9100") {
		t.Errorf("wrong port: %v\n%s", err, out)
	}

	out, err = runMatchArgs(t, "-json", dir, "GET", "/users?role=admin")
	var result matchResult
	if json.Unmarshal([]byte(out), &result) != nil || ExitCode(err) != 1 || result.Matched || len(result.Candidates) == 0 {
		t.Errorf("json: %v %+v\n%s", err, result, out)
	}
	for _, args := range [][]string{{}, {file}, {file, "GET"}, {"-nope", file, "GET", "/"}} {
		if _, err := runMatchArgs(t, args...); ExitCode(err) != 2 {
			t.Errorf("%v error = %v, want a usage error", args, err)
		}
	}
}
