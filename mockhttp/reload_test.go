package mockhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func postReload(server *Server, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/reload", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	server.ServeReload(response, request)
	return response
}

var ajax = map[string]string{"X-Requested-With": "fetch"}

func TestServeReload(t *testing.T) {
	server := newHeadTestServer(t, journalSource)
	if got := postReload(server, ajax).Code; got != http.StatusConflict {
		t.Fatalf("without a reloader status = %d, want 409", got)
	}

	calls := 0
	fail := false
	server.SetReloader(func() {
		calls++
		server.BeginReload()
		if fail {
			server.ReloadFailed(errors.New("broken.http:3: bad request line"))
			return
		}
		server.SetMethods(server.Methods())
	})

	response := postReload(server, ajax)
	var state ConfigState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode: %v\n%s", err, response.Body)
	}
	if response.Code != http.StatusOK || state.Revision != 2 || state.Error != "" || calls != 1 {
		t.Fatalf("reload = %d %+v after %d calls", response.Code, state, calls)
	}

	fail = true
	response = postReload(server, map[string]string{"Content-Type": "application/json"})
	state = ConfigState{}
	_ = json.Unmarshal(response.Body.Bytes(), &state)
	if response.Code != http.StatusUnprocessableEntity || state.Error == "" || state.Revision != 2 {
		t.Fatalf("failed reload = %d %+v", response.Code, state)
	}

	for name, headers := range map[string]map[string]string{
		"bare form post": nil,
		"cross-site":     {"X-Requested-With": "fetch", "Sec-Fetch-Site": "cross-site"},
		"other origin":   {"X-Requested-With": "fetch", "Origin": "http://evil.example"},
	} {
		if got := postReload(server, headers).Code; got != http.StatusForbidden {
			t.Errorf("%s status = %d, want 403", name, got)
		}
	}
	response = httptest.NewRecorder()
	server.ServeReload(response, httptest.NewRequest(http.MethodGet, "/reload", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /reload status = %d, want 405", response.Code)
	}
	if calls != 2 {
		t.Errorf("reloader calls = %d, want 2", calls)
	}
}
