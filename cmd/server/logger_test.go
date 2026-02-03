package main

import (
	"net/http"
	"testing"
)

func TestColorForStatus(t *testing.T) {
	tests := []struct {
		status        int
		expectedColor string
		description   string
	}{
		{200, green, "2xx should be green"},
		{201, green, "2xx should be green"},
		{299, green, "2xx should be green"},
		{300, white, "3xx should be white"},
		{301, white, "3xx should be white"},
		{399, white, "3xx should be white"},
		{400, red, "4xx should be red"},
		{404, red, "4xx should be red"},
		{499, red, "4xx should be red"},
		{500, red, "5xx should be red"},
		{503, red, "5xx should be red"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := colorForStatus(tt.status)
			if result != tt.expectedColor {
				t.Errorf("colorForStatus(%d) returned unexpected color", tt.status)
			}
		})
	}
}

func TestColorForMethod(t *testing.T) {
	tests := []struct {
		method        string
		expectedColor string
	}{
		{http.MethodGet, blue},
		{http.MethodPost, cyan},
		{http.MethodPut, yellow},
		{http.MethodDelete, red},
		{http.MethodPatch, green},
		{http.MethodHead, magenta},
		{http.MethodOptions, white},
		{"UNKNOWN", reset},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			result := colorForMethod(tt.method)
			if result != tt.expectedColor {
				t.Errorf("colorForMethod(%s) returned unexpected color", tt.method)
			}
		})
	}
}

func TestMonolog(t *testing.T) {
	logger := monolog()

	// Test that it doesn't panic
	testLog := HTTPLog{
		Request: HTTPRequestLog{
			Method: "GET",
			URL:    "/test",
		},
		Response: HTTPResponseLog{
			Status: 200,
		},
	}

	// Should not panic
	logger(testLog)
}

func TestColorlog(t *testing.T) {
	logger := colorlog()

	// Test that it doesn't panic
	testLog := HTTPLog{
		Request: HTTPRequestLog{
			Method: "GET",
			URL:    "/test",
		},
		Response: HTTPResponseLog{
			Status: 200,
		},
	}

	// Should not panic
	logger(testLog)
}

func TestNewLogger(t *testing.T) {
	// Test that newLogger returns a function
	logger := newLogger()

	if logger == nil {
		t.Error("newLogger() returned nil")
	}

	// Test that the returned function works
	testLog := HTTPLog{
		Request: HTTPRequestLog{
			Method: "POST",
			URL:    "/api/test",
		},
		Response: HTTPResponseLog{
			Status: 201,
		},
	}

	// Should not panic
	logger(testLog)
}

func TestHTTPLogStructs(t *testing.T) {
	// Test that the structs can be instantiated
	reqLog := HTTPRequestLog{
		Header:  http.Header{"Content-Type": []string{"application/json"}},
		Method:  "GET",
		URL:     "/test",
		Details: "details",
		Body:    "body",
	}

	respLog := HTTPResponseLog{
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Status:     200,
		StatusText: "OK",
		Time:       "12:00:00",
		Details:    "details",
		Body:       "body",
	}

	log := HTTPLog{
		Request:  reqLog,
		Response: respLog,
	}

	if log.Request.Method != "GET" {
		t.Error("HTTPLog struct not working correctly")
	}
}
