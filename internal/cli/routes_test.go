package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sspencer/mock/mockhttp"
)

const routesFile = `### Admin users
@tag = admin
@query.role = admin
@header.X-Tenant = *
GET /users?active=1

admins

### [201] Create user
@json = {"kind": "person"}
@json.name = *
POST /users

created

### Metrics
@tag = ops
GET http://localhost:9100/metrics

ok

### Forward
@proxy = http://example.test
GET /legacy/*
`

func TestRoutesCommand(t *testing.T) {
	t.Setenv("MOCK_PORT", "")
	file := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(file, []byte(routesFile), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := runRoutes([]string{file}, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 || !strings.HasPrefix(lines[0], "METHOD") {
		t.Fatalf("output:\n%s", out.String())
	}
	for i, want := range []string{
		`^GET +/users\?active=1 +8080 +200 +Admin users +.*api.http:1 +admin +header X-Tenant: \*; query role: admin$`,
		`^POST +/users +8080 +201 +Create user .*json: \{"kind":"person"\}; json name: \*$`,
		`^GET +/metrics +9100 +200 +Metrics .*ops`,
		`^GET +/legacy/\* +8080 +proxy +Forward `,
	} {
		if !matchesLine(lines[i+1], want) {
			t.Errorf("line %d = %q, want %s", i+1, lines[i+1], want)
		}
	}

	out.Reset()
	if err := runRoutes([]string{"-json", "-port", "9000", "-skip", "ops", file}, &out); err != nil {
		t.Fatal(err)
	}
	var routes []mockhttp.RouteInfo
	if err := json.Unmarshal([]byte(out.String()), &routes); err != nil {
		t.Fatal(err)
	}
	if len(routes) != 3 || routes[0].Port != 9000 || routes[1].MatchJSONBody != `{"kind":"person"}` {
		t.Fatalf("routes = %+v", routes)
	}

	if err := runRoutes([]string{"-only", "nothing", file}, &out); err == nil || ExitCode(err) != 1 {
		t.Fatalf("-only nothing: err = %v", err)
	}
	if err := runRoutes(nil, &out); ExitCode(err) != 2 {
		t.Fatalf("no files: err = %v", err)
	}
}

func matchesLine(line, pattern string) bool {
	return regexp.MustCompile(pattern).MatchString(strings.TrimRight(line, " "))
}
