package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
