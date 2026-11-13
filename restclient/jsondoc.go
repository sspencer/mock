package restclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// parseJSONDocument checks that text is one JSON object or array and returns
// it compacted.
func parseJSONDocument(text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return "", errors.New("the value is not a JSON object or array")
	}
	if _, err := DecodeJSON([]byte(trimmed)); err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := json.Compact(&out, []byte(trimmed)); err != nil {
		return "", err
	}
	return out.String(), nil
}

// DecodeJSON decodes one JSON value, keeping numbers as json.Number, and
// rejects trailing data.
func DecodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return value, nil
}

// JSONMismatch is the first place a request body differs from a @json
// document: the path ("" for the whole body) and what was expected and
// received there, as short text.
type JSONMismatch struct {
	Path     string
	Expected string
	Received string
}

// JSONContains reports whether actual contains expected, as a @json document
// requires: an object contains every member of the expected object (extra
// members are allowed, key order does not matter), an array has the same
// length with each element containing the expected one in order, numbers are
// equal in value (1 and 1.0), and other values are equal. When actual does
// not contain expected, it returns the first difference.
func JSONContains(actual, expected any) (bool, JSONMismatch) {
	return jsonContains(actual, expected, "")
}

func jsonContains(actual, expected any, path string) (bool, JSONMismatch) {
	differ := func(expectedText, receivedText string) (bool, JSONMismatch) {
		return false, JSONMismatch{Path: path, Expected: expectedText, Received: receivedText}
	}
	switch want := expected.(type) {
	case map[string]any:
		got, ok := actual.(map[string]any)
		if !ok {
			return differ("an object", jsonKind(actual))
		}
		for _, key := range slices.Sorted(maps.Keys(want)) {
			child := joinJSONPath(path, key)
			value, present := got[key]
			if !present {
				return false, JSONMismatch{Path: child, Expected: jsonPreview(want[key]), Received: "nothing"}
			}
			if ok, mismatch := jsonContains(value, want[key], child); !ok {
				return false, mismatch
			}
		}
		return true, JSONMismatch{}
	case []any:
		got, ok := actual.([]any)
		if !ok {
			return differ("an array", jsonKind(actual))
		}
		if len(got) != len(want) {
			return differ(fmt.Sprintf("%d elements", len(want)), fmt.Sprintf("%d", len(got)))
		}
		for i := range want {
			if ok, mismatch := jsonContains(got[i], want[i], joinJSONPath(path, strconv.Itoa(i))); !ok {
				return false, mismatch
			}
		}
		return true, JSONMismatch{}
	case json.Number:
		if got, ok := actual.(json.Number); ok {
			a, errA := strconv.ParseFloat(got.String(), 64)
			b, errB := strconv.ParseFloat(want.String(), 64)
			if errA == nil && errB == nil && a == b {
				return true, JSONMismatch{}
			}
		}
	default:
		if actual == expected {
			return true, JSONMismatch{}
		}
	}
	return differ(jsonPreview(expected), jsonPreview(actual))
}

// joinJSONPath appends key to a path in the @json.* style, quoting keys that
// are not plain segments.
func joinJSONPath(path, key string) string {
	segment := key
	if !validPlainSegment(key) || key == "*" {
		segment = `["` + key + `"]`
		if path != "" {
			return path + segment
		}
		return segment
	}
	if path == "" {
		return segment
	}
	return path + "." + segment
}

func jsonKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	}
	return "missing"
}

// jsonPreview is value as compact JSON, shortened for a diagnostic.
func jsonPreview(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return jsonKind(value)
	}
	text := string(data)
	if len(text) > 40 {
		text = text[:37] + "..."
	}
	return text
}
