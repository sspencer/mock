package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponseCapturingWriter_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &ResponseCapturingWriter{
		ResponseWriter: w,
		StatusCode:     http.StatusOK,
		Body:           &bytes.Buffer{},
	}

	cw.WriteHeader(http.StatusCreated)

	if cw.StatusCode != http.StatusCreated {
		t.Errorf("expected status code %d, got %d", http.StatusCreated, cw.StatusCode)
	}

	if w.Code != http.StatusCreated {
		t.Errorf("expected underlying writer status code %d, got %d", http.StatusCreated, w.Code)
	}
}

func TestResponseCapturingWriter_Write(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &ResponseCapturingWriter{
		ResponseWriter: w,
		StatusCode:     http.StatusOK,
		Body:           &bytes.Buffer{},
	}

	testData := []byte("test response body")
	n, err := cw.Write(testData)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if n != len(testData) {
		t.Errorf("expected to write %d bytes, wrote %d", len(testData), n)
	}

	if cw.Body.String() != "test response body" {
		t.Errorf("expected body %q, got %q", "test response body", cw.Body.String())
	}

	if w.Body.String() != "test response body" {
		t.Errorf("expected underlying writer body %q, got %q", "test response body", w.Body.String())
	}
}

func TestResponseCapturingWriter_WriteNilBody(t *testing.T) {
	w := httptest.NewRecorder()
	cw := &ResponseCapturingWriter{
		ResponseWriter: w,
		StatusCode:     http.StatusOK,
		Body:           nil, // No buffer
	}

	testData := []byte("test")
	n, err := cw.Write(testData)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if n != len(testData) {
		t.Errorf("expected to write %d bytes, wrote %d", len(testData), n)
	}

	// Should still write to underlying writer
	if w.Body.String() != "test" {
		t.Errorf("expected underlying writer body %q, got %q", "test", w.Body.String())
	}
}

func TestRequestLogger_SkipsLogPath(t *testing.T) {
	server := &mockServer{
		logPath: "/mock",
	}

	loggerCalled := false
	testLogger := func(log HTTPLog) {
		loggerCalled = true
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	middleware := server.requestLogger(testLogger)
	wrappedHandler := middleware(handler)

	// Request to /mock path should skip logging
	req := httptest.NewRequest("GET", "/mock/logs", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	if loggerCalled {
		t.Error("expected logger not to be called for /mock path")
	}

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestRequestLogger_CapturesRequest(t *testing.T) {
	server := &mockServer{
		logPath: "/mock",
		clients: make(map[chan string]struct{}),
	}

	var capturedLog HTTPLog
	testLogger := func(log HTTPLog) {
		capturedLog = log
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"status": "created"}`))
	})

	middleware := server.requestLogger(testLogger)
	wrappedHandler := middleware(handler)

	req := httptest.NewRequest("POST", "/api/users", strings.NewReader(`{"name": "John"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Verify response
	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}

	// Verify captured log
	if capturedLog.Request.Method != "POST" {
		t.Errorf("expected method POST, got %s", capturedLog.Request.Method)
	}

	if capturedLog.Request.URL != "/api/users" {
		t.Errorf("expected URL /api/users, got %s", capturedLog.Request.URL)
	}

	if capturedLog.Response.Status != http.StatusCreated {
		t.Errorf("expected status 201, got %d", capturedLog.Response.Status)
	}

	if !strings.Contains(capturedLog.Request.Body, "John") {
		t.Errorf("expected request body to contain 'John', got %q", capturedLog.Request.Body)
	}

	if !strings.Contains(capturedLog.Response.Body, "created") {
		t.Errorf("expected response body to contain 'created', got %q", capturedLog.Response.Body)
	}
}

func TestRequestLogger_MultipleRequests(t *testing.T) {
	server := &mockServer{
		logPath: "/mock",
		clients: make(map[chan string]struct{}),
	}

	logCount := 0
	testLogger := func(log HTTPLog) {
		logCount++
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := server.requestLogger(testLogger)
	wrappedHandler := middleware(handler)

	// Make multiple requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
	}

	if logCount != 5 {
		t.Errorf("expected 5 log calls, got %d", logCount)
	}
}

func TestRequestLogger_DifferentMethods(t *testing.T) {
	server := &mockServer{
		logPath: "/mock",
		clients: make(map[chan string]struct{}),
	}

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var capturedLog HTTPLog
			testLogger := func(log HTTPLog) {
				capturedLog = log
			}

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware := server.requestLogger(testLogger)
			wrappedHandler := middleware(handler)

			req := httptest.NewRequest(method, "/test", nil)
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)

			if capturedLog.Request.Method != method {
				t.Errorf("expected method %s, got %s", method, capturedLog.Request.Method)
			}
		})
	}
}
