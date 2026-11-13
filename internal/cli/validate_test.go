package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeValidateFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runValidateArgs(t *testing.T, args ...string) (stdout string, err error) {
	t.Helper()
	var out bytes.Buffer
	err = runValidate(args, &out)
	return out.String(), err
}

const (
	validFile   = "### Users\nGET /users\nContent-Type: application/json\n\n[]\n"
	warningFile = "### Bad\nGET /x\nContent-Type: application/json\n\n{\"a\":\"{{$randomInt 5 5}}\"}\n"
	brokenFile  = "### Broken\nFETCH\n"
)

func TestValidateAcceptsCleanFile(t *testing.T) {
	out, err := runValidateArgs(t, "-strict", writeValidateFile(t, validFile))
	if err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok: 1 routes, 0 warnings") {
		t.Fatalf("output = %q", out)
	}
}

func TestValidateWarningsFailOnlyInStrictMode(t *testing.T) {
	path := writeValidateFile(t, warningFile)

	out, err := runValidateArgs(t, path)
	if err != nil {
		t.Fatalf("warnings should pass without -strict: %v", err)
	}
	if !strings.Contains(out, "warning: invalid placeholder arguments") {
		t.Fatalf("missing warning in output: %q", out)
	}

	_, err = runValidateArgs(t, "-strict", path)
	if ExitCode(err) != 1 {
		t.Fatalf("-strict error = %v, want exit code 1", err)
	}
}

func TestValidateReportsLoadErrors(t *testing.T) {
	for name, content := range map[string]string{"broken": brokenFile, "empty": "# nothing here\n"} {
		out, err := runValidateArgs(t, writeValidateFile(t, content))
		if ExitCode(err) != 1 || !strings.Contains(out, "error: ") {
			t.Errorf("%s: err = %v, output = %q; want exit 1 with an error line", name, err, out)
		}
	}
	if _, err := runValidateArgs(t, filepath.Join(t.TempDir(), "missing.http")); ExitCode(err) != 1 {
		t.Fatalf("missing file error = %v, want exit code 1", err)
	}
}

func TestValidateJSONReport(t *testing.T) {
	out, err := runValidateArgs(t, "-json", writeValidateFile(t, warningFile))
	if err != nil {
		t.Fatal(err)
	}
	var report validateReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("invalid JSON %q: %v", out, err)
	}
	if !report.OK || report.Routes != 1 || len(report.Diagnostics) == 0 {
		t.Fatalf("report = %+v", report)
	}
	d := report.Diagnostics[0]
	if d.Level != "warning" || d.Line == 0 || !strings.HasSuffix(d.Source, "api.http") || d.Details["placeholder"] == "" {
		t.Fatalf("diagnostic = %+v", d)
	}

	out, err = runValidateArgs(t, "-json", writeValidateFile(t, brokenFile))
	if ExitCode(err) != 1 {
		t.Fatalf("broken file error = %v", err)
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil || report.OK || report.Diagnostics[0].Level != "error" {
		t.Fatalf("broken report = %+v, err = %v", report, err)
	}
}

func TestValidateReportsEveryError(t *testing.T) {
	path := writeValidateFile(t, "### A\n@times = 0\nGET /a\n\n### B\nGET /b\n\n### C\n@delay = soon\nGET /c\n")
	out, err := runValidateArgs(t, "-json", path)
	var report validateReport
	if jsonErr := json.Unmarshal([]byte(out), &report); jsonErr != nil || ExitCode(err) != 1 || len(report.Diagnostics) != 2 {
		t.Fatalf("report = %s, err = %v", out, err)
	}
	for i, line := range []int{2, 9} {
		if d := report.Diagnostics[i]; d.Level != "error" || d.Source != path || d.Line != line || strings.Contains(d.Message, path) {
			t.Errorf("diagnostic %d = %+v, want an error at line %d", i, d, line)
		}
	}
	out, _ = runValidateArgs(t, path)
	if !strings.Contains(out, path+":2: error: section \"A\": @times") || !strings.Contains(out, path+":9: error: ") {
		t.Errorf("text report = %q", out)
	}
	if err == nil || err.Error() != "validation failed: 2 errors, 0 warnings" {
		t.Errorf("err = %v", err)
	}
}

func TestValidateUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"-bogus", "x.http"}} {
		if _, err := runValidateArgs(t, args...); ExitCode(err) != 2 {
			t.Errorf("runValidate(%q) error = %v, want usage error", args, err)
		}
	}
}

func TestHelpFlagsPrintUsageAndSucceed(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"-help"}} {
		var out bytes.Buffer
		if err := Run(context.Background(), args, strings.NewReader(""), &out, &bytes.Buffer{}, nil); err != nil {
			t.Fatalf("Run(%q) error = %v, want nil", args, err)
		}
		for _, want := range []string{"usage:", "mock validate", "-p, --port", "-a, --ui-port", "-x, --no-ui", "-b, --bind", "MOCK_BIND"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("Run(%q) output missing %q:\n%s", args, want, out.String())
			}
		}
	}
	for _, flagName := range []string{"-h", "--help"} {
		out, err := runValidateArgs(t, flagName)
		if err != nil || !strings.Contains(out, "usage: mock validate") || !strings.Contains(out, "-strict") {
			t.Errorf("validate %s: err = %v, output = %q", flagName, err, out)
		}
	}
}

func TestRunDispatchesValidateSubcommand(t *testing.T) {
	var out bytes.Buffer
	err := Run(context.Background(), []string{"validate", writeValidateFile(t, validFile)}, strings.NewReader(""), &out, &bytes.Buffer{}, nil)
	if err != nil || !strings.Contains(out.String(), "ok: 1 routes") {
		t.Fatalf("run validate: err = %v, output = %q", err, out.String())
	}
}

func TestValidateVarSuppliesVariable(t *testing.T) {
	path := writeValidateFile(t, "### Users\nGET /users\nX-Token: {{token}}\n\nok\n")
	if out, err := runValidateArgs(t, "-strict", path); err == nil {
		t.Fatalf("missing variable should warn under -strict:\n%s", out)
	}
	if out, err := runValidateArgs(t, "-strict", "-var", "token=abc", path); err != nil {
		t.Fatalf("validate -var failed: %v\n%s", err, out)
	}
}

func TestValidateReportsInvalidJSONBody(t *testing.T) {
	path := writeValidateFile(t, "### Users\nGET /users\nContent-Type: application/json\n\n[{\"id\": {{$query.id}}},]\n")
	out, err := runValidateArgs(t, "-strict", path)
	if err == nil || !strings.Contains(out, "response body is not valid JSON") {
		t.Fatalf("validate should report the trailing comma (err %v):\n%s", err, out)
	}
}
