package mocktest

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const api = `### [201] Create user
POST /users
Content-Type: application/json

{"id":1,"name":"{{name}}"}

### [200] Get user
GET http://localhost:9999/users/:id

{"id":{{$id}}}

@name = Avery
`

// recorder captures assertion failures without failing the real test.
type recorder struct {
	testing.TB
	errors []string
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func send(t *testing.T, method, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp
}

func TestStartSourceServesAndAsserts(t *testing.T) {
	srv := StartSource(t, api)
	if resp := send(t, "POST", srv.URL+"/users"); resp.StatusCode != 201 {
		t.Fatalf("POST /users = %d, want 201", resp.StatusCode)
	}
	// The route names port 9999, but every route answers on the test port.
	if resp := send(t, "GET", srv.URL+"/users/7?expand=1"); resp.StatusCode != 200 {
		t.Fatalf("GET /users/7 = %d, want 200", resp.StatusCode)
	}
	send(t, "GET", srv.URL+"/users/8")

	srv.AssertCalled(t, "POST /users", 1)
	srv.AssertCalled(t, "GET /users/7", 1)
	srv.AssertCalled(t, "GET /users/7?expand=1", 1)
	srv.AssertCalled(t, "GET /users/7?expand=2", 0)
	srv.AssertCalled(t, "Get user", 2)
	srv.AssertNotCalled(t, "DELETE /users/7")
	srv.AssertAllMatched(t)

	srv.Clear()
	srv.AssertCalled(t, "Get user", 0)
}

func TestAssertionFailuresDescribeTraffic(t *testing.T) {
	srv := StartSource(t, api)
	send(t, "POST", srv.URL+"/users")
	send(t, "PUT", srv.URL+"/nowhere")

	rec := &recorder{TB: t}
	srv.AssertCalled(rec, "POST /users", 2)
	srv.AssertNotCalled(rec, "POST /users")
	srv.AssertAllMatched(rec)
	if len(rec.errors) != 3 {
		t.Fatalf("failures = %d, want 3: %q", len(rec.errors), rec.errors)
	}
	for _, want := range []string{"POST /users called 1 time, want 2", `POST /users -> 201 ("Create user")`, "PUT /nowhere -> 404 (no route)"} {
		if !strings.Contains(rec.errors[0], want) {
			t.Errorf("AssertCalled message missing %q:\n%s", want, rec.errors[0])
		}
	}
	if !strings.Contains(rec.errors[2], "1 request matched no route:\n  PUT /nowhere") {
		t.Errorf("AssertAllMatched message = %q", rec.errors[2])
	}
}

func TestStartFilesWithVars(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(path, []byte(api), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := StartWith(t, Options{Files: []string{path}, Vars: map[string]string{"name": "Sam"}})
	resp, err := http.Post(srv.URL+"/users", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); !strings.Contains(got, `"name":"Sam"`) {
		t.Errorf("body = %s, want name from Vars", got)
	}
}

func TestSplitCall(t *testing.T) {
	tests := []struct {
		call, method, target string
		ok                   bool
	}{
		{"POST /users", "POST", "/users", true},
		{" GET  /a?b=1 ", "GET", "/a?b=1", true},
		{"Create user", "", "", false},
		{"post /users", "", "", false},
		{"GET users", "", "", false},
	}
	for _, tt := range tests {
		method, target, ok := splitCall(tt.call)
		if method != tt.method || target != tt.target || ok != tt.ok {
			t.Errorf("splitCall(%q) = %q, %q, %v", tt.call, method, target, ok)
		}
	}
}

func TestReset(t *testing.T) {
	api := StartSource(t, "### [503] Down once\n@times = 1\nGET /x\n\n### Up\nGET /x\n\nup\n")
	get := func() int {
		resp, err := http.Get(api.URL + "/x")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if a, b := get(), get(); a != 503 || b != 200 {
		t.Fatalf("statuses = %d, %d", a, b)
	}
	api.Reset()
	if code := get(); code != 503 {
		t.Errorf("after Reset = %d, want 503", code)
	}
}

func TestStub(t *testing.T) {
	api := StartSource(t, "### Up\nGET /x\n\nup\n")
	t.Run("stubbed", func(t *testing.T) {
		api.Stub(t, "### [503] Down\nGET /x\n\ndown\n")
		resp, err := http.Get(api.URL + "/x")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Errorf("stubbed status = %d", resp.StatusCode)
		}
	})
	resp, err := http.Get(api.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("after the subtest = %d, want the stub removed", resp.StatusCode)
	}
}
