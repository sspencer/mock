package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sspencer/mock/mockhttp"
)

func TestStrictReportFailsOnUnmatchedRequests(t *testing.T) {
	var out bytes.Buffer
	err := strictReport(&out, mockhttp.Coverage{
		Routes: []mockhttp.RouteHits{
			{Name: "Users", Method: "GET", Path: "/users", Source: "a.http", Line: 1, Hits: 3},
			{Name: "Delete", Method: "DELETE", Path: "/users/:id", Port: 9090, Source: "a.http", Line: 7},
		},
		Unmatched:      []mockhttp.UnmatchedRequest{{Method: "GET", Path: "/missing", Count: 2}, {Method: "POST", Path: "/real", Count: 1, Proxied: true}},
		UnmatchedTotal: 4,
	}, false)
	if err == nil || ExitCode(err) != 1 || err.Error() != "strict: 4 requests unmatched" {
		t.Fatalf("err = %v (exit %d)", err, ExitCode(err))
	}
	want := "strict: 4 requests no route matched\n" +
		"  2x GET /missing\n" +
		"  1x POST /real (proxied)\n" +
		"  ... and 1 request to other paths\n" +
		"strict: 1 of 2 routes never matched a request\n" +
		"  a.http:7  DELETE /users/:id  Delete\n"
	if out.String() != want {
		t.Fatalf("report =\n%s\nwant\n%s", out.String(), want)
	}
}

func TestStrictReportPassesWithUnusedRoutes(t *testing.T) {
	var out bytes.Buffer
	err := strictReport(&out, mockhttp.Coverage{Routes: []mockhttp.RouteHits{{Name: "A", Method: "GET", Path: "/a", Source: "a.http", Line: 1}}}, false)
	if err != nil || !strings.Contains(out.String(), "strict: 1 of 1 route never matched a request") {
		t.Fatalf("err = %v report = %q", err, out.String())
	}
	out.Reset()
	err = strictReport(&out, mockhttp.Coverage{Routes: []mockhttp.RouteHits{{Name: "A", Hits: 1}}}, false)
	if err != nil || out.String() != "strict: every request matched a route, and all 1 route matched a request\n" {
		t.Fatalf("err = %v report = %q", err, out.String())
	}
}

func TestParseConfigStrict(t *testing.T) {
	for _, flag := range []string{"-s", "--strict"} {
		cfg, err := parseConfig([]string{flag, "a.http"})
		if err != nil || !cfg.Strict {
			t.Fatalf("parseConfig(%s) = %+v, %v", flag, cfg, err)
		}
	}
}

func TestParseConfigStrictUnusedAndReport(t *testing.T) {
	for _, tt := range []struct {
		args                 []string
		strict, unused       bool
		report, errorMessage string
	}{
		{args: []string{"--strict=unused"}, strict: true, unused: true},
		{args: []string{"-s=unused", "-s"}, strict: true},
		{args: []string{"--strict=false"}},
		{args: []string{"--strict-report", "out/junit.xml"}, strict: true, report: "out/junit.xml"},
		{args: []string{"--strict=unused", "--strict-report", "r.JSON"}, strict: true, unused: true, report: "r.JSON"},
		{args: []string{"--strict=maybe"}, errorMessage: "--strict=unused"},
		{args: []string{"--strict-report", "report.txt"}, errorMessage: ".xml (JUnit) or .json"},
	} {
		cfg, err := parseConfig(append(tt.args, "a.http"))
		if tt.errorMessage != "" {
			if err == nil || !strings.Contains(err.Error(), tt.errorMessage) {
				t.Errorf("%v: err = %v, want %q", tt.args, err, tt.errorMessage)
			}
			continue
		}
		if err != nil || cfg.Strict != tt.strict || cfg.StrictUnused != tt.unused || cfg.StrictReport != tt.report {
			t.Errorf("%v: cfg strict %v unused %v report %q, err %v", tt.args, cfg.Strict, cfg.StrictUnused, cfg.StrictReport, err)
		}
	}
}

func TestStrictReportFailsOnUnusedRoutes(t *testing.T) {
	coverage := mockhttp.Coverage{Routes: []mockhttp.RouteHits{{Name: "A", Method: "GET", Path: "/a", Source: "a.http", Line: 1, Hits: 1}, {Name: "B", Method: "GET", Path: "/b", Source: "a.http", Line: 5}}}
	var out bytes.Buffer
	if err := strictReport(&out, coverage, true); ExitCode(err) != 1 || err.Error() != "strict: 1 route never matched" {
		t.Fatalf("err = %v", err)
	}
	coverage.Unmatched, coverage.UnmatchedTotal = []mockhttp.UnmatchedRequest{{Method: "GET", Path: "/x", Count: 2}}, 2
	if err := strictReport(&out, coverage, true); err == nil || err.Error() != "strict: 2 requests unmatched, 1 route never matched" {
		t.Fatalf("err = %v", err)
	}
}

