package mockhttp

import (
	"bytes"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

// placeholderWarnings loads one .http section and returns the warnings New logs.
func placeholderWarnings(t *testing.T, section string) string {
	t.Helper()
	methods, err := restclient.Parse("fixture.http", strings.NewReader(section))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var logs bytes.Buffer
	New(methods, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return logs.String()
}

func TestWarnInvalidGeneratorArguments(t *testing.T) {
	const warning = "invalid placeholder arguments preserved literally"
	tests := []struct {
		name    string
		section string
		// want is the error text expected in the warning; empty means no warning.
		want string
	}{
		{"randomInt missing max", "### R\nGET /r\n\n{{$randomInt 5}}", "expected min and max"},
		{"randomInt not integers", "### R\nGET /r\n\n{{$randomInt a b}}", "min and max must be integers"},
		{"randomInt reversed", "### R\nGET /r\n\n{{$randomInt 10 1}}", "max must be greater than min"},
		{"datetime unknown format", "### R\nGET /r\n\n{{$datetime unix}}", `unknown format \"unix\"`},
		{"timestamp bad offset", "### R\nGET /r\n\n{{$timestamp soon d}}", `offset \"soon\" must be an integer`},
		{"timestamp bad option", "### R\nGET /r\n\n{{$timestamp -1 days}}", `unknown offset option \"days\"`},
		{"in a response header", "### R\nGET /r\nX-N: {{$randomInt 9 2}}\n\nok", "max must be greater than min"},
		{"in a repeat block", "### R\nGET /r\n\n{{#repeat 2}}{{$randomInt x}}{{/repeat}}", "expected min and max"},
		{"in a file path", "### R\nGET /r\n\n< ./{{$randomInt 3 1}}.json", "max must be greater than min"},
		{"valid arguments", "### R\nGET /r\n\n{{$randomInt 1 10}} {{$datetime iso8601 -1 d}} {{$timestamp}} {{$localDatetime rfc1123}}", ""},
		{"templating off", "### R\n@template = off\nGET /r\n\n{{$randomInt 10 1}}", ""},
		{"default filter", "### R\nGET /r\n\n{{$randomInt 10 1 | default 0}}", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := placeholderWarnings(t, tt.section)
			if tt.want == "" {
				if strings.Contains(logs, warning) {
					t.Fatalf("unexpected warning:\n%s", logs)
				}
				return
			}
			if !strings.Contains(logs, warning) || !strings.Contains(logs, tt.want) {
				t.Fatalf("logs missing %q with %q:\n%s", warning, tt.want, logs)
			}
			for _, attr := range []string{"source=fixture.http", "line=1", "method=R"} {
				if !strings.Contains(logs, attr) {
					t.Fatalf("warning missing %s:\n%s", attr, logs)
				}
			}
		})
	}
}

func TestWarnInvalidGeneratorArgumentsOncePerPlaceholder(t *testing.T) {
	logs := placeholderWarnings(t, "### R\nGET /r\nX-A: {{$randomInt 10 1}}\n\n{{$randomInt 10 1}} {{$randomInt 10 1}} {{$randomInt 9 1}}")
	if n := strings.Count(logs, "invalid placeholder arguments"); n != 2 {
		t.Fatalf("got %d warnings, want one per distinct placeholder:\n%s", n, logs)
	}
}

// The warning says the placeholder is preserved literally; check the response does that.
func TestInvalidGeneratorArgumentsAreServedLiterally(t *testing.T) {
	methods, err := restclient.Parse("fixture.http", strings.NewReader("### R\nGET /r\n\nn={{$randomInt 10 1}}"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	w := httptest.NewRecorder()
	New(methods, slog.New(slog.DiscardHandler)).ServeHTTP(w, httptest.NewRequest("GET", "/r", nil))
	if got := w.Body.String(); got != "n={{$randomInt 10 1}}" {
		t.Fatalf("body = %q, want the placeholder kept literally", got)
	}
}
