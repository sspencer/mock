package restclient

import (
	"strings"
	"testing"
)

func TestJSONDocumentDirective(t *testing.T) {
	methods, err := Parse("a.http", strings.NewReader("@kind = card\n\n### Pay\n@json = { \"type\": \"{{kind}}\", \"amount\": 100 }\nPOST /pay\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0].MatchJSONBody; got != `{"type":"card","amount":100}` {
		t.Fatalf("MatchJSONBody = %q", got)
	}
	if _, ok := methods[0].Variables["json"]; ok {
		t.Fatal("@json became a variable")
	}
	for _, bad := range []string{`card`, `{"type":`, `{"a":1} {"b":2}`, `"text"`} {
		_, err := Parse("a.http", strings.NewReader("### Pay\n@json = "+bad+"\nPOST /pay\n"))
		if err == nil || !strings.Contains(err.Error(), "a.http:2: section \"Pay\": @json needs a JSON object or array") {
			t.Errorf("@json = %s: err = %v", bad, err)
		}
	}
}

func TestJSONContains(t *testing.T) {
	decode := func(text string) any {
		value, err := DecodeJSON([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	tests := []struct {
		actual, expected string
		ok               bool
		path             string
	}{
		{`{"a":1,"b":2}`, `{"a":1}`, true, ""},
		{`{"a":1.0}`, `{"a":1}`, true, ""},
		{`{"a":{"b":[1,{"c":true,"d":0}]}}`, `{"a":{"b":[1,{"c":true}]}}`, true, ""},
		{`{"a":{"b":[1,{"c":false}]}}`, `{"a":{"b":[1,{"c":true}]}}`, false, "a.b.1.c"},
		{`{"a.b":1}`, `{"a.b":2}`, false, `["a.b"]`},
		{`{"a":null}`, `{"a":null}`, true, ""},
		{`{}`, `{"a":null}`, false, "a"},
		{`[1,2]`, `[1,2]`, true, ""},
		{`[1,2]`, `[2,1]`, false, "0"},
	}
	for _, tt := range tests {
		ok, mismatch := JSONContains(decode(tt.actual), decode(tt.expected))
		if ok != tt.ok || mismatch.Path != tt.path {
			t.Errorf("JSONContains(%s, %s) = %v %+v, want %v at %q", tt.actual, tt.expected, ok, mismatch, tt.ok, tt.path)
		}
	}
}
