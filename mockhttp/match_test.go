package mockhttp

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func TestParser(t *testing.T) {

	// Find the paths of all input files in the data directory.
	paths, err := filepath.Glob(filepath.Join("testdata", "*.http"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range paths {
		_, filename := filepath.Split(path)
		testName := filename[:len(filename)-len(filepath.Ext(path))]

		// Each path turns into a test: the test name is the filename without the
		// extension.
		t.Run(testName, func(t *testing.T) {
			output, err := parseFixture(path)
			if err != nil {
				t.Fatal(err)
			}

			gold := filepath.Join("testdata", testName+".golden")
			want, err := os.ReadFile(gold)
			if err != nil {
				t.Fatal("error reading good file:", err)
			}

			if !bytes.Equal([]byte(strings.TrimSpace(output)), bytes.TrimSpace(want)) {
				t.Errorf("test %q \n==== got:\n%s\n==== want:\n%s\n", testName, output, want)
			}
		})
	}
}

func parseFixture(path string) (string, error) {
	source, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("error reading source file: %w", err)
	}
	defer source.Close()

	methods, err := restclient.Parse(path, source)
	if err != nil {
		return err.Error(), nil
	}
	return formatFixture(methods)
}

func formatFixture(methods []restclient.Method) (string, error) {
	var output strings.Builder
	for _, method := range methods {
		if output.Len() > 0 {
			output.WriteByte('\n')
		}

		filePath, hasFile := resolveFilePath(&method)
		body, err := renderBody(method, nil, filePath, hasFile)
		if err != nil {
			return "", err
		}
		delay, _ := time.ParseDuration(method.Variables["delay"])

		fmt.Fprintf(&output, "%s: %s %s delay=%s status=%d header=%d body=%d",
			method.Name,
			method.Method,
			method.Path,
			delay,
			method.Status,
			headerSize(responseHeaders(method, nil, filePath)),
			len(body),
		)
	}
	return output.String(), nil
}

func headerSize(headers map[string][]string) int {
	size := 0
	for name, values := range headers {
		for _, value := range values {
			size += len(name) + len(value)
		}
	}
	return size
}

func TestEncodedPathAndIndependentRotation(t *testing.T) {
	input := "### encoded\nGET /x/:id\n\n{{$id}}\n"
	for _, name := range []string{"a1", "a2", "b1", "b2"} {
		input += "### " + name + "\n@header.X-Tenant = " + name[:1] + "\nGET /rotate\n\n" + name + "\n"
	}
	methods, err := restclient.Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, nil)
	for _, tc := range []struct{ url, tenant, want string }{
		{"/x/100%25", "", "100%"}, {"/x/a%252Fb", "", "a%2Fb"}, {"/x/a%2Fb", "", "a/b"},
		{"/x/path?id=query", "", "path"}, {"/rotate", "a", "a1"}, {"/rotate", "b", "b1"},
		{"/rotate", "a", "a2"}, {"/rotate", "b", "b2"},
	} {
		r := httptest.NewRequest("GET", tc.url, nil)
		r.Header.Set("X-Tenant", tc.tenant)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 200 || w.Body.String() != tc.want {
			t.Errorf("%s/%s: %d %q, want %q", tc.url, tc.tenant, w.Code, w.Body.String(), tc.want)
		}
	}
	methods[0].Variables["id"] = "changed"
	snapshot := s.Methods()
	snapshot[0].Headers.Set("X-Mutated", "yes")
	if s.Methods()[0].Headers.Get("X-Mutated") != "" {
		t.Fatal("snapshot aliases server state")
	}
}

func FuzzMatchEscapedPath(f *testing.F) {
	f.Add("a%252Fb")
	f.Add("100%25")
	f.Fuzz(func(t *testing.T, value string) {
		if value == "" {
			return
		}
		values, ok := matchPath("/x/:id", "/x/"+url.PathEscape(value))
		if !ok || values["id"] != value {
			t.Fatalf("path round-trip failed for %q: %v", value, values)
		}
	})
}

