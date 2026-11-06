package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/internal/ui"
	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func TestReloadErrorsReachDashboardState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.http")
	if err := os.WriteFile(path, []byte("### route\nGET /x\n\none"), 0600); err != nil {
		t.Fatal(err)
	}
	methods, err := restclient.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	server := mockhttp.New(methods, nil)
	assets, err := ui.Assets()
	if err != nil {
		t.Fatal(err)
	}
	handler := ui.NewHandler(server, assets, ui.Config{})
	if err := os.WriteFile(path, []byte("### invalid\nNOT-A-METHOD /x"), 0600); err != nil {
		t.Fatal(err)
	}
	reloadMockFiles(server, []string{path}, nil, slog.Default(), io.Discard, io.Discard)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/state", nil))
	var state mockhttp.ConfigState
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Error == "" || state.Revision != 1 || state.Loading {
		t.Fatalf("bad failed reload state: %+v", state)
	}
	if err := os.WriteFile(path, []byte("### fixed\nGET /x\n\ntwo"), 0600); err != nil {
		t.Fatal(err)
	}
	reloadMockFiles(server, []string{path}, nil, slog.Default(), io.Discard, io.Discard)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/state", nil))
	state = mockhttp.ConfigState{}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Error != "" || state.Revision != 2 || state.LastSuccess == "" {
		t.Fatalf("bad successful reload state: %+v", state)
	}
}

func TestReloadMockFilesUpdatesRoutesAndPrintsSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte(`### User
GET /users

ok
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := mockhttp.New(nil, logger)

	var output bytes.Buffer
	reloadMockFiles(server, []string{path}, nil, logger, &output, io.Discard)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}

	got := output.String()
	for _, want := range []string{"Available mock methods:", "GET", "/users", "User"} {
		if !strings.Contains(got, want) {
			t.Fatalf("reload output = %q, want to contain %q", got, want)
		}
	}
}

func TestReloadMockFilesKeepsPreviousRoutesOnParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("not a valid request file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	initial, err := restclient.Parse("test.http", strings.NewReader(`### User
GET /users

ok
`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := mockhttp.New(initial, logger)

	var output bytes.Buffer
	var errOut bytes.Buffer
	reloadMockFiles(server, []string{path}, nil, logger, &output, &errOut)
	if output.Len() != 0 {
		t.Fatalf("output = %q, want empty on failed reload", output.String())
	}
	if errOut.Len() == 0 {
		t.Fatal("stderr empty on failed reload, want parse error")
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users", nil))
	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("status = %d body = %q, want previous route preserved", response.Code, response.Body.String())
	}
}
