package data

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetEndpointsFromReader(t *testing.T) {
	input := `### Test Route
GET /api/users
Content-Type: application/json

[{"id": 1, "name": "John"}]
`
	reader := strings.NewReader(input)
	endpoints, err := GetEndpointsFromReader(reader)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}

	ep := endpoints[0]
	if ep.Method != "GET" {
		t.Errorf("expected method GET, got %s", ep.Method)
	}
	if ep.Path != "/api/users" {
		t.Errorf("expected path /api/users, got %s", ep.Path)
	}
}

func TestGetEndpointsFromFile(t *testing.T) {
	// Test with an existing test file
	endpoints, err := GetEndpointsFromFile("testdata/easy.http")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(endpoints) == 0 {
		t.Error("expected at least one endpoint")
	}
}

func TestGetEndpointsFromFile_NotFound(t *testing.T) {
	_, err := GetEndpointsFromFile("testdata/nonexistent.http")

	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestMerge(t *testing.T) {
	routes := []*route{
		{
			name:   "route1",
			method: "GET",
			path:   "/users",
			status: 200,
			body:   []byte("response1"),
			header: map[string]string{"content-type": "application/json"},
			delay:  0,
		},
		{
			name:   "route2",
			method: "GET",
			path:   "/users",
			status: 200,
			body:   []byte("response2"),
			header: map[string]string{"content-type": "application/json"},
			delay:  0,
		},
		{
			name:   "route3",
			method: "POST",
			path:   "/users",
			status: 201,
			body:   []byte("created"),
			header: map[string]string{"content-type": "application/json"},
			delay:  0,
		},
	}

	globalVars := map[string]string{"host": "localhost"}
	endpoints := merge(routes, globalVars)

	// Should merge the two GET /users into one endpoint with 2 responses
	// and keep POST /users separate
	if len(endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(endpoints))
	}

	// Find the GET endpoint
	var getEndpoint *Endpoint
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.Path == "/users" {
			getEndpoint = ep
			break
		}
	}

	if getEndpoint == nil {
		t.Fatal("GET /users endpoint not found")
	}

	if len(getEndpoint.responses) != 2 {
		t.Errorf("expected 2 responses for GET /users, got %d", len(getEndpoint.responses))
	}

	if getEndpoint.globalVars["host"] != "localhost" {
		t.Error("global vars not set correctly")
	}
}

func TestMergeWithQueryParams(t *testing.T) {
	routes := []*route{
		{
			name:   "route1",
			method: "GET",
			path:   "/users",
			uriKey: "status",
			uriVal: "active",
			status: 200,
			body:   []byte("active users"),
			header: map[string]string{"content-type": "text/plain"},
			delay:  0,
		},
		{
			name:   "route2",
			method: "GET",
			path:   "/users",
			uriKey: "status",
			uriVal: "inactive",
			status: 200,
			body:   []byte("inactive users"),
			header: map[string]string{"content-type": "text/plain"},
			delay:  0,
		},
	}

	endpoints := merge(routes, nil)

	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}

	ep := endpoints[0]
	if len(ep.localVars) != 2 {
		t.Errorf("expected 2 local vars, got %d", len(ep.localVars))
	}

	if _, ok := ep.localVars["status=active"]; !ok {
		t.Error("expected localVars to have 'status=active'")
	}

	if _, ok := ep.localVars["status=inactive"]; !ok {
		t.Error("expected localVars to have 'status=inactive'")
	}
}

func TestGetVarKey(t *testing.T) {
	tests := []struct {
		key      string
		value    string
		expected string
	}{
		{"status", "active", "status=active"},
		{"id", "123", "id=123"},
		{"", "", "="},
	}

	for _, tt := range tests {
		result := getVarKey(tt.key, tt.value)
		if result != tt.expected {
			t.Errorf("getVarKey(%q, %q) = %q, expected %q", tt.key, tt.value, result, tt.expected)
		}
	}
}

