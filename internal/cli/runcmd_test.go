package cli

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestMockRunHelper is the command mock run starts in these tests: it
// requests MOCK_RUN_PATHS from MOCK_URL and exits with MOCK_RUN_EXIT.
func TestMockRunHelper(t *testing.T) {
	if os.Getenv("MOCK_RUN_HELPER") != "1" {
		t.Skip("helper process for TestMockRun")
	}
	for _, path := range strings.Fields(os.Getenv("MOCK_RUN_PATHS")) {
		resp, err := http.Get(os.Getenv("MOCK_URL") + path)
		if err != nil {
			os.Exit(90)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if os.Getenv("MOCK_ADMIN_URL") == "" {
		os.Exit(91)
	}
	code, _ := strconv.Atoi(os.Getenv("MOCK_RUN_EXIT"))
	os.Exit(code)
}

func TestMockRun(t *testing.T) {
	file := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(file, []byte("### Users\nGET /users\n\n[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(paths, exit string) (string, error) {
		t.Helper()
		t.Setenv("MOCK_RUN_HELPER", "1")
		t.Setenv("MOCK_RUN_PATHS", paths)
		t.Setenv("MOCK_RUN_EXIT", exit)
		var stdout, stderr strings.Builder
		err := Run(context.Background(), []string{"run", file, "--", os.Args[0], "-test.run=^TestMockRunHelper$"}, strings.NewReader(""), &stdout, &stderr, nil)
		return stderr.String(), err
	}
	if out, err := run("/users", "0"); err != nil || !strings.Contains(out, "mock: serving 1 route at http://127.0.0.1:") || !strings.Contains(out, "every request matched") {
		t.Errorf("all matched: %v\n%s", err, out)
	}
	if out, err := run("/users /nope", "0"); ExitCode(err) != 1 || !strings.Contains(out, "1x GET /nope") {
		t.Errorf("unmatched: %v\n%s", err, out)
	}
	if _, err := run("/users", "3"); ExitCode(err) != 3 || !strings.Contains(err.Error(), "exited with status 3") {
		t.Errorf("failing command: %v", err)
	}
	for _, args := range [][]string{{"run", file}, {"run", file, "--"}, {"run", "--", "true"}} {
		if err := Run(context.Background(), args, strings.NewReader(""), io.Discard, io.Discard, nil); ExitCode(err) != 2 {
			t.Errorf("%v: error = %v, want a usage error", args, err)
		}
	}
	err := Run(context.Background(), []string{"run", file, "--", "no-such-command-for-mock-run"}, strings.NewReader(""), io.Discard, io.Discard, nil)
	if ExitCode(err) != 1 || !strings.Contains(err.Error(), "cannot run") {
		t.Errorf("missing command: %v", err)
	}
}
