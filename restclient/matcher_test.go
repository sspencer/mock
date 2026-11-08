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

func TestParseBodyMatcherDotMatchesNewline(t *testing.T) {
	m, err := ParseBodyMatcher("~.*hello.*")
	if err != nil {
		t.Fatal(err)
	}
	if m.Raw != "~.*hello.*" || !m.Match([]string{"a\nhello\nb"}) {
		t.Fatalf("body matcher = %+v", m)
	}
	if MustMatcher("~.*hello.*").Match([]string{"a\nhello\nb"}) {
		t.Fatal("header matcher should not let . match newlines")
	}
}

func TestParseBodyAndJSONMatchers(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`### Login
@body = ~.*alice.*
@json.user.roles.0 = admin
@json.items.*.sku = ~A[0-9]+
POST /login

ok
`))
	if err != nil {
		t.Fatal(err)
	}
	m := methods[0]
	if m.MatchBody != "~.*alice.*" || m.MatchJSON["user.roles.0"] != "admin" || m.MatchJSON["items.*.sku"] != "~A[0-9]+" {
		t.Fatalf("MatchBody = %q, MatchJSON = %v", m.MatchBody, m.MatchJSON)
	}
	if _, ok := m.Variables["body"]; ok {
		t.Fatal("@body stored as a variable")
	}
}

func TestMatcherCaptures(t *testing.T) {
	m := MustMatcher("~Bearer (?P<token>[a-z]+)(?P<rest>.*)")
	got := m.Captures([]string{"Basic x", "Bearer abc123"})
	if got["token"] != "abc" || got["rest"] != "123" {
		t.Fatalf("Captures = %v", got)
	}
	if MustMatcher("~[a-z]+").Captures([]string{"abc"}) != nil || MustMatcher("abc").Captures([]string{"abc"}) != nil {
		t.Fatal("captures without named groups")
	}
	if names := CaptureNames("~(?P<a>x)(y)(?P<b>z)"); len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("CaptureNames = %v", names)
	}
}

func TestParseWildcardAndForm(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`### Files
@path.path = ~.*\.txt
@form.kind = text
POST /files/*path

ok
`))
	if err != nil {
		t.Fatal(err)
	}
	if methods[0].MatchPath["path"] != `~.*\.txt` || methods[0].MatchForm.Get("kind") != "text" {
		t.Fatalf("MatchPath = %v, MatchForm = %v", methods[0].MatchPath, methods[0].MatchForm)
	}
	if got := PathParams("/a/:id/b/*rest"); len(got) != 2 || got[0] != "id" || got[1] != "rest" {
		t.Fatalf("PathParams = %v", got)
	}
	if got := PathParams("/a/%3Aid/%2Arest"); len(got) != 0 {
		t.Fatalf("escaped segments are literal, got %v", got)
	}
}

func TestMatcherMatchAny(t *testing.T) {
	tests := []struct {
		raw    string
		values []string
		want   bool
	}{
		{"admin", []string{"dev", "admin"}, true},
		{"admin", []string{"dev"}, false},
		{"admin", nil, false},
		{"~A[0-9]+", []string{"B1", "A2"}, true},
		{"*", []string{"", "x"}, true}, // any non-blank value, not only the first
		{"*", []string{"", " "}, false},
		{"*", nil, false},
		{"!", nil, true},
		{"!", []string{"", " "}, true},
		{"!", []string{"", "x"}, false}, // every value must be blank
	}
	for _, tt := range tests {
		if got := MustMatcher(tt.raw).MatchAny(tt.values); got != tt.want {
			t.Errorf("MustMatcher(%q).MatchAny(%q) = %v, want %v", tt.raw, tt.values, got, tt.want)
		}
	}
}

