package mockhttp

import (
	"github.com/sspencer/mock/restclient"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEncodedPathAndIndependentRotation(t *testing.T) {
	input := "### encoded\nGET /x/:id\n\n{{$id}}\n"
	for _, name := range []string{"a1", "a2", "b1", "b2"} {
		input += "### " + name + "\n# $header.X-Tenant=" + name[:1] + "\nGET /rotate\n\n" + name + "\n"
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
	methods, err := restclient.Parse("test.http", strings.NewReader("### literal\n# $header.Host=example.com\nGET /x/a%2Fb/%3Aid\n\nok"))
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
