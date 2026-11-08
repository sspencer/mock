package restclient

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseJSONPath(t *testing.T) {
	tests := []struct {
		path string
		want []JSONSegment
	}{
		{"user.name", []JSONSegment{{Name: "user"}, {Name: "name"}}},
		{"items.*.sku", []JSONSegment{{Name: "items"}, {Name: "*", Any: true}, {Name: "sku"}}},
		{`meta["user.id"]`, []JSONSegment{{Name: "meta"}, {Name: "user.id", Quoted: true}}},
		{`meta.["user.id"].x`, []JSONSegment{{Name: "meta"}, {Name: "user.id", Quoted: true}, {Name: "x"}}},
		{`a["*"]["0"]`, []JSONSegment{{Name: "a"}, {Name: "*", Quoted: true}, {Name: "0", Quoted: true}}},
		{`["k.v"]`, []JSONSegment{{Name: "k.v", Quoted: true}}},
	}
	for _, tt := range tests {
		got, err := ParseJSONPath(tt.path)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseJSONPath(%q) = %+v, %v; want %+v", tt.path, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "a..b", "a.", `a["b`, `a[""]`, `a["b c"]`, `a["b"]c`, "a b", "a/b"} {
		if _, err := ParseJSONPath(bad); err == nil {
			t.Errorf("ParseJSONPath(%q) error = nil", bad)
		}
	}
}

func TestParseQuotedJSONMatcher(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("### Trace\n@json.headers[\"x.trace\"] = *\n@json.items.*.sku = A1\nPOST /t\n\n{{$json.headers[\"x.trace\"]}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0].MatchJSON[`headers["x.trace"]`]; got != "*" {
		t.Fatalf("MatchJSON = %v", methods[0].MatchJSON)
	}
	if _, err := Parse("test.http", strings.NewReader("### Bad\n@json.a[\"b\"]c = 1\nPOST /t\n")); err == nil {
		t.Fatal("malformed quoted path parsed")
	}
	if m := PlaceholderPattern.FindStringSubmatch(`{{$json.a["b.c"].d}}`); m == nil || m[2] != `json.a["b.c"].d` {
		t.Fatalf("placeholder match = %q", m)
	}
}