func TestNegatedMatchers(t *testing.T) {
	tests := []struct {
		raw    string
		values []string
		want   bool
	}{
		{"!guest", []string{"admin"}, true},
		{"!guest", []string{"guest"}, false},
		{"!guest", []string{"admin", "guest"}, false}, // no value may be excluded
		{"!guest", nil, false},                        // present but not X needs a value
		{"!guest", []string{" "}, false},
		{"!~test-.*", []string{"prod-1"}, true},
		{"!~test-.*", []string{"test-1"}, false},
		{"!~test-.*", nil, false},
		{"!~test-.*", []string{"xtest-1"}, true}, // anchored like ~pattern
	}
	for _, tt := range tests {
		m, err := ParseMatcher(tt.raw)
		if err != nil {
			t.Fatalf("ParseMatcher(%q) error = %v", tt.raw, err)
		}
		if !m.Negated() {
			t.Fatalf("ParseMatcher(%q) is not negated: %+v", tt.raw, m)
		}
		if got := m.Match(tt.values); got != tt.want {
			t.Errorf("ParseMatcher(%q).Match(%q) = %v, want %v", tt.raw, tt.values, got, tt.want)
		}
		if got := m.MatchAny(tt.values); got != tt.want {
			t.Errorf("ParseMatcher(%q).MatchAny(%q) = %v, want %v", tt.raw, tt.values, got, tt.want)
		}
	}
	if _, err := ParseMatcher("!~("); err == nil {
		t.Fatal("invalid negated regexp parsed")
	}
	if _, err := ParseMatcher("!~"); err == nil {
		t.Fatal("empty negated regexp parsed")
	}
	if m := MustBodyMatcher("!~.*secret.*"); m.Match([]string{"a\nsecret\nb"}) || !m.Match([]string{"a\nb"}) {
		t.Fatal("negated body matcher should let . match newlines")
	}
}

func TestParseCookieMatcher(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("@cookie.consent = yes\n\n### Me\n@cookie.session = *\nGET /me\n\nok\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0].MatchCookies; got.Get("session") != "*" || got.Get("consent") != "yes" {
		t.Fatalf("MatchCookies = %v", got)
	}
	if _, err := Parse("test.http", strings.NewReader("### Me\n@cookie. = *\nGET /me\n")); err == nil || !strings.Contains(err.Error(), "requires a cookie name") {
		t.Fatalf("error = %v", err)
	}
	if err := CheckVariableOverride("cookie.session"); err == nil {
		t.Fatal("cookie matcher accepted as a --var override")
	}
}

func TestParseXMLMatcher(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("### Order\n@xml.order.item = pen\n@xml.order[\"@id\"] = ~[0-9]+\nPOST /o\n\nok\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0].MatchXML; got["order.item"] != "pen" || got[`order["@id"]`] != "~[0-9]+" {
		t.Fatalf("MatchXML = %v", got)
	}
	for _, bad := range []string{`@xml.order["@id"].x = 1`, `@xml.order["@"] = 1`, `@xml.a..b = 1`} {
		if _, err := Parse("test.http", strings.NewReader("### Bad\n"+bad+"\nPOST /o\n")); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
}

func TestParseProxy(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("@base = https://api.example.com\n### P\n@proxy = {{base}}/v1/{{$path.rest}}\nGET /p/*rest\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0].Proxy; got != "https://api.example.com/v1/{{$path.rest}}" {
		t.Fatalf("Proxy = %q", got)
	}
	for _, bad := range []string{"api.example.com", "ftp://x", "https://", "https://{{$query.h}}/x", "https://x/#y", "https://u:p@x"} {
		if _, err := Parse("test.http", strings.NewReader("### P\n@proxy = "+bad+"\nGET /p\n")); err == nil || !strings.Contains(err.Error(), "@proxy") {
			t.Errorf("@proxy = %s: err = %v", bad, err)
		}
	}
}

func TestParseCompress(t *testing.T) {
	for value, want := range map[string]string{"gzip": "gzip", "GZIP": "gzip", "auto": "auto", "deflate": "deflate", "off": ""} {
		methods, err := Parse("t.http", strings.NewReader("### C\n@compress = "+value+"\nGET /c\n"))
		if err != nil || methods[0].Compress != want {
			t.Errorf("@compress = %s: %q, %v", value, methods[0].Compress, err)
		}
	}
	if _, err := Parse("t.http", strings.NewReader("### C\n@compress = br\nGET /c\n")); err == nil {
		t.Error("@compress = br parsed")
	}
}
