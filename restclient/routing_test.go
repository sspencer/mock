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

func TestMatchHost(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("### Billing\n@match.host = ~.*\\.example\\.test\nGET /x\n"))
	if err != nil {
		t.Fatal(err)
	}
	if methods[0].MatchHost != `~.*\.example\.test` {
		t.Errorf("MatchHost = %q", methods[0].MatchHost)
	}
	if len(UnusedVariables(methods)) != 0 {
		t.Errorf("@match.host reported unused: %v", UnusedVariables(methods))
	}
	for source, want := range map[string]string{
		"### A\n@match.host =\nGET /x\n":        "@match.host",
		"### A\n@match.host = ~(\nGET /x\n":     "@match.host",
		"### A\n@match.hots = a.test\nGET /x\n": "did you mean @match.chance or @match.host?",
	} {
		if _, err := Parse("test.http", strings.NewReader(source)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want %q", source, err, want)
		}
	}
}

func TestScenarioDirectives(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader("@scenario = order\n\n### Pay\n@state = started\n@next-state = paid\nPOST /pay\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m := methods[0]; m.Scenario != "order" || m.State != "started" || m.NextState != "paid" {
		t.Errorf("method = %q %q %q", m.Scenario, m.State, m.NextState)
	}
	if len(UnusedVariables(methods)) != 0 || IsDefaultVariable("scenario") {
		t.Errorf("scenario directives treated as variables: %v", UnusedVariables(methods))
	}
	for source, want := range map[string]string{
		"### A\n@state = paid\nGET /x\n":                    "need a @scenario",
		"### A\n@scenario = my order\nGET /x\n":             "@scenario needs one name without spaces",
		"### A\n@scenario = order\n@next-state =\nGET /x\n": "@next-state needs one name",
	} {
		if _, err := Parse("test.http", strings.NewReader(source)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) error = %v, want %q", source, err, want)
		}
	}
}
