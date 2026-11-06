package restclient

import (
	"strings"
	"testing"
)

func TestMatcherMatch(t *testing.T) {
	tests := []struct {
		raw    string
		values []string
		want   bool
	}{
		{"secret", []string{"secret"}, true},
		{"secret", []string{"other", "secret"}, true},
		{"secret", []string{"Secret"}, false},
		{"secret", nil, false},
		{"*", []string{"x"}, true},
		{"*", []string{"  "}, false},
		{"*", nil, false},
		{"!", nil, true},
		{"!", []string{""}, true},
		{"!", []string{"x"}, false},
		{"~Bearer [a-z]+", []string{"Bearer abc"}, true},
		{"~Bearer [a-z]+", []string{"Bearer abc123"}, false}, // anchored at both ends
		{"~Bearer [a-z]+", []string{"xBearer abc"}, false},
		{"~[0-9]+", []string{"x", "42"}, true},
		{"~[0-9]+", nil, false},
		{"~a|b", []string{"b"}, true}, // alternation is grouped before anchoring
		{"~a|b", []string{"ab"}, false},
	}
	for _, tt := range tests {
		m, err := ParseMatcher(tt.raw)
		if err != nil {
			t.Fatalf("ParseMatcher(%q) error = %v", tt.raw, err)
		}
		if got := m.Match(tt.values); got != tt.want {
			t.Errorf("ParseMatcher(%q).Match(%q) = %v, want %v", tt.raw, tt.values, got, tt.want)
		}
	}
}

func TestMustMatcherInvalidRegexpNeverMatches(t *testing.T) {
	if MustMatcher("~(").Match([]string{"("}) {
		t.Fatal("invalid regexp matched")
	}
}

func TestParseRequestMatchers(t *testing.T) {
	input := `@path.id = ~[0-9]+
@query.debug = !

### Numeric user
@header.Authorization = ~Bearer .+
@query.page = ~[0-9]+
GET /users/:id

ok

### Listing has no :id
GET /users

ok
`
	methods, err := Parse("test.http", strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	user, list := methods[0], methods[1]
	if got := user.MatchHeaders.Get("Authorization"); got != "~Bearer .+" {
		t.Fatalf("Authorization matcher = %q", got)
	}
	if got := user.MatchQuery.Get("page"); got != "~[0-9]+" {
		t.Fatalf("page matcher = %q", got)
	}
	if got := user.MatchQuery.Get("debug"); got != "!" {
		t.Fatalf("file-scoped debug matcher = %q", got)
	}
	if got := user.MatchPath["id"]; got != "~[0-9]+" {
		t.Fatalf("id matcher = %q", got)
	}
	if len(list.MatchPath) != 0 {
		t.Fatalf("file-scoped @path.id should not apply without :id, got %v", list.MatchPath)
	}
	for _, method := range methods {
		for name := range method.Variables {
			if isMatcherName(name) {
				t.Fatalf("matcher %q stored as a variable", name)
			}
		}
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Fatalf("matchers reported as unused variables: %v", unused)
	}
}
