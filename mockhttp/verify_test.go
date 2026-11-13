package mockhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func verify(t *testing.T, s *Server, body string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	s.ServeVerify(rec, req)
	return rec.Code, rec.Body.String()
}

func TestVerifyEndpoint(t *testing.T) {
	s := newTestServer(t, "### [201] Create order\nPOST /orders\n\n{}\n\n### Orders\nGET /orders\n\n[]\n")
	serveRequest(t, s, "POST", "/orders", "application/json", "{}")
	serveRequest(t, s, "GET", "/orders?page=2", "", "")
	serveRequest(t, s, "GET", "/orders", "", "")

	code, body := verify(t, s, `{"call": "POST /orders", "times": 1}`, nil)
	var report VerifyReport
	if err := json.Unmarshal([]byte(body), &report); err != nil || code != http.StatusOK || !report.OK || report.Results[0].Got != 1 || report.Message != "verify: 1 expectation passed" {
		t.Fatalf("one expectation: %d %s", code, body)
	}

	code, body = verify(t, s, `[
		{"call": "Orders", "atLeast": 2},
		{"call": "GET /orders?page=2", "times": 1},
		{"call": "GET /orders", "atMost": 1},
		{"call": "DELETE /orders"},
		{"call": "POST /orders", "times": 0},
		{"allMatched": true}
	]`, nil)
	report = VerifyReport{}
	if err := json.Unmarshal([]byte(body), &report); err != nil || code != http.StatusUnprocessableEntity || report.OK {
		t.Fatalf("failing expectations: %d %s", code, body)
	}
	var oks []bool
	for _, result := range report.Results {
		oks = append(oks, result.OK)
	}
	if want := []bool{true, true, false, false, false, true}; !slices.Equal(oks, want) {
		t.Errorf("results ok = %v, want %v", oks, want)
	}
	for _, want := range []string{
		"verify: 3 of 6 expectations failed",
		"GET /orders: called 2 times, want at most 1 time",
		"DELETE /orders: called 0 times, want at least 1 time",
		"POST /orders: called 1 time, want never",
		`GET /orders?page=2 -> 200 ("Orders")`,
	} {
		if !strings.Contains(report.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, report.Message)
		}
	}

	// Plain text for shells, and unmatched requests.
	serveRequest(t, s, "GET", "/nope", "", "")
	code, body = verify(t, s, `{"allMatched": true}`, map[string]string{"Accept": "text/plain"})
	if code != http.StatusUnprocessableEntity || !strings.HasPrefix(body, "verify: 1 of 1 expectation failed\n  1 request matched no route: GET /nope\n") {
		t.Errorf("text answer: %d %q", code, body)
	}
}

func TestVerifyRejectsBadRequests(t *testing.T) {
	s := newTestServer(t, "### A\nGET /a\n")
	for _, body := range []string{
		``, `[]`, `{}`, `{"call": "GET /a", "times": 1, "atLeast": 1}`, `{"call": "GET /a", "count": 1}`,
		`{"call": "GET /a", "atLeast": 3, "atMost": 1}`, `{"allMatched": true, "call": "GET /a"}`, `{"call": "GET /a", "times": -1}`,
	} {
		if code, _ := verify(t, s, body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, code)
		}
	}
	req := httptest.NewRequest("POST", "/verify", strings.NewReader(`{"call":"GET /a"}`))
	rec := httptest.NewRecorder()
	s.ServeVerify(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("without a JSON content type: %d, want 403", rec.Code)
	}
	rec = httptest.NewRecorder()
	s.ServeVerify(rec, httptest.NewRequest("GET", "/verify", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d, want 405", rec.Code)
	}
}