func TestStrictReportFiles(t *testing.T) {
	coverage := mockhttp.Coverage{
		Routes: []mockhttp.RouteHits{
			{Name: "Users", Method: "GET", Path: "/users", Source: "a.http", Line: 1, Hits: 3},
			{Name: "Delete", Method: "DELETE", Path: "/users/:id", Source: "a.http", Line: 7},
		},
		Unmatched:      []mockhttp.UnmatchedRequest{{Method: "GET", Path: "/missing", Count: 2, Proxied: true}},
		UnmatchedTotal: 3,
	}
	dir := t.TempDir()
	junit := filepath.Join(dir, "strict.xml")
	if err := writeStrictReport(junit, coverage, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(junit)
	for _, want := range []string{
		`<testsuites name="mock strict" tests="4" failures="2" skipped="1">`,
		`<testsuite name="mock requests" tests="2" failures="2" skipped="0">`,
		`<testcase name="GET /missing" classname="unmatched">`,
		`<failure message="2 requests no route matched (forwarded with --upstream)">`,
		`<testcase name="other paths" classname="unmatched">`,
		`<testcase name="GET /users  Users" classname="a.http" file="a.http" line="1"></testcase>`,
		`<skipped message="never matched a request">a.http:7: route never matched a request</skipped>`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JUnit report lacks %s:\n%s", want, data)
		}
	}
	if err := writeStrictReport(junit, mockhttp.Coverage{Routes: coverage.Routes}, true); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(junit)
	if !strings.Contains(string(data), `<testcase name="every request matched a route" classname="unmatched"></testcase>`) || !strings.Contains(string(data), `<failure message="never matched a request">`) {
		t.Errorf("JUnit report with failUnused:\n%s", data)
	}

	jsonPath := filepath.Join(dir, "strict.json")
	if err := writeStrictReport(jsonPath, coverage, false); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Passed         bool `json:"passed"`
		UnmatchedTotal int  `json:"unmatchedTotal"`
		Unused         []struct {
			Name string `json:"name"`
		} `json:"unused"`
	}
	data, _ = os.ReadFile(jsonPath)
	if err := json.Unmarshal(data, &report); err != nil || report.Passed || report.UnmatchedTotal != 3 || len(report.Unused) != 1 || report.Unused[0].Name != "Delete" {
		t.Errorf("JSON report = %s (%v)", data, err)
	}
	if err := writeStrictReport(filepath.Join(dir, "missing", "r.json"), coverage, false); err == nil {
		t.Error("writing into a missing directory: want an error")
	}
}

func TestRunStrictRejectsStaticDirectory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := Run(cancelledContext(), []string{"-s", t.TempDir()}, strings.NewReader(""), io.Discard, io.Discard, logger)
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "strict") {
		t.Fatalf("Run(dir, -s) error = %v (exit %d), want usage error", err, ExitCode(err))
	}
}

// lockedBuffer lets the test read output while Run writes it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunStrictReportsAtShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(path, []byte("### Users\nGET /users\n\nok\n\n### Orders\nGET /orders\n\nok\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out lockedBuffer
	var errOut bytes.Buffer
	done := make(chan error, 1)
	go func() {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		done <- Run(ctx, []string{"-x", "-s", "-p", "0", path}, strings.NewReader(""), &out, &errOut, logger)
	}()
	addr := regexp.MustCompile(`starting mock HTTP server on (\S+)`)
	var base string
	for deadline := time.Now().Add(3 * time.Second); base == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if m := addr.FindStringSubmatch(out.String()); m != nil {
			base = "http://" + m[1]
		}
	}
	if base == "" {
		t.Fatalf("server did not start; stdout = %q", out.String())
	}
	for _, target := range []string{"/users", "/nope", "/nope"} {
		resp, err := http.Get(base + target)
		if err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		_ = resp.Body.Close()
	}
	cancel()
	err := <-done
	if err == nil || ExitCode(err) != 1 {
		t.Fatalf("Run() error = %v (exit %d), want exit 1", err, ExitCode(err))
	}
	report := errOut.String()
	for _, want := range []string{"strict: 2 requests no route matched\n  2x GET /nope\n", "strict: 1 of 2 routes never matched a request\n", "GET /orders  Orders\n"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report = %q, want %q", report, want)
		}
	}
}
