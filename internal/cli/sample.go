package cli

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// sampleRequest is a request built from a route's path and matchers, the
// same one the admin UI's "Sample request" curl command sends.
type sampleRequest struct {
	Method string
	// Target is the escaped path and query, such as "/users/1?active=true".
	Target  string
	Headers http.Header
	Body    string
}

// numericParam matches parameter names that look numeric; they get "1" and
// anything else gets "example" (sampleParam in the UI's inspector.mjs).
var numericParam = regexp.MustCompile(`^(?:id|ids|num|number|count|page|limit|offset|index|year|age|size)$|(?:_id|Id|ID)$`)

func sampleParam(name string) string {
	if numericParam.MatchString(name) {
		return "1"
	}
	return "example"
}

// sampleExact reports whether a matcher value is used as written: anything
// but "*" (present), "!..." (absent or negated) and "~regex".
func sampleExact(value string) bool {
	return value != "*" && !strings.HasPrefix(value, "!") && !strings.HasPrefix(value, "~")
}

// sampleValue is the value a sample request sends for a matcher: the value
// itself when exact, else placeholder. A placeholder may not satisfy a regex.
func sampleValue(value, placeholder string) string {
	if sampleExact(value) {
		return value
	}
	return placeholder
}

// buildSampleRequest builds the sample request for method. Path parameters
// take an exact @path value or a sample; required query parameters, headers
// and cookies are added; the body comes from @json, then @form, then an
// exact @body. Absent ("!") matchers are left out.
func buildSampleRequest(method restclient.Method) sampleRequest {
	sample := sampleRequest{Method: method.Method, Headers: http.Header{}}

	segments := strings.Split(method.EscapedPath, "/")
	for i, segment := range segments {
		if name, ok := strings.CutPrefix(segment, ":"); ok && name != "" {
			if value, ok := method.MatchPath[name]; ok && sampleExact(value) {
				segments[i] = url.PathEscape(value)
			} else {
				segments[i] = sampleParam(name)
			}
		} else if name, ok := strings.CutPrefix(segment, "*"); ok && i == len(segments)-1 {
			// A trailing * or *name wildcard matches the rest of the path.
			segments[i] = "example"
			if value, ok := method.MatchPath[name]; ok && name != "" && sampleExact(value) {
				parts := strings.Split(value, "/")
				for j, part := range parts {
					parts[j] = url.PathEscape(part)
				}
				segments[i] = strings.Join(parts, "/")
			}
		}
	}
	sample.Target = strings.Join(segments, "/")

	query := url.Values{}
	for name, values := range method.Query {
		query[name] = slices.Clone(values)
	}
	for _, name := range slices.Sorted(maps.Keys(method.MatchQuery)) {
		for _, value := range method.MatchQuery[name] {
			if value != "!" {
				query.Add(name, sampleValue(value, sampleParam(name)))
			}
		}
	}
	if encoded := query.Encode(); encoded != "" {
		sample.Target += "?" + encoded
	}

	for name, values := range method.MatchHeaders {
		for _, value := range values {
			if value != "!" {
				sample.Headers.Add(name, sampleValue(value, "example"))
			}
		}
	}
	var cookies []string
	for _, name := range slices.Sorted(maps.Keys(method.MatchCookies)) {
		for _, value := range method.MatchCookies[name] {
			if value != "!" {
				cookies = append(cookies, name+"="+sampleValue(value, "example"))
			}
		}
	}
	if len(cookies) > 0 {
		sample.Headers.Set("Cookie", strings.Join(cookies, "; "))
	}

	if body := sampleJSONBody(method.MatchJSON); body != "" {
		sample.Body = body
		if sample.Headers.Get("Content-Type") == "" {
			sample.Headers.Set("Content-Type", "application/json")
		}
	} else if body := sampleFormBody(method.MatchForm); body != "" {
		sample.Body = body
		if sample.Headers.Get("Content-Type") == "" {
			sample.Headers.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else if method.MatchBody != "" && sampleExact(method.MatchBody) {
		sample.Body = method.MatchBody
	}
	return sample
}

// sampleFormBody encodes the @form matchers like a query string.
func sampleFormBody(matchForm url.Values) string {
	form := url.Values{}
	for name, values := range matchForm {
		for _, value := range values {
			if value != "!" {
				form.Add(name, sampleValue(value, "example"))
			}
		}
	}
	return form.Encode()
}

var errSampleConflict = errors.New("conflicting @json paths")

// sampleJSONBody builds a JSON body from @json matchers. Exact values are
// JSON literals when they parse (42, true) and strings otherwise; "*" and
// regex values become "example"; "!" fields are left out. Digit segments
// make arrays and a "*" segment puts the value first. It returns "" when
// there is nothing to send or the paths conflict.
func sampleJSONBody(matchJSON map[string]string) string {
	var root any
	for _, path := range slices.Sorted(maps.Keys(matchJSON)) {
		value := matchJSON[path]
		if value == "!" {
			continue
		}
		segments, err := restclient.ParseJSONPath(path)
		if err != nil {
			return ""
		}
		var literal any = "example"
		if sampleExact(value) {
			if json.Unmarshal([]byte(value), &literal) != nil {
				literal = value
			}
		}
		if root, err = setJSONPath(root, segments, literal); err != nil {
			return ""
		}
	}
	if root == nil {
		return ""
	}
	body, err := json.Marshal(root)
	if err != nil {
		return ""
	}
	return string(body)
}

// setJSONPath stores value at segments under node, creating objects and
// arrays on the way, and returns the updated node.
func setJSONPath(node any, segments []restclient.JSONSegment, value any) (any, error) {
	if len(segments) == 0 {
		return value, nil
	}
	segment := segments[0]
	name := segment.Name
	if segment.Any {
		name = "0"
	}
	index, err := strconv.Atoi(name)
	if !segment.Quoted && err == nil && index >= 0 && strconv.Itoa(index) == name {
		list, ok := node.([]any)
		if node != nil && !ok {
			return nil, errSampleConflict
		}
		if index > 1000 {
			return nil, errSampleConflict
		}
		for len(list) <= index {
			list = append(list, nil)
		}
		child, err := setJSONPath(list[index], segments[1:], value)
		if err != nil {
			return nil, err
		}
		list[index] = child
		return list, nil
	}
	object, ok := node.(map[string]any)
	if node != nil && !ok {
		return nil, errSampleConflict
	}
	if object == nil {
		object = map[string]any{}
	}
	child, err := setJSONPath(object[name], segments[1:], value)
	if err != nil {
		return nil, err
	}
	object[name] = child
	return object, nil
}
