package mockhttp

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestJSONDocumentMatcher(t *testing.T) {
	methods, err := restclient.Parse("body.http", strings.NewReader(`### Card
@json = {"type": "card", "amount": 100, "meta": {"currency": "EUR"}}
POST /pay

card

### Pair
@json = {"items": [{"sku": "A1"}, {"sku": "B2"}]}
POST /pay

pair

### Card with path matcher
@json = {"type": "cash"}
@json.amount = >10
POST /pay

cash

### Fallback
POST /pay

fallback
`))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct{ body, want string }{
		{`{"type":"card","amount":100,"meta":{"currency":"EUR"}}`, "card"},
		{`{"meta":{"currency":"EUR","x":1},"extra":true,"amount":100.0,"type":"card"}`, "card"},
		{`{"type":"card","amount":"100","meta":{"currency":"EUR"}}`, "fallback"}, // a string is not a number
		{`{"type":"card","amount":100}`, "fallback"},
		{`{"items":[{"sku":"A1","qty":1},{"sku":"B2"}]}`, "pair"},
		{`{"items":[{"sku":"B2"},{"sku":"A1"}]}`, "fallback"},              // order matters
		{`{"items":[{"sku":"A1"},{"sku":"B2"},{"sku":"C3"}]}`, "fallback"}, // and length
		{`{"type":"cash","amount":20}`, "cash"},
		{`{"type":"cash","amount":5}`, "fallback"},
		{`not json`, "fallback"},
		{``, "fallback"},
	}
	for _, tt := range tests {
		if code, body := serveBody(t, s, "/pay", tt.body); code != 200 || body != tt.want {
			t.Errorf("POST %q = %d %q, want %q", tt.body, code, body, tt.want)
		}
	}
}

func TestJSONDocumentReasons(t *testing.T) {
	methods, err := restclient.Parse("body.http", strings.NewReader(`### Card
@json = {"type": "card", "amount": 100, "items": [1, 2], "user": {"password": "x"}}
POST /pay

card
`))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for body, want := range map[string]string{
		`{"type":"cash","amount":100,"items":[1,2],"user":{"password":"x"}}`: `JSON type: expected "card"; received "cash"`,
		`{"type":"card"}`:                          `JSON amount: expected 100; received nothing`,
		`{"type":"card","amount":100,"items":[1]}`: `JSON items: expected 2 elements; received 1`,
		`{"type":"card","amount":100,"items":{}}`:  `JSON items: expected an array; received an object`,
		`[1]`: `JSON body: expected an object; received an array`,
		``:    `JSON body: expected a JSON body; received none`,
		`{"type":"card","amount":100,"items":[1,2],"user":{"password":"y"}}`: `JSON user.password: expected "x"; received "y"`,
	} {
		serveBody(t, s, "/pay", body)
		reasons := strings.Join(s.events[len(s.events)-1].Match.Candidates[0].Reasons, "\n")
		if !strings.Contains(reasons, want) {
			t.Errorf("POST %q reasons = %q, want %q", body, reasons, want)
		}
	}
}

func TestJSONDocumentShadowing(t *testing.T) {
	shadowWarnings := func(text string) int {
		t.Helper()
		methods, err := restclient.Parse("body.http", strings.NewReader(text))
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_ = New(methods, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		return strings.Count(buf.String(), "route can never match")
	}
	if n := shadowWarnings("### Any\nPOST /pay\n\n### Card\n@json = {\"type\": \"card\"}\nPOST /pay\n"); n != 1 {
		t.Errorf("general before @json: %d warnings, want 1", n)
	}
	if n := shadowWarnings("### Cash\n@json = {\"type\": \"cash\"}\nPOST /pay\n\n### Card\n@json = {\"type\": \"card\"}\nPOST /pay\n"); n != 0 {
		t.Errorf("different @json documents: %d warnings, want 0", n)
	}
}
