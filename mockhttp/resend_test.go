package mockhttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func postResend(t *testing.T, server *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/resend", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	// The admin listener's own address must not decide which routes match.
	ctx := context.WithValue(request.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8081})
	response := httptest.NewRecorder()
	server.ServeResend(response, request.WithContext(ctx))
	return response
}

func TestServeResendReplaysThroughTheRoutes(t *testing.T) {
	methods := []restclient.Method{
		{Name: "admin port", Method: http.MethodPost, Path: "/users", Port: 8081, Status: 500},
		{Name: "create", Method: http.MethodPost, Path: "/users", Port: 8080, Status: 201, MatchHeaders: map[string][]string{"X-Team": {"blue"}}, Body: `{"name":"{{$json.name}}"}`},
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))

	response := postResend(t, server, `{"from":7,"method":"post","url":"/users?x=1","host":"localhost:8080","headers":{"X-Team":["blue"],"Content-Type":["application/json"],"Content-Length":["99"]},"body":"{\"name\":\"Ada\"}"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var result struct{ ID uint64 }
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.ID == 0 {
		t.Fatalf("result = %s, %v", response.Body.String(), err)
	}
	events := server.Requests(RequestFilter{})
	if len(events) != 1 || events[0].ID != result.ID {
		t.Fatalf("events = %+v", events)
	}
	event := events[0]
	if event.ResendOf == nil || *event.ResendOf != 7 {
		t.Errorf("resendOf = %v, want 7", event.ResendOf)
	}
	if event.Match.Route == nil || event.Match.Route.Name != "create" || event.Response.Status != 201 {
		t.Errorf("answered by %+v with %d, want create/201", event.Match.Route, event.Response.Status)
	}
	if event.Response.Body.Text != `{"name":"Ada"}` || event.Request.URL != "/users?x=1" || event.Request.Host != "localhost:8080" {
		t.Errorf("event = %+v", event)
	}

	// Binary bodies round-trip as base64; a reset fault is logged, not fatal.
	server.SetMethods([]restclient.Method{{Name: "boom", Method: http.MethodPut, Path: "/blob", Status: 200, Body: "x", Fault: "reset", FaultRate: 1}})
	response = postResend(t, server, `{"method":"PUT","url":"/blob","body":"AAEC","encoding":"base64"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("reset status = %d: %s", response.Code, response.Body.String())
	}
	last := server.Requests(RequestFilter{})[1]
	if last.Response.Fault != "reset" || last.Request.Body.Encoding != "base64" || last.Request.Body.Text != "AAEC" || last.ResendOf == nil || *last.ResendOf != 0 {
		t.Errorf("reset event = %+v", last)
	}
}

func TestServeResendRejectsBadInput(t *testing.T) {
	server := New(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, body := range []string{
		`{`, `{"method":"","url":"/"}`, `{"method":"GET","url":"http://evil/"}`,
		`{"method":"GET","url":"/","encoding":"gzip"}`, `{"method":"GET","url":"/","body":"!","encoding":"base64"}`,
	} {
		if response := postResend(t, server, body); response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", body, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/resend", strings.NewReader(`{"method":"GET","url":"/"}`))
	response := httptest.NewRecorder()
	server.ServeResend(response, request)
	if response.Code != http.StatusForbidden {
		t.Errorf("a POST without X-Requested-With or JSON must be refused: %d", response.Code)
	}
	if len(server.Requests(RequestFilter{})) != 0 {
		t.Error("rejected resends must not be logged")
	}
}
