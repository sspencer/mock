package mockhttp

import (
	"net/url"
	"reflect"
	"testing"
)

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
