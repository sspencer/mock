package data

import (
	"net/http/httptest"
	"sync"
	"testing"
)

func TestEndpointConcurrentAccess(t *testing.T) {
	ep := &Endpoint{
		Method: "GET",
		Path:   "/test",
		responses: []mockResponse{
			{status: 200, body: []byte("r1"), header: map[string]string{"content-type": "text/plain"}},
			{status: 201, body: []byte("r2"), header: map[string]string{"content-type": "text/plain"}},
		},
		localVars: map[string]mockResponse{
			"status=active": {status: 200, body: []byte("active"), header: map[string]string{"content-type": "text/plain"}},
		},
		globalVars: map[string]string{
			"host": "example.com",
		},
	}

	var wg sync.WaitGroup

	// Spawn 1000 concurrent requests
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(iteration int) {
			defer wg.Done()

			path := "/test"
			if iteration%2 == 0 {
				path = "/test?status=active"
			}

			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			ep.Handle(w, req)
		}(i)
	}

	wg.Wait()
}

func TestEndpointHotReloadIsolation(t *testing.T) {
	globalVars1 := map[string]string{"version": "v1"}
	globalVars2 := map[string]string{"version": "v2"}

	routes := []*route{
		{method: "GET", path: "/test", status: 200, body: []byte("test"),
			header: map[string]string{"content-type": "text/plain"}},
	}

	// Create two sets of endpoints (simulating hot-reload)
	endpoints1 := merge(routes, globalVars1)
	endpoints2 := merge(routes, globalVars2)

	// Verify independence
	if endpoints1[0].globalVars["version"] != "v1" {
		t.Error("endpoint1 has wrong version")
	}
	if endpoints2[0].globalVars["version"] != "v2" {
		t.Error("endpoint2 has wrong version")
	}

	// Modify one, ensure other unchanged
	endpoints1[0].globalVars["version"] = "modified"
	if endpoints2[0].globalVars["version"] != "v2" {
		t.Error("endpoint2 was affected by endpoint1 modification")
	}
}
