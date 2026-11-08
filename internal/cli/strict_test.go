package cli

import (
	"bytes"
	"context"
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
	})
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
	err := strictReport(&out, mockhttp.Coverage{Routes: []mockhttp.RouteHits{{Name: "A", Method: "GET", Path: "/a", Source: "a.http", Line: 1}}})
	if err != nil || !strings.Contains(out.String(), "strict: 1 of 1 route never matched a request") {
		t.Fatalf("err = %v report = %q", err, out.String())
	}
	out.Reset()
	err = strictReport(&out, mockhttp.Coverage{Routes: []mockhttp.RouteHits{{Name: "A", Hits: 1}}})
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
