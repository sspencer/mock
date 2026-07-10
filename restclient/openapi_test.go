package restclient

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOpenAPIJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.json")
	spec := `{
  "openapi": "3.0.0",
  "paths": {
    "/pets/{id}": {
      "get": {"operationId": "getPet", "summary": "Get pet"},
      "delete": {"summary": "Delete pet"}
    },
    "/pets": {
      "post": {"operationId": "createPet"}
    }
  }
}`
	if err := os.WriteFile(path, []byte(spec), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	methods, err := LoadOpenAPI(path)
	if err != nil {
		t.Fatalf("LoadOpenAPI() error = %v", err)
	}
	if len(methods) != 3 {
		t.Fatalf("len(methods) = %d, want 3", len(methods))
	}

	found := map[string]string{}
	for _, m := range methods {
		found[m.Method+" "+m.Path] = m.Name
	}
	if found["GET /pets/:id"] != "getPet" {
		t.Fatalf("routes = %#v, want GET /pets/:id named getPet", found)
	}
	if _, ok := found["POST /pets"]; !ok {
		t.Fatalf("routes = %#v, want POST /pets", found)
	}
}
