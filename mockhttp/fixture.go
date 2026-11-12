package mockhttp

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// fixtureFormat is a fixture file's format, from its extension: "csv",
// "ndjson" (.ndjson or .jsonl) or "json" (anything else).
func fixtureFormat(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv":
		return "csv"
	case ".ndjson", ".jsonl":
		return "ndjson"
	}
	return "json"
}

// loadFixture reads and decodes a route's @fixture file. It is read for
// each response that uses it, so edits apply to the next request. A CSV file
// becomes an array of objects keyed by its header row, with string values;
// an NDJSON file becomes an array of its lines' values.
func loadFixture(method *restclient.Method) (any, error) {
	path := restclient.ResolveFile(method.Source, method.Fixture)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("@fixture %s: %w", method.Fixture, err)
	}
	switch fixtureFormat(method.Fixture) {
	case "csv":
		root, err := decodeCSV(data)
		if err != nil {
			return nil, fmt.Errorf("@fixture %s is not valid CSV: %w", method.Fixture, err)
		}
		return root, nil
	case "ndjson":
		root, err := decodeNDJSON(data)
		if err != nil {
			return nil, fmt.Errorf("@fixture %s is not valid NDJSON: %w", method.Fixture, err)
		}
		return root, nil
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

// decodeCSV reads CSV with a header row into an array of objects. A UTF-8
// byte order mark is ignored; rows may have fewer fields than the header.
func decodeCSV(data []byte) ([]any, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	rows := []any{}
	if len(records) == 0 {
		return rows, nil
	}
	header := records[0]
	for _, record := range records[1:] {
		if len(record) > len(header) {
			return nil, fmt.Errorf("a row has %d fields but the header has %d", len(record), len(header))
		}
		row := make(map[string]any, len(header))
		for i, name := range header {
			if i < len(record) {
				row[name] = record[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// decodeNDJSON reads one JSON value per line into an array. Blank lines are
// skipped.
func decodeNDJSON(data []byte) ([]any, error) {
	values := []any{}
	for i, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil || decoder.More() {
			if err == nil {
				err = fmt.Errorf("more than one JSON value")
			}
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		values = append(values, value)
	}
	return values, nil
}

// fixturePath returns the JSON path of a $fixture placeholder key: "" for
// $fixture itself (the whole file, as for a CSV fixture), the part after
// "fixture." or, for a top-level quoted key or selector, after "fixture".
func fixturePath(key string) (string, bool) {
	if key == "fixture" {
		return "", true
	}
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
