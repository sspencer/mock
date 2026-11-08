package mockhttp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestEnvPlaceholder(t *testing.T) {
	t.Setenv("MOCK_TEST_TOKEN", "s3cret")
	server := newHeadTestServer(t, "### Token\nGET /token\nX-Token: {{$env.MOCK_TEST_TOKEN}}\n\n{{$env.MOCK_TEST_TOKEN}} {{$env.MOCK_TEST_UNSET | default none}}\n")

	response := httptest.NewRecorder()
	// A query parameter with the same name must not stand in for the environment.
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/token?env.MOCK_TEST_TOKEN=spoof", nil))

	if got := response.Body.String(); got != "s3cret none" {
		t.Errorf("body = %q, want %q", got, "s3cret none")
	}
	if got := response.Header().Get("X-Token"); got != "s3cret" {
		t.Errorf("X-Token = %q", got)
	}
}

func TestEnvPlaceholderWarnsWhenUnset(t *testing.T) {
	t.Setenv("MOCK_TEST_SET", "1")
	methods, err := restclient.Parse("env.http", strings.NewReader("### E\nGET /e\n\n{{$env.MOCK_TEST_SET}} {{$env.MOCK_TEST_MISSING}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	New(methods, slog.New(slog.NewTextHandler(&logs, nil)))
	if !strings.Contains(logs.String(), "environment variable MOCK_TEST_MISSING is not set") {
		t.Errorf("missing warning for unset variable:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "MOCK_TEST_SET ") {
		t.Errorf("unexpected warning for a set variable:\n%s", logs.String())
	}
}
