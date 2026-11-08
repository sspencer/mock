package mockhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// requestBody is the part of a request body that body matchers see.
// unavailable is set when the body was larger than maxMatchBodyBytes or could
// not be read; then no body matcher matches.
type requestBody struct {
	data        []byte
	unavailable bool
}

// requestFacts is what routes are matched against, computed once per request.
// The JSON body is decoded lazily, on the first @json matcher.
type requestFacts struct {
	r         *http.Request
	query     url.Values
	queryErr  error
	headers   http.Header
	localPort int
	body      requestBody

	jsonDone bool
	json     any
	jsonErr  error

	formDone bool
	form     url.Values
	formErr  error

	cookiesDone bool
	cookies     map[string][]string

	xmlDone bool
	xml     *xmlNode
	xmlErr  error
}

func newRequestFacts(r *http.Request, body requestBody) *requestFacts {
	query, err := url.ParseQuery(r.URL.RawQuery)
	return &requestFacts{r: r, query: query, queryErr: err, headers: requestEventHeaders(r), localPort: requestLocalPort(r), body: body}
}

// findMethod returns the first route, in load order, that matches r.
func (s *Server) findMethod(r *http.Request, body requestBody) (*restclient.Method, templateValues, MatchInfo, bool) {
	// Routes are immutable once installed; SetMethods swaps the slice, so a
	// snapshot keeps one request within one configuration revision.
	s.mu.Lock()
	methods, hits, revision := s.methods, s.hits, s.config.Revision
	s.mu.Unlock()

	facts := newRequestFacts(r, body)
	if facts.queryErr == nil {
		// HEAD tries explicit HEAD routes first, then answers from a GET route
		// (the body is dropped when the response is written).
		requestMethods := []string{r.Method}
		if r.Method == http.MethodHead {
			requestMethods = append(requestMethods, http.MethodGet)
		}
		for _, requestMethod := range requestMethods {
			for i := range methods {
				method := &methods[i]
				if values, ok := facts.match(method, requestMethod); ok {
					route := describeRoute(*method, i, revision)
					route.Hits = hits[i].Add(1)
					path, _ := matchPath(routePattern(*method), r.URL.EscapedPath())
					return method, templateValues{values: values, path: path, facts: facts}, MatchInfo{Route: &route, Revision: revision}, true
				}
			}
		}
	}
	return nil, templateValues{}, MatchInfo{Revision: revision, Candidates: closestRoutes(methods, facts)}, false
}

// methodAnswers reports whether a route declared for routeMethod can answer a
// request with requestMethod: the same method, or a GET route for HEAD.
func methodAnswers(routeMethod, requestMethod string) bool {
	return routeMethod == requestMethod || requestMethod == http.MethodHead && routeMethod == http.MethodGet
}

// match reports whether method accepts the request, treated as requestMethod,
// and returns the path and query values available to its templates.
func (f *requestFacts) match(method *restclient.Method, requestMethod string) (map[string]string, bool) {
	if method.Method != requestMethod || !portMatches(method.Port, f.localPort) {
		return nil, false
	}
	values, ok := matchPath(routePattern(*method), f.r.URL.EscapedPath())
	if !ok || !queryMatches(method.Query, f.query) || !queryMatcherMatches(method.MatchQuery, f.query) {
		return nil, false
	}
	if !pathMatcherMatches(method.MatchPath, values) || !headerMatches(method.MatchHeaders, f.headers) {
		return nil, false
	}
	for name, expectedValues := range method.MatchCookies {
		for _, expected := range expectedValues {
			if !restclient.MustMatcher(expected).Match(f.cookieValues(name)) {
				return nil, false
			}
		}
	}
	if method.MatchBody != "" && !f.bodyMatches(method.MatchBody) {
		return nil, false
	}
	for path, expected := range method.MatchJSON {
		if !f.jsonMatches(path, expected) {
			return nil, false
		}
	}
	for path, expected := range method.MatchXML {
		if !f.xmlMatches(path, expected) {
			return nil, false
		}
	}
	for name, expectedValues := range method.MatchForm {
		for _, expected := range expectedValues {
			if !f.formMatches(name, expected) {
				return nil, false
			}
		}
	}
	params := make(map[string]bool, len(values))
	for name := range values {
		params[name] = true
	}
	for name, value := range f.captures(method) {
		if !params[name] {
			values[name] = value
		}
	}
	for name, queryValues := range f.query {
		if _, exists := values[name]; !exists && len(queryValues) > 0 {
			values[name] = queryValues[0]
		}
	}
	return values, true
}

