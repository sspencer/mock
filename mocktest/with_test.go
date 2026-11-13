package mocktest

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func post(t *testing.T, url, body string, header http.Header) {
	t.Helper()
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header = header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestAssertCalledWith(t *testing.T) {
	srv := StartSource(t, api)
	post(t, srv.URL+"/users?source=web", `{"name":"Avery","roles":["admin"],"age":40}`, http.Header{"X-Tenant": {"acme"}, "Content-Type": {"application/json"}})
	post(t, srv.URL+"/users", `{"name":"Blake"}`, nil)

	got := srv.AssertCalledWith(t, "POST /users", With{
		Header: map[string]string{"X-Tenant": "acme", "Content-Type": "~.*json"},
		Query:  map[string]string{"source": "web"},
		JSON:   `{"name": "Avery", "roles": ["admin"]}`,
		Body:   "~.*age.*",
	})
	if len(got) != 1 {
		t.Fatalf("matched %d requests, want 1", len(got))
	}
	srv.AssertCalledWith(t, "Create user", With{JSON: `{"name": "Blake"}`, Header: map[string]string{"X-Tenant": "!"}})

	rec := &recorder{TB: t}
	srv.AssertCalledWith(rec, "POST /users", With{JSON: `{"name": "Casey"}`})
	srv.AssertCalledWith(rec, "POST /users", With{Query: map[string]string{"source": "app"}})
	srv.AssertCalledWith(rec, "DELETE /users", With{})
	if len(rec.errors) != 3 {
		t.Fatalf("failures = %d, want 3: %q", len(rec.errors), rec.errors)
	}
	for i, want := range []string{
		"POST /users called 2 times, none as wanted:\n  POST /users?source=web: JSON name: want \"Casey\"; got \"Avery\"",
		`query source: want app; got "web"`,
		"DELETE /users was not called",
	} {
		if !strings.Contains(rec.errors[i], want) {
			t.Errorf("failure %d = %q, want it to contain %q", i, rec.errors[i], want)
		}
	}
}

func TestLastCallAndWaitFor(t *testing.T) {
	srv := StartSource(t, api)
	go func() {
		time.Sleep(20 * time.Millisecond)
		post(t, srv.URL+"/users", `{"n":1}`, nil)
		post(t, srv.URL+"/users", `{"n":2}`, nil)
	}()
	if first := srv.WaitFor(t, "Create user", 2*time.Second); first.Request.Body.Text != `{"n":1}` {
		t.Errorf("WaitFor body = %q", first.Request.Body.Text)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(srv.Calls("Create user")) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if last := srv.LastCall(t, "POST /users"); last.Request.Body.Text != `{"n":2}` {
		t.Errorf("LastCall body = %q", last.Request.Body.Text)
	}
}

func TestAssertAllRoutesCalled(t *testing.T) {
	srv := StartSource(t, api)
	srv.Stub(t, "### Stubbed\nGET /stub\n")
	send(t, "POST", srv.URL+"/users")

	rec := &recorder{TB: t}
	srv.AssertAllRoutesCalled(rec)
	if len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "1 route never called:\n  GET /users/:id (Get user, ") {
		t.Fatalf("failures = %q", rec.errors)
	}
	send(t, "GET", srv.URL+"/users/1")
	srv.AssertAllRoutesCalled(t)
}
