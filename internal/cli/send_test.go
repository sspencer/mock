package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func TestSendFiresSampleRequestsForAllRoutes(t *testing.T) {
	dir := t.TempDir()
	apiPath := filepath.Join(dir, "api.http")
	source := `
@token = secret-token

### List admins
@tag = users
@query.role = admin
GET /users
Content-Type: application/json

[{"id": 1, "role": "admin"}]

### Get a user
@tag = users
@header.Authorization = Bearer {{token}}
@cookie.session = abc
GET /users/:id
Content-Type: application/json

{"id": {{$id}}, "auth": "{{$header.Authorization}}"}

### [201] Create a user
@tag = writes
@json.name = Ada
@json.age = >20
POST /users
Content-Type: application/json

{"name": "{{$json.name}}", "age": {{$json.age}}}

### [404] Missing user
@path.id = 0
GET /users/:id
Content-Type: application/json

{"error": "missing"}

### [302] Redirect old path
GET /old
Location: /users

### [204] Delete user
@tag = writes
DELETE /users/:id

### Host route
@match.host = api.internal.test
GET /internal
Content-Type: text/plain

internal ok
`
	if err := os.WriteFile(apiPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	methods, err := restclient.Parse(apiPath, strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	mockServer := mockhttp.New(methods, slog.New(slog.DiscardHandler))
	defer mockServer.Close()
	ts := httptest.NewServer(mockServer)
	defer ts.Close()

	var out bytes.Buffer
	err = Run(context.Background(), []string{"send", "-all", "-url", ts.URL, "-H", "X-Run: test", apiPath}, strings.NewReader(""), &out, io.Discard, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("mock send failed: %v\n%s", err, out.String())
	}

	events := mockServer.Requests(mockhttp.RequestFilter{})
	if len(events) != len(methods) {
		t.Fatalf("received %d requests, want %d:\n%s", len(events), len(methods), out.String())
	}
	// Every route in this file should have matched its own sample request.
	for i, ev := range events {
		if ev.Match.Route == nil {
			t.Errorf("request %d (%s %s) went unmatched", i, ev.Request.Method, ev.Request.URL)
		}
		if got := ev.Request.Headers.Get("X-Run"); got != "test" {
			t.Errorf("request %d X-Run = %q, want test", i, got)
		}
		if got := ev.Request.Headers.Get("User-Agent"); got != "mock-send" {
			t.Errorf("request %d User-Agent = %q, want mock-send", i, got)
		}
	}
	// Redirect must not be followed; 302 is reported directly.
	if events[4].Response.Status != 302 {
		t.Errorf("redirect status = %d, want 302", events[4].Response.Status)
	}
	// Host-matched route received its @match.host header and matched.
	if events[6].Match.Route == nil || events[6].Match.Route.Name != "Host route" {
		t.Errorf("host route match = %+v, want Host route", events[6].Match.Route)
	}
	// Output lists statuses and summary.
	for _, want := range []string{"GET", "/users?role=admin", "200", "201", "404", "302", "204", "sent 7 requests (0 errors)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}

	// Tag filtering and JSON output (flags after the file path work too).
	out.Reset()
	err = runSend(context.Background(), []string{apiPath, "-url", ts.URL, "-only", "writes", "-json", "-all"}, &out)
	if err != nil {
		t.Fatalf("mock send -only -json failed: %v", err)
	}
	var report sendReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if !report.OK || report.Sent != 2 || report.Errors != 0 || len(report.Results) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Results[0].Method != "POST" || report.Results[0].Status != 201 {
		t.Errorf("result[0] = %+v", report.Results[0])
	}
	if report.Results[1].Method != "DELETE" || report.Results[1].Status != 204 {
		t.Errorf("result[1] = %+v", report.Results[1])
	}
}

func TestSendMultiPortAndEnvDefaults(t *testing.T) {
	// Bind two mock ports directly to test per-route @port targeting.
	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		ln1.Close()
		t.Fatal(err)
	}
	port1 := ln1.Addr().(*net.TCPAddr).Port
	port2 := ln2.Addr().(*net.TCPAddr).Port

	dir := t.TempDir()
	apiPath := filepath.Join(dir, "ports.http")
	source := fmt.Sprintf(`
### Main
GET /main

ok

### Secondary
@port = %d
GET /secondary

ok2
`, port2)
	if err := os.WriteFile(apiPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	methods, err := restclient.Parse(apiPath, strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	mockServer := mockhttp.New(methods, slog.New(slog.DiscardHandler))
	defer mockServer.Close()

	srv1 := &http.Server{Handler: mockServer}
	srv2 := &http.Server{Handler: mockServer}
	go srv1.Serve(ln1)
	go srv2.Serve(ln2)
	defer srv1.Close()
	defer srv2.Close()

	// MOCK_URL sets the default base URL while @port still targets port2.
	t.Setenv("MOCK_URL", fmt.Sprintf("http://127.0.0.1:%d", port1))
	var out bytes.Buffer
	if err := runSend(context.Background(), []string{"-json", apiPath}, &out); err != nil {
		t.Fatalf("runSend with MOCK_URL failed: %v\n%s", err, out.String())
	}
	var report sendReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Sent != 2 {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(report.Results[0].URL, fmt.Sprintf(":%d/main", port1)) {
		t.Errorf("result[0].URL = %q, want port %d", report.Results[0].URL, port1)
	}
	if !strings.Contains(report.Results[1].URL, fmt.Sprintf(":%d/secondary", port2)) {
		t.Errorf("result[1].URL = %q, want port %d", report.Results[1].URL, port2)
	}
	for _, r := range report.Results {
		if r.Status != 200 {
			t.Errorf("%s status = %d, want 200", r.Name, r.Status)
		}
	}
}

func TestSendReportsTransportErrorsAndExitsOne(t *testing.T) {
	// Reserve and immediately close a port so connections are refused.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	dir := t.TempDir()
	apiPath := filepath.Join(dir, "api.http")
	if err := os.WriteFile(apiPath, []byte("### Ping\nGET /ping\n\nok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err = runSend(context.Background(), []string{"-port", fmt.Sprintf("%d", closedPort), "-timeout", "1s", apiPath}, &out)
	if ExitCode(err) != 1 {
		t.Fatalf("ExitCode = %d (%v), want 1", ExitCode(err), err)
	}
	if !strings.Contains(out.String(), "ERR") || !strings.Contains(out.String(), "sent 1 request (1 error)") {
		t.Errorf("unexpected output:\n%s", out.String())
	}
}

func TestSendDelayAndCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	dir := t.TempDir()
	apiPath := filepath.Join(dir, "api.http")
	if err := os.WriteFile(apiPath, []byte("### One\nGET /1\n\nok\n\n### Two\nGET /2\n\nok\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	var out bytes.Buffer
	if err := runSend(context.Background(), []string{"-url", ts.URL, "-delay", "25ms", apiPath}, &out); err != nil {
		t.Fatalf("runSend with -delay failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("elapsed = %v, expected at least 20ms delay between requests", elapsed)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runSend(ctx, []string{"-url", ts.URL, apiPath}, &out); ExitCode(err) != 1 {
		t.Errorf("cancelled context ExitCode = %d (%v), want 1", ExitCode(err), err)
	}
}

func TestSendUsageErrorsAndHelp(t *testing.T) {
	var out bytes.Buffer
	if err := runSend(context.Background(), []string{"-h"}, &out); err != nil {
		t.Fatalf("-h failed: %v", err)
	}
	if !strings.Contains(out.String(), "usage: mock send") {
		t.Errorf("help output missing usage:\n%s", out.String())
	}

	cases := [][]string{
		{},
		{"-url", "http://127.0.0.1:8080", "-port", "8080", "a.http"},
		{"-url", "ftp://example.com", "a.http"},
		{"-url", "http://127.0.0.1:8080?x=1", "a.http"},
		{"-port", "0", "a.http"},
		{"-delay", "-1s", "a.http"},
		{"-timeout", "0s", "a.http"},
		{"-env-file", "envs.json", "a.http"},
	}
	for _, args := range cases {
		if err := runSend(context.Background(), args, io.Discard); ExitCode(err) != 2 {
			t.Errorf("args %v: ExitCode = %d (%v), want 2", args, ExitCode(err), err)
		}
	}
}

func TestSendSkipsUnsafeMethodsWithoutAll(t *testing.T) {
	source := "### Read\nGET /items\n\nok\n\n### Write\nPOST /items\n\nok\n\n### Remove\nDELETE /items/:id\n\nok\n"
	apiPath := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(apiPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	var methods []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
	}))
	defer ts.Close()

	var out bytes.Buffer
	if err := runSend(context.Background(), []string{"-url", ts.URL, "-json", apiPath}, &out); err != nil {
		t.Fatalf("mock send: %v\n%s", err, out.String())
	}
	var report sendReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Sent != 1 || report.Skipped != 2 || len(report.Results) != 3 {
		t.Fatalf("report = %+v", report)
	}
	if !report.Results[1].Skipped || report.Results[1].Method != "POST" || !report.Results[2].Skipped {
		t.Errorf("results = %+v", report.Results)
	}
	if strings.Join(methods, ",") != "GET" {
		t.Errorf("server saw %v, want only GET", methods)
	}

	out.Reset()
	if err := runSend(context.Background(), []string{"-url", ts.URL, apiPath}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SKIP") || !strings.Contains(out.String(), "sent 1 request (0 errors, 2 skipped)") {
		t.Errorf("text output:\n%s", out.String())
	}
}

func TestSendFailsOnTruncatedBody(t *testing.T) {
	apiPath := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(apiPath, []byte("### Short\nGET /short\n\nok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "only ten b")
	}))
	defer ts.Close()

	var out bytes.Buffer
	err := runSend(context.Background(), []string{"-url", ts.URL, "-json", apiPath}, &out)
	if ExitCode(err) != 1 {
		t.Fatalf("exit = %d (%v), want 1\n%s", ExitCode(err), err, out.String())
	}
	var report sendReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Errors != 1 || report.Results[0].Status != 200 || !strings.Contains(report.Results[0].Error, "reading the response body") {
		t.Errorf("report = %+v", report)
	}
}

func TestSendBaseURLKeepsIPv6Brackets(t *testing.T) {
	base, err := parseSendURL("MOCK_URL", "http://[::1]:8080/api")
	if err != nil {
		t.Fatal(err)
	}
	if got := sendBaseURL(base, false, 8080, 9001); got != "http://[::1]:9001/api" {
		t.Errorf("route port: %q", got)
	}
	if got := sendBaseURL(base, false, 8080, 0); got != "http://[::1]:8080/api" {
		t.Errorf("default: %q", got)
	}
}