// captures collects named regexp groups from a matched route's matchers.
// Later matchers win when two groups share a name: path, query, headers,
// cookies, body, JSON fields, XML fields, then form fields, each in name
// order.
func (f *requestFacts) captures(method *restclient.Method) map[string]string {
	out := make(map[string]string)
	add := func(m restclient.Matcher, values []string) {
		for name, value := range m.Captures(values) {
			out[name] = value
		}
	}
	pathValues, _ := matchPath(routePattern(*method), f.r.URL.EscapedPath())
	for _, name := range slices.Sorted(maps.Keys(method.MatchPath)) {
		add(restclient.MustMatcher(method.MatchPath[name]), []string{pathValues[name]})
	}
	for _, name := range slices.Sorted(maps.Keys(method.MatchQuery)) {
		for _, raw := range method.MatchQuery[name] {
			add(restclient.MustMatcher(raw), f.query[name])
		}
	}
	for _, name := range slices.Sorted(maps.Keys(method.MatchHeaders)) {
		for _, raw := range method.MatchHeaders[name] {
			add(restclient.MustMatcher(raw), f.headers.Values(name))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(method.MatchCookies)) {
		for _, raw := range method.MatchCookies[name] {
			add(restclient.MustMatcher(raw), f.cookieValues(name))
		}
	}
	if method.MatchBody != "" {
		add(restclient.MustBodyMatcher(method.MatchBody), f.bodyValues())
	}
	for _, path := range slices.Sorted(maps.Keys(method.MatchJSON)) {
		values, _ := f.jsonValues(path)
		add(restclient.MustMatcher(method.MatchJSON[path]), values)
	}
	for _, path := range slices.Sorted(maps.Keys(method.MatchXML)) {
		values, _ := f.xmlValues(path)
		add(restclient.MustMatcher(method.MatchXML[path]), values)
	}
	for _, name := range slices.Sorted(maps.Keys(method.MatchForm)) {
		values, _ := f.formValues(name)
		for _, raw := range method.MatchForm[name] {
			add(restclient.MustMatcher(raw), values)
		}
	}
	return out
}

// cookieValues returns the values of every request cookie with that name, in
// the order the Cookie headers list them.
func (f *requestFacts) cookieValues(name string) []string {
	if !f.cookiesDone {
		f.cookiesDone = true
		f.cookies = make(map[string][]string)
		for _, cookie := range f.r.Cookies() {
			f.cookies[cookie.Name] = append(f.cookies[cookie.Name], cookie.Value)
		}
	}
	return f.cookies[name]
}

// multipartBoundary returns the boundary of a multipart/form-data content
// type.
func multipartBoundary(contentType string) (string, bool) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return "", false
	}
	return params["boundary"], true
}

// parseMultipartForm reads the fields of a multipart/form-data body. A text
// part's value is its content; a file part (one with a filename) contributes
// its file name, so @form.avatar = ~.*\.png matches an upload by name. A
// blank body has no fields.
func parseMultipartForm(body []byte, boundary string) (url.Values, error) {
	form := url.Values{}
	if len(bytes.TrimSpace(body)) == 0 {
		return form, nil
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return form, nil
		}
		if err != nil {
			return nil, errBodyNotMultipart
		}
		name := part.FormName()
		if name == "" {
			continue
		}
		if filename := part.FileName(); filename != "" {
			form.Add(name, filename)
			continue
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return nil, errBodyNotMultipart
		}
		form.Add(name, string(value))
	}
}

func (f *requestFacts) formMatches(name, expected string) bool {
	values, err := f.formValues(name)
	return err == nil && restclient.MustMatcher(expected).Match(values)
}

// formValues returns a field of a URL-encoded or multipart/form-data body.
// A blank body has no fields; a body that does not parse is an error, and
// then no @form matcher matches.
func (f *requestFacts) formValues(name string) ([]string, error) {
	if f.body.unavailable {
		return nil, errBodyUnavailable
	}
	if !f.formDone {
		f.formDone = true
		f.form, f.formErr = url.Values{}, nil
		if boundary, ok := multipartBoundary(f.r.Header.Get("Content-Type")); ok {
			f.form, f.formErr = parseMultipartForm(f.body.data, boundary)
		} else if text := strings.TrimSpace(string(f.body.data)); text != "" {
			if form, err := url.ParseQuery(text); err != nil {
				f.formErr = errBodyNotForm
			} else {
				f.form = form
			}
		}
	}
	if f.formErr != nil {
		return nil, f.formErr
	}
	return f.form[name], nil
}

