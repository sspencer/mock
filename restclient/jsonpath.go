package restclient

import (
	"fmt"
	"strings"
)

// JSONSegment is one step of a @json matcher or {{$json.*}} path.
type JSONSegment struct {
	// Name is an object key, or an array index when it is all digits and
	// not Quoted.
	Name string
	// Any is set for a "*" segment, which selects every array element.
	Any bool
	// Quoted is set for a ["key"] segment, which is always an object key,
	// even when it is "*" or all digits, and may contain dots.
	Quoted bool
	// Select is set for a [key=value] segment, which keeps the elements of
	// an array that are objects whose Name field equals Value. Value may be
	// a "$reference" that the caller resolves.
	Select bool
	Value  string
}

// ParseJSONPath splits a JSON field path such as user.roles.0, items.*.sku,
// meta["user.id"].name or users[id=42].name into segments. Plain segments use
// letters, digits, '_' and '-'; ["key"] segments hold any key without double
// quotes or whitespace; [key=value] selectors pick array elements by a field.
// Bracketed segments may follow a segment directly or after a dot.
func ParseJSONPath(path string) ([]JSONSegment, error) {
	var segments []JSONSegment
	rest := path
	for {
		if strings.HasPrefix(rest, "[") && !strings.HasPrefix(rest, `["`) {
			end := strings.IndexByte(rest, ']')
			key, value, ok := strings.Cut(rest[1:max(end, 1)], "=")
			if end < 0 || !ok || key == "*" || !validPlainSegment(key) || value == "" || strings.ContainsAny(value, "\" \t{}") {
				return nil, fmt.Errorf(`invalid [field=value] selector in JSON path %q (example: users[id=42].name)`, path)
			}
			segments = append(segments, JSONSegment{Name: key, Select: true, Value: value})
			rest = rest[end+1:]
		} else if strings.HasPrefix(rest, `["`) {
			end := strings.Index(rest[2:], `"]`)
			if end < 0 {
				return nil, fmt.Errorf(`unterminated ["key"] in JSON path %q`, path)
			}
			key := rest[2 : 2+end]
			if key == "" || strings.ContainsAny(key, "\" \t") {
				return nil, fmt.Errorf(`invalid ["key"] in JSON path %q (keys cannot be empty or hold quotes or spaces)`, path)
			}
			segments = append(segments, JSONSegment{Name: key, Quoted: true})
			rest = rest[2+end+2:]
		} else {
			end := strings.IndexAny(rest, `.[`)
			if end < 0 {
				end = len(rest)
			}
			name := rest[:end]
			if !validPlainSegment(name) {
				return nil, fmt.Errorf("invalid JSON path %q (use dot-separated fields such as user.name, or [\"a.b\"] for a key with dots)", path)
			}
			segments = append(segments, JSONSegment{Name: name, Any: name == "*"})
			rest = rest[end:]
		}
		switch {
		case rest == "":
			return segments, nil
		case strings.HasPrefix(rest, ".["):
			rest = rest[1:]
		case rest[0] == '.':
			rest = rest[1:]
			if rest == "" {
				return nil, fmt.Errorf("invalid JSON path %q (it ends with a dot)", path)
			}
		case rest[0] == '[':
		default:
			return nil, fmt.Errorf("invalid JSON path %q", path)
		}
	}
}

func validPlainSegment(name string) bool {
	if name == "*" {
		return true
	}
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
