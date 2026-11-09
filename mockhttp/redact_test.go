package mockhttp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestSensitiveName(t *testing.T) {
	for name, want := range map[string]bool{
		"Authorization": true, "X-Api-Key": true, "api_key": true, "access_token": true,
		"password": true, "client-secret": true, "Cookie": true, "key": true, "sessionId": true,
		"Content-Type": false, "author": false, "keyboard": false, "name": false, "monkey": false,
	} {
		if got := sensitiveName(name); got != want {
			t.Errorf("sensitiveName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestRedactHelpers(t *testing.T) {
	tests := []struct{ name, got, want string }{
		{"bearer", redactHeaderValue("Authorization", "Bearer abc.def"), "Bearer [redacted]"},
		{"basic without scheme", redactHeaderValue("Authorization", "abc"), "[redacted]"},
		{"cookie names kept", redactHeaderValue("Cookie", "sid=1; theme=dark"), "sid=[redacted]; theme=[redacted]"},
		{"set-cookie attributes kept", redactHeaderValue("Set-Cookie", "sid=1; Path=/; HttpOnly"), "sid=[redacted]; Path=/; HttpOnly"},
		{"api key header", redactHeaderValue("X-Api-Key", "k"), "[redacted]"},
		{"plain header", redactHeaderValue("Accept", "text/html"), "text/html"},
		{"query", redactURL("/a?token=x&page=2&api_key=y#top"), "/a?token=%5Bredacted%5D&page=2&api_key=%5Bredacted%5D#top"},
		{"query untouched", redactURL("/a?page=2&q=a%20b"), "/a?page=2&q=a%20b"},
		{"json", redactBody(`{"user":"a", "password" : "p\"w", "nested":{"refresh_token":"t"},"n":1}`, "application/json"), `{"user":"a", "password" : "[redacted]", "nested":{"refresh_token":"[redacted]"},"n":1}`},
		{"json sniffed", redactBody(`{"secret":"s"}`, ""), `{"secret":"[redacted]"}`},
		{"form", redactBody("user=a&password=p", "application/x-www-form-urlencoded; charset=utf-8"), "user=a&password=%5Bredacted%5D"},
		{"text", redactBody(`password=p`, "text/plain"), `password=p`},
		{"text json", redactBody(`{"token":"t"}`, "text/plain; charset=utf-8"), `{"token":"[redacted]"}`},
		{"reason", redactReason(`JSON $.user.password: expected "x"; received ["hunter2"]`), `JSON $.user.password: expected "x"; received [redacted]`},
		{"reason kept", redactReason(`Query page: expected "1"; received ["2"]`), `Query page: expected "1"; received ["2"]`},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestSetRedactMasksRecordedEvents(t *testing.T) {
	methods := []restclient.Method{
		{Name: "login", Method: http.MethodPost, Path: "/login", Status: 200, Headers: http.Header{"Set-Cookie": {"sid=s3cr3t; Path=/"}}, Body: `{"token":"t0k3n","ok":true}`},
		{Name: "needs key", Method: http.MethodGet, Path: "/items", Status: 200, MatchQuery: map[string][]string{"api_key": {"good"}}},
	}
	var logged strings.Builder
	server := New(methods, slog.New(slog.NewTextHandler(&logged, nil)))
	server.SetRedact(true)
	var hooked RequestEvent
	server.SetRequestHook(func(event RequestEvent) { hooked = event })

	request := httptest.NewRequest(http.MethodPost, "/login?access_token=q", strings.NewReader(`{"user":"a","password":"hunter2"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer abc")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if !strings.Contains(response.Body.String(), "t0k3n") || !strings.Contains(response.Header().Get("Set-Cookie"), "s3cr3t") {
		t.Fatalf("the client must get the real response: %q %q", response.Body.String(), response.Header())
	}
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items?api_key=wrong", nil))

	events := server.Requests(RequestFilter{})
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if hooked.Request.URL != events[1].Request.URL {
		t.Errorf("hook event URL = %q, want the redacted %q", hooked.Request.URL, events[1].Request.URL)
	}
	for _, secret := range []string{"hunter2", "abc", "t0k3n", "s3cr3t", "access_token=q", "wrong"} {
		for _, event := range events {
			for _, text := range []string{
				event.Request.URL, event.Request.Details, event.Request.Body.Text, event.Request.Headers.Get("Authorization"),
				event.Response.Details, event.Response.Body.Text, event.Response.Headers.Get("Set-Cookie"),
			} {
				if strings.Contains(text, secret) {
					t.Errorf("event %d leaks %q in %q", event.ID, secret, text)
				}
			}
			for _, candidate := range event.Match.Candidates {
				if strings.Contains(strings.Join(candidate.Reasons, "\n"), secret) {
					t.Errorf("event %d reasons leak %q: %q", event.ID, secret, candidate.Reasons)
				}
			}
		}
	}
	if strings.Contains(logged.String(), "access_token=q") {
		t.Errorf("log line leaks the query: %s", logged.String())
	}
	if got := events[0].Request.Headers.Get("Authorization"); got != "Bearer [redacted]" {
		t.Errorf("Authorization = %q", got)
	}
	if !strings.Contains(events[0].Request.Details, `"user":"a"`) {
		t.Errorf("non-credential fields should be kept: %q", events[0].Request.Details)
	}
	if len(events[1].Match.Candidates) == 0 {
		t.Fatalf("expected near-miss candidates for the unmatched request")
	}

}
