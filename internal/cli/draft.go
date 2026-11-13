package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// exchange is one captured request and response that a draft section is
// built from: a HAR entry for mock import, or a proxied request for -r.
type exchange struct {
	Method string
	// Target is the escaped path and query, such as "/users/1?page=2".
	Target string
	// Port is the port the request arrived on, or 0 when unknown.
	Port           int
	RequestHeaders http.Header
	RequestBody    string
	// Status is the real response's status; 0 drafts a placeholder response.
	Status          int
	ResponseHeaders http.Header
	// ResponseBody is the response as text. ResponseOK is false when there
	// is no usable response (binary, truncated or failed), which drafts a
	// placeholder 200 JSON response.
	ResponseBody string
	ResponseOK   bool
	// host is the request's host, for mock import's -host filter.
	host string
}

// This file is the Go port of buildRouteDraft and buildRouteDrafts in
// internal/ui/static/draft.mjs; keep the two in step.

var (
	draftVariableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	draftJSONKey      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	sectionMarker     = regexp.MustCompile(`(?m)^###`)
)

const draftMaxMatchers = 8

// draftRoutePath turns a captured path into a literal route path: "*"
// segments are wildcards and ":name" segments are parameters in routes, so
// escape them.
func draftRoutePath(path string) string {
	if path == "" {
		path = "/"
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segment = strings.NewReplacer("*", "%2A", "{", "%7B", "}", "%7D").Replace(segment)
		if strings.HasPrefix(segment, ":") {
			segment = "%3A" + segment[1:]
		}
		segments[i] = segment
	}
	return strings.Join(segments, "/")
}

// draftInlineSafe reports whether a body can be pasted inline: no line may
// end the section ("###") and the first line must not read as a file
// reference ("< path").
func draftInlineSafe(text string) bool {
	return !sectionMarker.MatchString(text) && !strings.HasPrefix(text, "< ")
}

// usable reports whether the exchange's real response becomes the draft's.
func (x exchange) usable() bool {
	return x.ResponseOK && x.Status > 0 && draftInlineSafe(x.ResponseBody)
}

// orderedNames returns the distinct names of a query or form string in the
// order they first appear, keeping only valid variable names, up to the cap.
func orderedNames(raw string) []string {
	var names []string
	seen := map[string]bool{}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, _, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(key)
		if err != nil || seen[name] {
			continue
		}
		seen[name] = true
		if draftVariableName.MatchString(name) {
			names = append(names, name)
		}
	}
	if len(names) > draftMaxMatchers {
		names = names[:draftMaxMatchers]
	}
	return names
}

// jsonKeys returns the top-level keys of a JSON object body in order, leaving
// out null fields and keys a @json path cannot name, up to the cap.
func jsonKeys(text string) []string {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &fields) != nil || fields == nil {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var keys []string
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil
		}
		key, _ := token.(string)
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			return nil
		}
		if seen[key] || !draftJSONKey.MatchString(key) || bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	if len(keys) > draftMaxMatchers {
		keys = keys[:draftMaxMatchers]
	}
	return keys
}

// draftSection returns a .http section for the exchange. Matchers require
// only what the request had (fields present, not their values), so the draft
// also answers similar requests. A usable real response becomes the draft's
// response; otherwise it is a placeholder 200 JSON body. mockPort is the
// default port; when the request's port differs, the draft sets @port.
func draftSection(x exchange, mockPort int) string {
	path, query, _ := strings.Cut(x.Target, "?")
	routePath := draftRoutePath(path)
	useResponse := x.usable()
	status := 200
	if useResponse {
		status = x.Status
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### [%d] %s %s\n", status, x.Method, routePath)
	if x.Port != 0 && mockPort != 0 && x.Port != mockPort {
		fmt.Fprintf(&b, "@port = %d\n", x.Port)
	}
	for _, name := range orderedNames(query) {
		fmt.Fprintf(&b, "@query.%s = *\n", name)
	}
	if x.RequestHeaders.Get("Authorization") != "" {
		b.WriteString("@header.Authorization = *\n")
	}
	if strings.TrimSpace(x.RequestBody) != "" {
		if strings.Contains(strings.ToLower(x.RequestHeaders.Get("Content-Type")), "x-www-form-urlencoded") {
			for _, name := range orderedNames(x.RequestBody) {
				fmt.Fprintf(&b, "@form.%s = *\n", name)
			}
		} else {
			for _, key := range jsonKeys(x.RequestBody) {
				fmt.Fprintf(&b, "@json.%s = *\n", key)
			}
		}
	}
	// Real response bodies are served as captured, not as templates.
	if useResponse && strings.Contains(x.ResponseBody, "{{") {
		b.WriteString("@template = off\n")
	}
	fmt.Fprintf(&b, "%s %s\n", x.Method, routePath)
	if useResponse {
		if contentType := x.ResponseHeaders.Get("Content-Type"); contentType != "" {
			fmt.Fprintf(&b, "Content-Type: %s\n", contentType)
		}
		if body := strings.TrimRightFunc(x.ResponseBody, unicode.IsSpace); body != "" {
			fmt.Fprintf(&b, "\n%s\n", body)
		}
	} else {
		b.WriteString("Content-Type: application/json\n\n{}\n")
	}
	return b.String()
}

// draftKey is the method and path that drafts are deduplicated by.
func draftKey(x exchange) string {
	path, _, _ := strings.Cut(x.Target, "?")
	return x.Method + " " + path
}

// draftSections drafts one section per distinct method and path, in first
// appearance order, using the first exchange with a usable response for each
// (else the first). Sections are separated by blank lines.
func draftSections(exchanges []exchange, mockPort int) (string, int) {
	var order []string
	chosen := map[string]exchange{}
	for _, x := range exchanges {
		key := draftKey(x)
		current, ok := chosen[key]
		if !ok {
			order = append(order, key)
		}
		if !ok || !current.usable() && x.usable() {
			chosen[key] = x
		}
	}
	sections := make([]string, len(order))
	for i, key := range order {
		sections[i] = draftSection(chosen[key], mockPort)
	}
	return strings.Join(sections, "\n"), len(sections)
}
