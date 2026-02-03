package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMethodNotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()

	methodNotFound(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	body := strings.TrimSpace(w.Body.String())
	if !strings.Contains(body, "could not be found") {
		t.Errorf("expected error message about resource not found, got %q", body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	tests := []struct {
		method string
	}{
		{"POST"},
		{"PUT"},
		{"DELETE"},
		{"PATCH"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/resource", nil)
			w := httptest.NewRecorder()

			methodNotAllowed(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
			}

			body := strings.TrimSpace(w.Body.String())
			if !strings.Contains(body, tt.method) {
				t.Errorf("expected error message to contain method %s, got %q", tt.method, body)
			}
			if !strings.Contains(body, "not supported") {
				t.Errorf("expected error message about method not supported, got %q", body)
			}
		})
	}
}
