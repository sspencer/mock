package mockhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const journalSource = `### [201] Create user
POST /users

{"id":1}

### [200] Get user
GET /users/:id

{"id":{{$id}}}
`

func journalRequests(t *testing.T, server *Server, query string) []RequestEvent {
	t.Helper()
	response := httptest.NewRecorder()
	server.ServeRequests(response, httptest.NewRequest(http.MethodGet, "/requests"+query, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /requests%s status = %d: %s", query, response.Code, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	var events []RequestEvent
	if err := json.Unmarshal(response.Body.Bytes(), &events); err != nil {
		t.Fatalf("decode: %v\n%s", err, response.Body)
	}
	return events
}

func sendJournalTraffic(server *Server) {
	for _, req := range []struct{ method, target string }{
		{"POST", "/users"},
		{"GET", "/users/7?expand=1"},
		{"GET", "/users/8"},
		{"DELETE", "/users/7"},
	} {
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(req.method, req.target, strings.NewReader("")))
	}
}

func TestServeRequestsFilters(t *testing.T) {
	server := newHeadTestServer(t, journalSource)
	if got := journalRequests(t, server, ""); len(got) != 0 {
		t.Fatalf("empty journal = %d events, want 0", len(got))
	}
	sendJournalTraffic(server)

	urls := func(events []RequestEvent) string {
		var out []string
		for _, e := range events {
			out = append(out, e.Request.Method+" "+e.Request.URL)
		}
		return strings.Join(out, ", ")
	}
	tests := []struct{ query, want string }{
		{"", "POST /users, GET /users/7?expand=1, GET /users/8, DELETE /users/7"},
		{"?method=get", "GET /users/7?expand=1, GET /users/8"},
		{"?path=/users/7", "GET /users/7?expand=1, DELETE /users/7"},
		{"?method=GET&path=/users/7", "GET /users/7?expand=1"},
		{"?route=Get+user", "GET /users/7?expand=1, GET /users/8"},
		{"?matched=false", "DELETE /users/7"},
		{"?matched=true&method=POST", "POST /users"},
		{"?route=nope", ""},
	}
	for _, tt := range tests {
		if got := urls(journalRequests(t, server, tt.query)); got != tt.want {
			t.Errorf("GET /requests%s = %q, want %q", tt.query, got, tt.want)
		}
	}

	all := journalRequests(t, server, "")
	since := journalRequests(t, server, "?since="+strconv.FormatUint(all[1].ID, 10))
	if got := urls(since); got != "GET /users/8, DELETE /users/7" {
		t.Errorf("since second event = %q", got)
	}
}

func TestServeRequestsAfterClear(t *testing.T) {
	server := newHeadTestServer(t, journalSource)
	sendJournalTraffic(server)
	server.mu.Lock()
	server.clearLocked()
	server.mu.Unlock()
	if got := journalRequests(t, server, ""); len(got) != 0 {
		t.Fatalf("after clear = %d events, want 0", len(got))
	}
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/users", nil))
	if got := journalRequests(t, server, ""); len(got) != 1 {
		t.Fatalf("after clear and one request = %d events, want 1", len(got))
	}
}

func TestServeRequestsRejectsBadInput(t *testing.T) {
	server := newHeadTestServer(t, journalSource)
	for _, query := range []string{"?matched=maybe", "?since=-1"} {
		response := httptest.NewRecorder()
		server.ServeRequests(response, httptest.NewRequest(http.MethodGet, "/requests"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET /requests%s status = %d, want 400", query, response.Code)
		}
	}
	response := httptest.NewRecorder()
	server.ServeRequests(response, httptest.NewRequest(http.MethodPost, "/requests", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /requests status = %d, want 405", response.Code)
	}
}
