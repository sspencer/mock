package mockhttp

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func adminRequest(t *testing.T, s *Server, method, target, contentType, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-Requested-With", "test")
	rec := httptest.NewRecorder()
	s.ServeRoutes(rec, req)
	return rec.Code, rec.Body.String()
}

func TestTemporaryRoutes(t *testing.T) {
	s := newTestServer(t, "### Users\nGET /users\n\nfrom file\n")
	code, body := adminRequest(t, s, "POST", "/routes", "text/plain", "### [500] Broken users\nGET /users\n\nbroken\n")
	var first TemporaryRoutes
	if code != 201 || json.Unmarshal([]byte(body), &first) != nil || first.ID != "t1" || first.Routes[0] != "Broken users" {
		t.Fatalf("POST /routes = %d %s", code, body)
	}
	if code, got := serveRequest(t, s, "GET", "/users", "", ""); code != 500 || got != "broken" {
		t.Errorf("with the stub = %d %q", code, got)
	}
	// A newer stub wins; JSON bodies work too.
	code, body = adminRequest(t, s, "POST", "/routes", "application/json", `{"source": "### [418] Teapot\nGET /users\n\n{{$unknownThing}}\n"}`)
	var second TemporaryRoutes
	if code != 201 || json.Unmarshal([]byte(body), &second) != nil || second.ID != "t2" || len(second.Warnings) == 0 {
		t.Fatalf("second POST = %d %s", code, body)
	}
	if code, _ := serveRequest(t, s, "GET", "/users", "", ""); code != 418 {
		t.Errorf("newest stub = %d, want 418", code)
	}

	// Reloads keep temporary routes; /routes lists them first.
	s.SetMethods(s.Methods())
	rec := httptest.NewRecorder()
	s.ServeRoutes(rec, httptest.NewRequest("GET", "/routes", nil))
	var routes []RouteInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &routes)
	if len(routes) != 3 || routes[0].Temporary != "t2" || routes[1].Temporary != "t1" || routes[2].Temporary != "" {
		t.Errorf("routes = %+v", routes)
	}
	if len(s.Methods()) != 1 {
		t.Errorf("Methods() includes temporary routes: %d", len(s.Methods()))
	}

	if code, body := adminRequest(t, s, "DELETE", "/routes/t2", "", ""); code != 200 || !strings.Contains(body, `"removed":1`) {
		t.Errorf("DELETE /routes/t2 = %d %s", code, body)
	}
	if code, _ := serveRequest(t, s, "GET", "/users", "", ""); code != 500 {
		t.Errorf("after removing t2 = %d, want t1's 500", code)
	}
	if code, _ := adminRequest(t, s, "DELETE", "/routes/t2", "", ""); code != 404 {
		t.Errorf("deleting t2 again = %d", code)
	}
	s.Reset()
	if code, got := serveRequest(t, s, "GET", "/users", "", ""); code != 200 || got != "from file" {
		t.Errorf("after reset = %d %q", code, got)
	}

	for body, want := range map[string]string{"not a route file": "content before first ###", "": "no routes"} {
		if code, got := adminRequest(t, s, "POST", "/routes", "text/plain", body); code != 400 || !strings.Contains(got, want) {
			t.Errorf("POST %q = %d %s, want %q", body, code, got, want)
		}
	}
	req := httptest.NewRequest("POST", "/routes", strings.NewReader("### X\nGET /x\n"))
	rec = httptest.NewRecorder()
	s.ServeRoutes(rec, req)
	if rec.Code != 403 {
		t.Errorf("POST without X-Requested-With = %d, want 403", rec.Code)
	}
}