func TestEndpointGetNextResponse(t *testing.T) {
	ep := &Endpoint{
		responses: []mockResponse{
			{status: 200, body: []byte("response1")},
			{status: 201, body: []byte("response2")},
			{status: 202, body: []byte("response3")},
		},
	}

	// Test round-robin behavior
	resp1 := ep.getNextResponse()
	if resp1.status != 200 {
		t.Errorf("expected first response status 200, got %d", resp1.status)
	}

	resp2 := ep.getNextResponse()
	if resp2.status != 201 {
		t.Errorf("expected second response status 201, got %d", resp2.status)
	}

	resp3 := ep.getNextResponse()
	if resp3.status != 202 {
		t.Errorf("expected third response status 202, got %d", resp3.status)
	}

	// Should wrap around
	resp4 := ep.getNextResponse()
	if resp4.status != 200 {
		t.Errorf("expected wrapped response status 200, got %d", resp4.status)
	}
}

func TestEndpointHandle_SimpleResponse(t *testing.T) {
	ep := &Endpoint{
		Method: "GET",
		Path:   "/test",
		responses: []mockResponse{
			{
				status: 200,
				body:   []byte("test response"),
				header: map[string]string{"content-type": "text/plain"},
			},
		},
		localVars:  make(map[string]mockResponse),
		globalVars: make(map[string]string),
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	ep.Handle(w, req)

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "test response") {
		t.Errorf("expected body to contain 'test response', got %q", w.Body.String())
	}
}

func TestEndpointHandle_WithQueryParam(t *testing.T) {
	activeResp := mockResponse{
		status: 200,
		body:   []byte("active users"),
		header: map[string]string{"content-type": "text/plain"},
	}

	defaultResp := mockResponse{
		status: 200,
		body:   []byte("all users"),
		header: map[string]string{"content-type": "text/plain"},
	}

	ep := &Endpoint{
		Method:    "GET",
		Path:      "/users",
		responses: []mockResponse{defaultResp},
		localVars: map[string]mockResponse{
			"status=active": activeResp,
		},
		globalVars: make(map[string]string),
	}

	// Test with matching query param
	req := httptest.NewRequest("GET", "/users?status=active", nil)
	w := httptest.NewRecorder()

	ep.Handle(w, req)

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "active users") {
		t.Errorf("expected body to contain 'active users', got %q", w.Body.String())
	}
}

func TestEndpointHandle_RoundRobin(t *testing.T) {
	ep := &Endpoint{
		Method: "GET",
		Path:   "/test",
		responses: []mockResponse{
			{
				status: 200,
				body:   []byte("response1"),
				header: map[string]string{"content-type": "text/plain"},
			},
			{
				status: 201,
				body:   []byte("response2"),
				header: map[string]string{"content-type": "text/plain"},
			},
		},
		localVars:  make(map[string]mockResponse),
		globalVars: make(map[string]string),
	}

	// First request should get first response
	req1 := httptest.NewRequest("GET", "/test", nil)
	w1 := httptest.NewRecorder()
	ep.Handle(w1, req1)

	if w1.Code != 200 {
		t.Errorf("expected first status 200, got %d", w1.Code)
	}

	// Second request should get second response
	req2 := httptest.NewRequest("GET", "/test", nil)
	w2 := httptest.NewRecorder()
	ep.Handle(w2, req2)

	if w2.Code != 201 {
		t.Errorf("expected second status 201, got %d", w2.Code)
	}

	// Third request should wrap to first response
	req3 := httptest.NewRequest("GET", "/test", nil)
	w3 := httptest.NewRecorder()
	ep.Handle(w3, req3)

	if w3.Code != 200 {
		t.Errorf("expected third status 200, got %d", w3.Code)
	}
}

