package mockhttp

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

const allValuesFixture = `### Two tags
@query.tag[] = #2
GET /count

two {{$query.tag[]}}

### Known tags
@query.tag[] = ~a|b
GET /tags

known {{$query.tag[]}}

### Unknown tags
GET /tags

unknown

### Text only
@header.Accept[] = ~text/.*
GET /accept

text

### Other accept
GET /accept

other
`

func TestAllValuesMatchers(t *testing.T) {
	methods, err := restclient.Parse("all.http", strings.NewReader(allValuesFixture))
	if err != nil {
		t.Fatal(err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct {
		target string
		accept []string
		want   string
	}{
		{"/tags?tag=a&tag=b", nil, `known ["a","b"]`},
		{"/tags?tag=a&tag=c", nil, "unknown"},
		{"/tags?tag[]=b&tag=a", nil, `known ["a","b"]`},
		{"/tags", nil, "unknown"},
		{"/count?tag=x&tag[]=y", nil, `two ["x","y"]`},
		{"/accept", []string{"text/html", "text/plain"}, "text"},
		{"/accept", []string{"text/html", "application/json"}, "other"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.target, nil)
		for _, value := range tt.accept {
			req.Header.Add("Accept", value)
		}
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if got := strings.TrimSpace(rec.Body.String()); got != tt.want {
			t.Errorf("%s %v: got %q, want %q", tt.target, tt.accept, got, tt.want)
		}
	}

	explained := server.Explain(httptest.NewRequest("GET", "/count?tag=x", nil))
	var reasons []string
	for _, miss := range explained.Candidates {
		reasons = append(reasons, miss.Reasons...)
	}
	if !strings.Contains(strings.Join(reasons, "\n"), "Query tag[]: must have exactly 2 values; received 1") {
		t.Fatalf("reasons %q", reasons)
	}
}

func TestMatchAll(t *testing.T) {
	for _, tt := range []struct {
		matcher string
		values  []string
		want    bool
	}{
		{"*", []string{"a", "b"}, true},
		{"*", []string{"a", ""}, false},
		{"*", nil, false},
		{"!", nil, true},
		{"!", []string{""}, true},
		{"!", []string{"a"}, false},
		{"!x", []string{"a", "b"}, true},
		{"!x", []string{"a", "x"}, false},
		{">1", []string{"2", "3"}, true},
		{">1", []string{"2", "0"}, false},
		{"#>=2", []string{"a", "b"}, true},
		{"#0", nil, true},
	} {
		if got := restclient.MustMatcher(tt.matcher).MatchAll(tt.values); got != tt.want {
			t.Errorf("%q.MatchAll(%q) = %v, want %v", tt.matcher, tt.values, got, tt.want)
		}
	}
}
