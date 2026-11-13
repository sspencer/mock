package restclient

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseDelay(t *testing.T) {
	tests := []struct {
		raw       string
		low, high time.Duration
		ok        bool
	}{
		{"250ms", 250 * time.Millisecond, 0, true},
		{"100ms..800ms", 100 * time.Millisecond, 800 * time.Millisecond, true},
		{" 1s .. 2s ", time.Second, 2 * time.Second, true},
		{"0s..0s", 0, 0, true},
		{"-1s", 0, 0, false},
		{"800ms..100ms", 0, 0, false},
		{"1s..", 0, 0, false},
		{"soon", 0, 0, false},
	}
	for _, tt := range tests {
		delay, err := ParseDelay(tt.raw)
		if (err == nil) != tt.ok || delay.Min != tt.low || delay.Max != tt.high || delay.Sigma != 0 {
			t.Errorf("ParseDelay(%q) = %+v, %v", tt.raw, delay, err)
		}
	}
}

func TestParseFault(t *testing.T) {
	tests := []struct {
		raw   string
		fault string
		rate  float64
		ok    bool
	}{
		{"reset", "reset", 1, true},
		{"Truncate", "truncate", 1, true},
		{"reset 25%", "reset", 0.25, true},
		{"truncate 0%", "truncate", 0, true},
		{"reset 25", "", 0, false},
		{"reset 101%", "", 0, false},
		{"explode", "", 0, false},
		{"", "", 0, false},
	}
	for _, tt := range tests {
		fault, rate, err := ParseFault(tt.raw)
		if (err == nil) != tt.ok || fault != tt.fault || rate != tt.rate {
			t.Errorf("ParseFault(%q) = %q, %v, %v", tt.raw, fault, rate, err)
		}
	}
}

func TestResponseControlVariables(t *testing.T) {
	methods, err := Parse("api.http", strings.NewReader(`### A
@delay = 100ms..300ms
@stream = 50ms
@fault = truncate 10%
@template = off
@seed = demo
GET /a

{{$id}}`))
	if err != nil {
		t.Fatal(err)
	}
	m := methods[0]
	if m.Delay != 100*time.Millisecond || m.DelayMax != 300*time.Millisecond || m.Stream != 50*time.Millisecond ||
		m.Fault != "truncate" || m.FaultRate != 0.1 || m.Template != "off" {
		t.Fatalf("method = %+v", m)
	}
	if seed, _ := m.Variable("seed"); seed != "demo" {
		t.Fatalf("seed = %q", seed)
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Fatalf("control variables reported unused: %+v", unused)
	}
}

func TestInvalidResponseControlVariables(t *testing.T) {
	for _, line := range []string{"@stream = fast", "@stream = -1s", "@fault = explode", "@template = maybe", "@trim = sometimes", "@seed =", "@delay = 2s..1s"} {
		_, err := Parse("api.http", strings.NewReader("### A\n"+line+"\nGET /a\n\nok"))
		if err == nil || !strings.HasPrefix(err.Error(), "api.http:2:") {
			t.Errorf("%s: err = %v", line, err)
		}
	}
}

func TestTrimOffKeepsLineEndings(t *testing.T) {
	methods, err := Parse("api.http", strings.NewReader("### A\r\n@trim = off\r\nGET /a\r\n\r\n  x\r\n\ny\r\n\r\n### B\nGET /b\n\n\nz\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0].Body; got != "  x\r\n\ny\r\n\r\n" {
		t.Fatalf("exact body = %q", got)
	}
	if got := methods[1].Body; got != "\nz" {
		t.Fatalf("trimmed body = %q", got)
	}
}

func TestFileDependenciesSkipTemplatedPaths(t *testing.T) {
	methods, err := Parse("api.http", strings.NewReader("### A\nGET /a/:id\n\n< ./users/{{$id}}.json\n\n### B\nGET /b\n\n< ./b.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := FileDependencies(methods); !slices.Equal(got, []string{"./b.json"}) {
		t.Fatalf("dependencies = %q", got)
	}
}

func TestTags(t *testing.T) {
	methods, err := Parse("tags.http", strings.NewReader("@tag = billing\n\n### Invoices\nGET /invoices\n\nok\n\n### Slow report\n@tag = Reports, slow, reports\nGET /report\n\nok\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(methods[0].Tags, []string{"billing"}) || !slices.Equal(methods[1].Tags, []string{"Reports", "slow"}) {
		t.Fatalf("tags = %v / %v", methods[0].Tags, methods[1].Tags)
	}
	if !methods[1].HasTag("reports") || methods[1].HasTag("billing") {
		t.Errorf("HasTag ignores case and a section tag replaces the file one")
	}
	for _, bad := range []string{"", "a b", "billing,", "-x"} {
		if _, err := Parse("bad.http", strings.NewReader("### A\n@tag = "+bad+"\nGET /a\n")); err == nil || !strings.Contains(err.Error(), "@tag") {
			t.Errorf("@tag = %q: err = %v, want a @tag error", bad, err)
		}
	}
}
