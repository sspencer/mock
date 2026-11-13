package mocktest

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

// Route is a route built from Go values, for Add: a test that computes its
// response builds one instead of writing .http text.
type Route struct {
	// Name names the route for AssertCalled; it defaults to the request line,
	// such as "GET /users/:id".
	Name string
	// Method is the request method, such as "GET" (the default), a comma
	// list such as "GET,HEAD", or "*".
	Method string
	// Path is the request path, with :name parameters and a trailing *
	// wildcard as in a request file, and an optional ?query to match.
	Path string
	// Status is the response status; 0 means 200.
	Status int
	// Headers are response headers.
	Headers map[string]string
	// Body is the response body, sent as is unless Template is set.
	Body string
	// JSON, when not nil, is marshaled as the body (replacing Body), and
	// Content-Type defaults to application/json.
	JSON any
	// Template renders Body (or the JSON text) as a request-file template,
	// so {{$path.id}} and the generator functions work.
	Template bool
	// MatchHeader and MatchQuery map request header and query names to
	// matchers in the request-file syntax ("*", "!", "~regex", ">10", ...),
	// as @header.Name and @query.name do.
	MatchHeader map[string]string
	MatchQuery  map[string]string
	// MatchJSON is a JSON document the request body must contain, as @json.
	MatchJSON string
	// Delay delays the response.
	Delay time.Duration
	// Times limits how many requests the route answers; 0 is unlimited.
	Times int
}

// Add serves routes built from Go values in front of every other route until
// the test ends, as Stub does for .http text. It returns the routes' names,
// for AssertCalled, and fails the test if a route is invalid.
func (s *Server) Add(t testing.TB, routes ...Route) []string {
	t.Helper()
	var methods []restclient.Method
	for _, route := range routes {
		method, err := route.method()
		if err != nil {
			t.Fatalf("mocktest: Add %s %s: %v", route.Method, route.Path, err)
		}
		methods = append(methods, method)
	}
	added, err := s.Mock.AddMethods(methods)
	if err != nil {
		t.Fatalf("mocktest: %v", err)
	}
	for _, warning := range added.Warnings {
		t.Logf("mocktest: %s", warning)
	}
	t.Cleanup(func() { s.Mock.RemoveRoutes(added.ID) })
	return added.Routes
}

// method builds the route: everything but the body is written as .http text
// and parsed, so it is checked exactly as a request file is; the body is set
// afterwards, so it needs no escaping.
func (r Route) method() (restclient.Method, error) {
	body := r.Body
	headers := maps.Clone(r.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	if r.JSON != nil {
		data, err := json.Marshal(r.JSON)
		if err != nil {
			return restclient.Method{}, fmt.Errorf("JSON: %w", err)
		}
		body = string(data)
		if !hasHeader(headers, "Content-Type") {
			headers["Content-Type"] = "application/json"
		}
	}
	method := strings.TrimSpace(r.Method)
	if method == "" {
		method = http.MethodGet
	}
	if !strings.HasPrefix(r.Path, "/") {
		return restclient.Method{}, fmt.Errorf("path %q must start with /", r.Path)
	}
	var text strings.Builder
	text.WriteString("### ")
	if r.Name != "" {
		text.WriteString(r.Name)
	}
	text.WriteString("\n")
	values := map[string]string{}
	if r.Status != 0 {
		values["status"] = strconv.Itoa(r.Status)
	}
	if r.Delay > 0 {
		values["delay"] = r.Delay.String()
	}
	if r.Times > 0 {
		values["times"] = strconv.Itoa(r.Times)
	}
	if !r.Template {
		values["template"] = "off"
	}
	if r.MatchJSON != "" {
		values["json"] = r.MatchJSON
	}
	for name, matcher := range r.MatchHeader {
		values["header."+name] = matcher
	}
	for name, matcher := range r.MatchQuery {
		values["query."+name] = matcher
	}
	for _, name := range slices.Sorted(maps.Keys(values)) {
		if err := oneLine(name, values[name]); err != nil {
			return restclient.Method{}, err
		}
		fmt.Fprintf(&text, "@%s = %s\n", name, values[name])
	}
	if err := oneLine("path", r.Path); err != nil {
		return restclient.Method{}, err
	}
	fmt.Fprintf(&text, "%s %s\n", method, r.Path)
	for _, name := range slices.Sorted(maps.Keys(headers)) {
		if err := oneLine(name, name+headers[name]); err != nil {
			return restclient.Method{}, err
		}
		fmt.Fprintf(&text, "%s: %s\n", name, headers[name])
	}
	methods, err := restclient.Parse("route.http", strings.NewReader(text.String()))
	if err != nil {
		return restclient.Method{}, err
	}
	if len(methods) != 1 {
		return restclient.Method{}, fmt.Errorf("built %d routes, want 1", len(methods))
	}
	methods[0].Body = body
	return methods[0], nil
}

func oneLine(name, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must not contain a line break", name)
	}
	return nil
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}
