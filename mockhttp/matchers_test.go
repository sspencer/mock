package mockhttp

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

const matcherFixture = `### Authorized
@header.Authorization = ~Bearer [A-Za-z0-9.]+
GET /me

alice

### Unauthorized
@header.Authorization = !
GET /me

anonymous

### Numeric user
@path.id = ~[0-9]+
GET /users/:id

user {{$id}}

### Bad user id
GET /users/:id

bad id

### Search
@query.q = *
GET /search

results

### Search form
@query.q = !
GET /search

form

### Page
@query.page = ~[1-9][0-9]*
GET /items

page {{$page}}
`

func serveMatcher(t *testing.T, s *Server, method, target string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func TestAbsentAndRegexpMatchers(t *testing.T) {
	methods, err := restclient.Parse("matchers.http", strings.NewReader(matcherFixture))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, nil)
	tests := []struct {
		target  string
		headers map[string]string
		code    int
		body    string
	}{
		{"/me", map[string]string{"Authorization": "Bearer abc.def"}, 200, "alice"},
		{"/me", nil, 200, "anonymous"},
		{"/me", map[string]string{"Authorization": "Basic xyz"}, 404, ""},
		{"/users/42", nil, 200, "user 42"},
		{"/users/42", nil, 200, "user 42"}, // the earlier, more specific route wins every time
		{"/users/abc", nil, 200, "bad id"},
		{"/search?q=cats", nil, 200, "results"},
		{"/search", nil, 200, "form"},
		{"/search?q=", nil, 200, "form"},
		{"/items?page=3", nil, 200, "page 3"},
		{"/items?page=0", nil, 404, ""},
		{"/items", nil, 404, ""},
	}
	for _, tt := range tests {
		code, body := serveMatcher(t, s, "GET", tt.target, tt.headers)
		if code != tt.code || (tt.body != "" && body != tt.body) {
			t.Errorf("GET %s %v = %d %q, want %d %q", tt.target, tt.headers, code, body, tt.code, tt.body)
		}
	}
}

func TestFirstDeclaredRouteWins(t *testing.T) {
	input := "### Any\nGET /users/:id\n\nany\n\n### Numeric\n@path.id = ~[0-9]+\nGET /users/:id\n\nnumeric\n"
	methods, err := restclient.Parse("matchers.http", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, nil)
	for _, target := range []string{"/users/1", "/users/1", "/users/abc"} {
		if _, body := serveMatcher(t, s, "GET", target, nil); body != "any" {
			t.Fatalf("%s = %q, want the earlier catch-all", target, body)
		}
	}
}

func TestMatcherMismatchReasons(t *testing.T) {
	input := `### Constrained
@header.Authorization = ~Bearer .+
@header.X-Debug = !
@query.page = ~[0-9]+
@path.id = ~[0-9]+
GET /users/:id

ok
`
	methods, err := restclient.Parse("matchers.http", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, nil)
	code, _ := serveMatcher(t, s, "GET", "/users/abc?page=x", map[string]string{"Authorization": "Basic secret", "X-Debug": "1"})
	if code != 404 {
		t.Fatalf("code = %d, want 404", code)
	}
	reasons := strings.Join(s.events[0].Match.Candidates[0].Reasons, "\n")
	for _, want := range []string{
		`Query page: does not match pattern "[0-9]+"; received ["x"]`,
		`Path parameter id: does not match pattern "[0-9]+"; received ["abc"]`,
		`Header Authorization: does not match pattern "Bearer .+"`,
		`Header X-Debug: must be absent`,
	} {
		if !strings.Contains(reasons, want) {
			t.Errorf("reasons missing %q:\n%s", want, reasons)
		}
	}
	if strings.Contains(reasons, "secret") {
		t.Fatal("request header value repeated in diagnostic")
	}
}

func TestNegatedMatchersRouteRequests(t *testing.T) {
	s := newTestServer(t, `### Not a guest
@header.X-Role = !guest
GET /me

member

### Not a test order
@json.id = !~test-.*
POST /orders

real

### Fallback
GET /me

fallback
`)
	for _, tt := range []struct{ role, want string }{{"admin", "member"}, {"guest", "fallback"}, {"", "fallback"}} {
		req := httptest.NewRequest("GET", "/me", nil)
		if tt.role != "" {
			req.Header.Set("X-Role", tt.role)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if got := strings.TrimSpace(rec.Body.String()); got != tt.want {
			t.Errorf("X-Role %q: body = %q, want %q", tt.role, got, tt.want)
		}
	}
	if code, body := serveBody(t, s, "/orders", `{"id":"o-1"}`); code != 200 || body != "real" {
		t.Errorf("real order: %d %q", code, body)
	}
	if code, _ := serveBody(t, s, "/orders", `{"id":"test-1"}`); code != 404 {
		t.Errorf("test order: status %d, want 404", code)
	}
	reason := matcherReason(restclient.MustMatcher("!~test-.*"), []string{"test-1"}, true)
	if !strings.Contains(reason, `must not match pattern "test-.*"`) {
		t.Errorf("reason = %q", reason)
	}
	if reason := matcherReason(restclient.MustMatcher("!guest"), nil, true); reason != "required value is missing" {
		t.Errorf("missing reason = %q", reason)
	}
}