func TestEncodedLiteralRouteAndSpecialHeaders(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader("### literal\n@header.Host = example.com\nGET /x/a%2Fb/%3Aid\n\nok"))
	if err != nil {
		t.Fatal(err)
	}
	server := New(methods, nil)
	for _, tc := range []struct {
		url  string
		want int
	}{{"http://example.com/x/a%2Fb/%3Aid", 200}, {"http://example.com/x/a/b/:id", 404}, {"http://other.com/x/a%2Fb/%3Aid", 404}} {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
		if w.Code != tc.want {
			t.Errorf("%s: %d", tc.url, w.Code)
		}
	}
}

func TestMatchPath(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		requestPath string
		wantValues  map[string]string
		wantOK      bool
	}{
		{
			name:        "exact path",
			pattern:     "/users",
			requestPath: "/users",
			wantValues:  map[string]string{},
			wantOK:      true,
		},
		{
			name:        "escaped path parameter",
			pattern:     "/users/:name",
			requestPath: "/users/alice%20smith",
			wantValues:  map[string]string{"name": "alice smith"},
			wantOK:      true,
		},
		{
			name:        "index fallback for directory route",
			pattern:     "/docs/",
			requestPath: "/docs/index.html",
			wantValues:  map[string]string{},
			wantOK:      true,
		},
		{
			name:        "invalid escaped parameter",
			pattern:     "/users/:name",
			requestPath: "/users/%zz",
			wantOK:      false,
		},
		{
			name:        "empty parameter name rejected",
			pattern:     "/users/:",
			requestPath: "/users/42",
			wantOK:      false,
		},
		{
			name:        "different segment count",
			pattern:     "/users/:id",
			requestPath: "/users/42/profile",
			wantOK:      false,
		},
		{
			name:        "literal mismatch",
			pattern:     "/users",
			requestPath: "/projects",
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, ok := matchPath(tt.pattern, tt.requestPath)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(values, tt.wantValues) {
				t.Fatalf("values = %#v, want %#v", values, tt.wantValues)
			}
		})
	}
}

func TestHeaderMatches(t *testing.T) {
	tests := []struct {
		name     string
		expected http.Header
		actual   http.Header
		want     bool
	}{
		{
			name:     "exact match",
			expected: http.Header{"Authorization": []string{"Bearer x"}},
			actual:   http.Header{"Authorization": []string{"Bearer x"}},
			want:     true,
		},
		{
			name:     "missing header",
			expected: http.Header{"Authorization": []string{"Bearer x"}},
			actual:   http.Header{},
			want:     false,
		},
		{
			name:     "wildcard requires non-empty",
			expected: http.Header{"X-Trace": []string{"*"}},
			actual:   http.Header{"X-Trace": []string{"abc"}},
			want:     true,
		},
		{
			name:     "wildcard rejects empty",
			expected: http.Header{"X-Trace": []string{"*"}},
			actual:   http.Header{"X-Trace": []string{""}},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := headerMatches(tt.expected, tt.actual); got != tt.want {
				t.Fatalf("headerMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestQueryMatches(t *testing.T) {
	tests := []struct {
		name     string
		expected url.Values
		actual   url.Values
		want     bool
	}{
		{
			name:     "expected query subset matches",
			expected: url.Values{"type": []string{"cat"}},
			actual:   url.Values{"type": []string{"cat"}, "limit": []string{"10"}},
			want:     true,
		},
		{
			name:     "duplicate expected values match in order",
			expected: url.Values{"tag": []string{"red", "blue"}},
			actual:   url.Values{"tag": []string{"red", "blue", "green"}},
			want:     true,
		},
		{
			name:     "duplicate expected values require order",
			expected: url.Values{"tag": []string{"red", "blue"}},
			actual:   url.Values{"tag": []string{"blue", "red"}},
			want:     false,
		},
		{
			name:     "missing key fails",
			expected: url.Values{"type": []string{"cat"}},
			actual:   url.Values{},
			want:     false,
		},
		{
			name:     "too few duplicate values fails",
			expected: url.Values{"tag": []string{"red", "blue"}},
			actual:   url.Values{"tag": []string{"red"}},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryMatches(tt.expected, tt.actual); got != tt.want {
				t.Fatalf("queryMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}