// lookup resolves a {{$name}} placeholder: $env.NAME from the server's
// environment (never from the request), then path, capture and query values,
// then the explicit namespaces $path.name, $query.name, $header.Name and
// $cookie.name, then $json.field.path, $xml.element.path and $form.name from
// the request body.
func (v templateValues) lookup(key string) (string, bool) {
	if name, ok := strings.CutPrefix(key, "env."); ok && name != "" {
		return os.LookupEnv(name)
	}
	if value, ok := v.values[key]; ok {
		return value, true
	}
	if name, ok := strings.CutPrefix(key, "path."); ok && name != "" {
		value, found := v.path[name]
		return value, found
	}
	if v.facts == nil {
		return "", false
	}
	var values []string
	if name, ok := strings.CutPrefix(key, "query."); ok && name != "" {
		values = v.facts.query[name]
	} else if name, ok := strings.CutPrefix(key, "header."); ok && name != "" {
		values = v.facts.headers.Values(name)
	} else if name, ok := strings.CutPrefix(key, "cookie."); ok && name != "" {
		values = v.facts.cookieValues(name)
	} else if path, ok := strings.CutPrefix(key, "json."); ok && path != "" {
		values, _ = v.facts.jsonValues(path)
	} else if path, ok := strings.CutPrefix(key, "xml."); ok && path != "" {
		values, _ = v.facts.xmlValues(path)
	} else if name, ok := strings.CutPrefix(key, "form."); ok && name != "" {
		values, _ = v.facts.formValues(name)
	}
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// templateValues are the request values a response template can reference.
type templateValues struct {
	values map[string]string
	// path holds only the path parameters, for $path.name.
	path  map[string]string
	facts *requestFacts
}

func routePattern(method restclient.Method) string {
	return (&url.URL{Path: method.Path, RawPath: method.EscapedPath}).EscapedPath()
}

func (f *requestFacts) bodyMatches(expected string) bool {
	if f.body.unavailable {
		return false
	}
	return restclient.MustBodyMatcher(expected).Match(f.bodyValues())
}

// bodyValues is the body as matcher values: none when it is empty.
func (f *requestFacts) bodyValues() []string {
	if len(f.body.data) == 0 {
		return nil
	}
	return []string{string(f.body.data)}
}

func (f *requestFacts) jsonMatches(path, expected string) bool {
	values, err := f.jsonValues(path)
	return err == nil && restclient.MustMatcher(expected).MatchAny(values)
}

// jsonValues looks up a dot-separated field path in the JSON body. Numeric
// segments index arrays, ["key"] segments name object keys that may hold
// dots, and a "*" segment selects every element of an array,
// so a path can name many values, in document order. A missing field or null
// is no value; strings are their text; other values are compact JSON. A blank
// body has no fields; any other body that is not JSON is an error, and then no
// @json matcher matches.
func (f *requestFacts) jsonValues(path string) ([]string, error) {
	if f.body.unavailable {
		return nil, errBodyUnavailable
	}
	if !f.jsonDone {
		f.jsonDone = true
		if len(bytes.TrimSpace(f.body.data)) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(f.body.data))
			decoder.UseNumber()
			if err := decoder.Decode(&f.json); err != nil {
				f.jsonErr = errBodyNotJSON
			} else if decoder.More() {
				f.jsonErr = errBodyNotJSON
			}
		}
	}
	if f.jsonErr != nil {
		return nil, f.jsonErr
	}
	return selectJSON(f.json, path), nil
}

// selectJSON returns the values at path in a decoded JSON document: strings as
// their text, and numbers, booleans, objects and arrays as compact JSON.
// Missing values and null are left out.
func selectJSON(root any, path string) []string {
	segments, err := restclient.ParseJSONPath(path)
	if err != nil {
		return nil
	}
	nodes := []any{root}
	for _, segment := range segments {
		var next []any
		for _, node := range nodes {
			switch node := node.(type) {
			case map[string]any:
				if child, ok := node[segment.Name]; ok && !segment.Any {
					next = append(next, child)
				}
			case []any:
				if segment.Quoted {
					continue
				}
				if segment.Any {
					next = append(next, node...)
				} else if index, err := strconv.Atoi(segment.Name); err == nil && index >= 0 && index < len(node) {
					next = append(next, node[index])
				}
			}
		}
		nodes = next
	}
	var values []string
	for _, node := range nodes {
		switch node := node.(type) {
		case nil:
		case string:
			values = append(values, node)
		default:
			if text, err := json.Marshal(node); err == nil {
				values = append(values, string(text))
			}
		}
	}
	return values
}

