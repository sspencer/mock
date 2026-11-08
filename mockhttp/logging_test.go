package mockhttp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestRequestLogJSONFields(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader("### Get user\nGET /users/:id\n\nok\n"))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	server := New(methods, slog.New(slog.NewJSONHandler(&logs, nil)))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/7?x=1", nil))

	var record map[string]any
	if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
		t.Fatalf("log %q: %v", logs.String(), err)
	}
	if record["msg"] != "http request" || record["method"] != "GET" || record["path"] != "/users/7?x=1" ||
		record["status"] != float64(200) || record["route"] != "Get user" {
		t.Errorf("record = %v", record)
	}
	if _, ok := record["duration"].(float64); !ok {
		t.Errorf("duration = %#v, want nanoseconds", record["duration"])
	}
}