func TestEndpointHandle_WithDelay(t *testing.T) {
	ep := &Endpoint{
		Method: "GET",
		Path:   "/test",
		responses: []mockResponse{
			{
				status: 200,
				body:   []byte("delayed response"),
				header: map[string]string{"content-type": "text/plain"},
				delay:  50 * time.Millisecond,
			},
		},
		localVars:  make(map[string]mockResponse),
		globalVars: make(map[string]string),
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	start := time.Now()
	ep.Handle(w, req)
	elapsed := time.Since(start)

	if elapsed < 50*time.Millisecond {
		t.Errorf("expected delay of at least 50ms, got %v", elapsed)
	}

	if w.Code != 200 {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestEndpointHandle_WithGlobalVars(t *testing.T) {
	ep := &Endpoint{
		Method: "GET",
		Path:   "/test",
		responses: []mockResponse{
			{
				status: 200,
				body:   []byte("Host: {{host}}"),
				header: map[string]string{"content-type": "text/plain"},
			},
		},
		localVars: make(map[string]mockResponse),
		globalVars: map[string]string{
			"host": "example.com",
		},
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	ep.Handle(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "example.com") {
		t.Errorf("expected body to contain 'example.com', got %q", body)
	}
}

func TestEndpointHandle_CustomHeaders(t *testing.T) {
	ep := &Endpoint{
		Method: "GET",
		Path:   "/test",
		responses: []mockResponse{
			{
				status: 200,
				body:   []byte("test"),
				header: map[string]string{
					"content-type":    "application/json",
					"x-custom-header": "custom-value",
				},
			},
		},
		localVars:  make(map[string]mockResponse),
		globalVars: make(map[string]string),
	}

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	ep.Handle(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected content-type 'application/json', got %q", ct)
	}

	if xh := w.Header().Get("X-Custom-Header"); xh != "custom-value" {
		t.Errorf("expected x-custom-header 'custom-value', got %q", xh)
	}
}

func TestParserGetRoutes(t *testing.T) {
	input := `### Test
GET /test

OK`

	p := NewParser("", "")
	err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	routes := p.GetRoutes()
	if len(routes) == 0 {
		t.Error("expected at least one route")
	}
}

func TestParserGetGlobalVars(t *testing.T) {
	input := `@host = localhost
@port = 8080

### Test
GET /test

OK`

	p := NewParser("", "")
	err := p.Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	vars := p.GetGlobalVars()
	if vars["host"] != "localhost" {
		t.Errorf("expected host=localhost, got %q", vars["host"])
	}
	if vars["port"] != "8080" {
		t.Errorf("expected port=8080, got %q", vars["port"])
	}
}

func TestParseStateString(t *testing.T) {
	tests := []struct {
		state    parseState
		expected string
	}{
		{stateNone, "NONE"},
		{stateVariable, "VARIABLE"},
		{stateResponse, "RESPONSE"},
		{stateHeader, "HEADER"},
		{stateBody, "BODY"},
	}

	for _, tt := range tests {
		result := tt.state.String()
		if result != tt.expected {
			t.Errorf("state %v String() = %q, expected %q", tt.state, result, tt.expected)
		}
	}
}

func TestRouteString(t *testing.T) {
	r := &route{
		name:   "test-route",
		method: "GET",
		path:   "/test",
		status: 200,
		delay:  100 * time.Millisecond,
		body:   []byte("test body"),
		header: map[string]string{"content-type": "text/plain"},
	}

	result := r.String()
	if !strings.Contains(result, "test-route") {
		t.Errorf("expected String() to contain route name, got %q", result)
	}
	if !strings.Contains(result, "GET") {
		t.Errorf("expected String() to contain method, got %q", result)
	}
	if !strings.Contains(result, "/test") {
		t.Errorf("expected String() to contain path, got %q", result)
	}
}

func TestEndpointConcurrency(t *testing.T) {
	// Test that getNextResponse is thread-safe
	ep := &Endpoint{
		responses: []mockResponse{
			{status: 200},
			{status: 201},
			{status: 202},
		},
	}

	done := make(chan bool)

	// Spawn multiple goroutines accessing getNextResponse
	for i := 0; i < 100; i++ {
		go func() {
			ep.getNextResponse()
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 100; i++ {
		<-done
	}

	// Should have cycled through responses without panic
	if ep.index != 100 {
		t.Errorf("expected index 100, got %d", ep.index)
	}
}

func TestGetEndpointsIntegration(t *testing.T) {
	// Test the full flow from parsing to endpoints
	input := `@host = api.example.com

### Get User
GET /users/:id

{"id": "{{id}}", "name": "{{name}}"}

### Create User
POST /users
Content-Type: application/json

{"status": "created"}
`

	reader := strings.NewReader(input)
	endpoints, err := GetEndpointsFromReader(reader)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}

	// Check that global vars are set
	for _, ep := range endpoints {
		if ep.globalVars["host"] != "api.example.com" {
			t.Error("global vars not propagated to endpoints")
		}
	}
}

func TestGetEndpointsWithMultipleResponses(t *testing.T) {
	input := `### Response 1
GET /users

[{"id": 1}]

### Response 2
GET /users

[{"id": 2}]
`

	reader := bytes.NewBufferString(input)
	endpoints, err := GetEndpointsFromReader(reader)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint (merged), got %d", len(endpoints))
	}

	ep := endpoints[0]
	if len(ep.responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(ep.responses))
	}
}
