package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func TestCORSDoesNotExposeAdmin(t *testing.T) {
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), "*", "console")
	for _, path := range []string{"/console", "/console/events", "/console/clear", "/console/routes", "/users"} {
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Origin", "https://other.test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := ""
		if path == "/users" {
			want = "*"
		}
		if w.Header().Get("Access-Control-Allow-Origin") != want {
			t.Errorf("%s exposed unexpected CORS", path)
		}
	}
}
func TestConfigRejectsInvalidMountAndPort(t *testing.T) {
	for _, args := range [][]string{{"-l", "bad/{path}"}, {"-l", "a/../b"}, {"-p", "65536"}, {"-p", "-1"}} {
		if _, err := parseConfig(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

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
	assets, err := staticFileSystem()
	if err != nil {
		t.Fatal(err)
	}
	handler := newHandler(server, "console", assets)
	if err := os.WriteFile(path, []byte("### invalid\nNOT-A-METHOD /x"), 0600); err != nil {
		t.Fatal(err)
	}
	reloadMockFiles(server, []string{path}, slog.Default(), io.Discard, io.Discard)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/console/state", nil))
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
	reloadMockFiles(server, []string{path}, slog.Default(), io.Discard, io.Discard)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/console/state", nil))
	state = mockhttp.ConfigState{}
	if err := json.Unmarshal(response.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.Error != "" || state.Revision != 2 || state.LastSuccess == "" {
		t.Fatalf("bad successful reload state: %+v", state)
	}
	reset := httptest.NewRequest("POST", "/console/reset", nil)
	reset.Header.Set("X-Requested-With", "xhr")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, reset)
	if response.Code != 204 {
		t.Fatal("reset endpoint not mounted")
	}
}
