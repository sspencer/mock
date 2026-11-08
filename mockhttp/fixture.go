package mockhttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// loadFixture reads and decodes a route's @fixture JSON file. It is read for
// each response that uses it, so edits apply to the next request.
func loadFixture(method *restclient.Method) (any, error) {
	path := restclient.ResolveFile(method.Source, method.Fixture)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("@fixture %s: %w", method.Fixture, err)
	}
	var root any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil || decoder.More() {
		if err == nil {
			err = fmt.Errorf("more than one JSON value")
		}
		return nil, fmt.Errorf("@fixture %s is not valid JSON: %w", method.Fixture, err)
	}
	return root, nil
}

// fixturePath returns the JSON path of a $fixture placeholder key: the part
// after "fixture." or, for a top-level quoted key, after "fixture".
func fixturePath(key string) (string, bool) {
	if path, ok := strings.CutPrefix(key, "fixture."); ok && path != "" {
		return path, true
	}
	if path, ok := strings.CutPrefix(key, "fixture"); ok && strings.HasPrefix(path, "[") {
		return path, true
	}
	return "", false
}

// itemPath returns the JSON path of an each block's $item placeholder key:
// "" for $item itself, or the part after "item." (or after "item" for a
// quoted key).
func itemPath(key string) (string, bool) {
	if key == "item" {
		return "", true
	}
	if path, ok := strings.CutPrefix(key, "item."); ok && path != "" {
		return path, true
	}
	if path, ok := strings.CutPrefix(key, "item"); ok && strings.HasPrefix(path, "[") {
		return path, true
	}
	return "", false
}

// fixtureDocument loads the route's fixture file once per response.
func (r *renderer) fixtureDocument() (any, bool) {
	if r.method.Fixture == "" {
		return nil, false
	}
	if !r.fixtureDone {
		r.fixtureDone = true
		r.fixture, r.fixtureErr = loadFixture(r.method)
	}
	return r.fixture, r.fixtureErr == nil
}

// fixtureValue resolves {{$fixture.path}} from the route's fixture file.
func (r *renderer) fixtureValue(path string) (string, bool) {
	root, ok := r.fixtureDocument()
	if !ok {
		return "", false
	}
	values := selectJSON(root, path)
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}