var (
	errBodyUnavailable  = errors.New("request body is larger than 1 MiB or could not be read")
	errBodyNotJSON      = errors.New("request body is not valid JSON")
	errBodyNotForm      = errors.New("request body is not valid URL-encoded form data")
	errBodyNotMultipart = errors.New("request body is not valid multipart/form-data")
)

// requestLocalPort returns the port of the listener that accepted r, or 0 when
// it is unknown (as for requests built in tests without a connection).
func requestLocalPort(r *http.Request) int {
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr); ok {
		return addr.Port
	}
	return 0
}

// portMatches reports whether a route declared for port serves a request that
// arrived on localPort. A route without a port, or a request whose listener
// is unknown, matches any port.
func portMatches(port, localPort int) bool {
	return port == 0 || localPort == 0 || port == localPort
}

func matchPath(pattern string, requestPath string) (map[string]string, bool) {
	if strings.HasSuffix(pattern, "/") && strings.TrimSuffix(requestPath, "index.html") == pattern {
		requestPath = pattern
	}
	patternParts := splitPath(pattern)
	requestParts := splitPath(requestPath)
	values := make(map[string]string)
	// A trailing * or *name segment matches the rest of the path, including
	// nothing; *name captures it, unescaped.
	if last := len(patternParts) - 1; last >= 0 && strings.HasPrefix(patternParts[last], "*") {
		if len(requestParts) < last {
			return nil, false
		}
		if name := patternParts[last][1:]; name != "" {
			rest, err := url.PathUnescape(strings.Join(requestParts[last:], "/"))
			if err != nil {
				return nil, false
			}
			values[name] = rest
		}
		patternParts, requestParts = patternParts[:last], requestParts[:last]
	}
	if len(patternParts) != len(requestParts) {
		return nil, false
	}

	for i := range patternParts {
		if key, ok := strings.CutPrefix(patternParts[i], ":"); ok {
			if key == "" {
				return nil, false
			}
			value, err := url.PathUnescape(requestParts[i])
			if err != nil {
				return nil, false
			}
			values[key] = value
			continue
		}
		literal, err := url.PathUnescape(requestParts[i])
		expected, patternErr := url.PathUnescape(patternParts[i])
		if err != nil || patternErr != nil || expected != literal {
			return nil, false
		}
	}
	return values, true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func queryMatches(expected url.Values, actual url.Values) bool {
	for key, expectedValues := range expected {
		actualValues, ok := actual[key]
		if !ok || len(actualValues) < len(expectedValues) {
			return false
		}
		for i, expectedValue := range expectedValues {
			if actualValues[i] != expectedValue {
				return false
			}
		}
	}
	return true
}

// headerMatches requires every expected header matcher to accept the request's
// values for that header: an exact value, "*" (present and non-blank), "!"
// (missing or blank), or "~pattern" (a full-value regular expression).
func headerMatches(expected http.Header, actual http.Header) bool {
	for name, expectedValues := range expected {
		actualValues := actual.Values(name)
		for _, expectedValue := range expectedValues {
			if !restclient.MustMatcher(expectedValue).Match(actualValues) {
				return false
			}
		}
	}
	return true
}

// queryMatcherMatches applies @query.name matchers to the request query.
func queryMatcherMatches(expected url.Values, actual url.Values) bool {
	for name, expectedValues := range expected {
		for _, expectedValue := range expectedValues {
			if !restclient.MustMatcher(expectedValue).Match(actual[name]) {
				return false
			}
		}
	}
	return true
}

// pathMatcherMatches applies @path.name matchers to the decoded path parameters.
func pathMatcherMatches(expected map[string]string, params map[string]string) bool {
	for name, expectedValue := range expected {
		value, ok := params[name]
		if !ok || !restclient.MustMatcher(expectedValue).Match([]string{value}) {
			return false
		}
	}
	return true
}
