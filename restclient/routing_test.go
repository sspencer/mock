package restclient

import (
	"strings"
	"testing"
)

func TestParseMethodLists(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`GET,HEAD /bare

### Health
GET,head,OPTIONS,GET /health

ok

### Maintenance
* /maintenance

down
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0]; got.Method != "GET,HEAD" || got.Name != "GET,HEAD /bare" {
		t.Errorf("unnamed method list = %q named %q", got.Method, got.Name)
	}
	if got := methods[1].Method; got != "GET,HEAD,OPTIONS" {
		t.Errorf("method list = %q, want GET,HEAD,OPTIONS", got)
	}
	if !methods[1].Accepts("HEAD") || methods[1].Accepts("POST") || methods[1].SampleMethod() != "GET" {
		t.Errorf("Accepts/SampleMethod wrong for %q", methods[1].Method)
	}
	if got := methods[2].Method; got != AnyMethod || !methods[2].Accepts("DELETE") || methods[2].Methods() != nil || methods[2].SampleMethod() != "GET" {
		t.Errorf("any method = %q, Methods() = %v", got, methods[2].Methods())
	}
	if got := (Method{Method: "PUT"}); !got.Accepts("PUT") || got.Accepts("GET") || len(got.Methods()) != 1 {
		t.Errorf("single method Accepts/Methods wrong")
	}

	for line, want := range map[string]string{
		"GET,FOO /x":  `unrecognized HTTP method "FOO"`,
		"GET,,PUT /x": "invalid method list",
		"GET,* /x":    "invalid method list",
		"GET, PUT /x": "invalid HTTP request line",
	} {
		_, err := Parse("test.http", strings.NewReader("### route\n"+line+"\n"))
		if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "test.http:2:") {
			t.Errorf("%q error = %v, want %q", line, err, want)
		}
	}
}

func TestParseFallback(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("### Not found\n@fallback = true\nGET /*\n\nnope\n\n### Off\n@fallback = FALSE\nGET /x\n"))
	if err != nil || !methods[0].Fallback || methods[1].Fallback {
		t.Fatalf("Parse() = %+v, %v", methods, err)
	}
	_, err = Parse("test.http", strings.NewReader("### Not found\n@fallback = yes\nGET /*\n"))
	if err == nil || !strings.Contains(err.Error(), "test.http:2:") || !strings.Contains(err.Error(), "@fallback must be true or false") {
		t.Errorf("bad @fallback error = %v", err)
	}
}

func TestParseChance(t *testing.T) {
	for raw, want := range map[string]float64{"20%": 0.2, " 0.5% ": 0.005, "100%": 0} {
		if got, err := ParseChance(raw); err != nil || got != want {
			t.Errorf("ParseChance(%q) = %v, %v; want %v", raw, got, err, want)
		}
	}
	for _, raw := range []string{"", "20", "0%", "-5%", "101%", "often"} {
		if _, err := ParseChance(raw); err == nil {
			t.Errorf("ParseChance(%q) error = nil", raw)
		}
	}
	methods, err := Parse("test.http", strings.NewReader("### Flaky\n@match.chance = 25%\nGET /x\n"))
	if err != nil || methods[0].Chance != 0.25 {
		t.Fatalf("Parse() = %+v, %v", methods, err)
	}
	if len(UnusedVariables(methods)) != 0 {
		t.Errorf("@match.chance reported unused: %v", UnusedVariables(methods))
	}
	for source, want := range map[string]string{
		"### Flaky\n@match.chance = sometimes\nGET /x\n": "@match.chance must be a percentage",
		"### Flaky\n@match.odds = 5%\nGET /x\n":          "unknown directive @match.odds",
	} {
		if _, err := Parse("test.http", strings.NewReader(source)); err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "test.http:2:") {
			t.Errorf("error = %v, want %q", err, want)
		}
	}
	if IsDefaultVariable("match.chance") || IsDefaultVariable("fallback") {
		t.Error("an environment may not set @match.chance or @fallback")
	}
}

func TestParseTimes(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("### X\n@times = 3\nGET /x\n"))
	if err != nil || methods[0].Times != 3 {
		t.Fatalf("Parse() = %+v, %v", methods, err)
	}
	for _, bad := range []string{"0", "-1", "two", ""} {
		if _, err := Parse("test.http", strings.NewReader("### X\n@times = "+bad+"\nGET /x\n")); err == nil || !strings.Contains(err.Error(), "test.http:2:") {
			t.Errorf("@times = %q error = %v", bad, err)
		}
	}
	if len(UnusedVariables(methods)) != 0 || IsDefaultVariable("times") {
		t.Error("@times is a control variable")
	}
}
