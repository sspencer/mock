package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const checkFile = `### Find user
GET /users/:id
Content-Type: application/json

{"id": {{$id}}, "name": "{{$name}}", "nickname": "x"}

### List users
GET /users
Content-Type: application/json

[{"id": 1, "name": "Ann"}]

### Health
GET /health
Content-Type: text/plain

ok

### Create user
POST /users

{"id": 2}

### Charges
@proxy = https://example.com
GET /charges

### Regex only
@header.X-Key = ~^k[0-9]+$
GET /keyed

ok
`

func TestCheckReportsDrift(t *testing.T) {
	var seen []*http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r)
		switch r.URL.Path {
		case "/v1/users/1":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write([]byte(`{"id": 1, "name": "Ann", "created_at": "2026-01-01"}`))
		case "/v1/users":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id": 1, "name": "Ann"}]`))
		case "/v1/health":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	path := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(path, []byte(checkFile), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := runCheck(context.Background(), []string{"-upstream", upstream.URL + "/v1/", "-header", "Authorization: Bearer t", path}, &out)
	if ExitCode(err) != 1 {
		t.Fatalf("runCheck() = %v, want exit 1\n%s", err, out.String())
	}
	text := out.String()
	for _, want := range []string{
		"drift  GET /users/1 (" + path + ":1 Find user)",
		"json: missing from mock: created_at",
		"json: only in mock: nickname",
		"ok     GET /users (",
		"status: mock 200, upstream 503",
		"content type: mock text/plain, upstream text/html",
		"skip   POST /users (",
		"skip   GET /charges (",
		"error  GET /keyed (",
		"checked 6 routes: 1 ok, 2 drifted, 1 errors, 2 skipped",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	for _, r := range seen {
		if r.Header.Get("Authorization") != "Bearer t" || r.Method != http.MethodGet {
			t.Errorf("upstream got %s %s with Authorization %q", r.Method, r.URL, r.Header.Get("Authorization"))
		}
	}

	out.Reset()
	err = runCheck(context.Background(), []string{"-upstream", upstream.URL + "/v1", "-all", "-json", path}, &out)
	if ExitCode(err) != 1 {
		t.Fatalf("runCheck(-all -json) = %v", err)
	}
	var report checkReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	create := report.Results[3]
	if create.Result != "drift" || create.Method != "POST" || len(create.Differences) != 1 ||
		create.Differences[0] != "content type: mock text/plain, upstream application/json" {
		t.Errorf("create result = %+v", create)
	}
}

func TestCheckPassesWhenNothingDrifted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	}))
	defer upstream.Close()
	path := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(path, []byte("### Ping\nGET /ping\nContent-Type: text/plain; charset=utf-8\n\npong\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runCheck(context.Background(), []string{"-upstream", upstream.URL, path}, &out); err != nil {
		t.Fatalf("runCheck() = %v\n%s", err, out.String())
	}
}

func TestCheckUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"api.http"}, {"-upstream", "ftp://x", "api.http"}, {"-upstream", "http://x"}, {"-upstream", "http://x", "-header", "bad", "a.http"}} {
		if err := runCheck(context.Background(), args, &bytes.Buffer{}); ExitCode(err) != 2 {
			t.Errorf("runCheck(%q) = %v, want usage error", args, err)
		}
	}
}

func TestCompareJSONFieldsDeep(t *testing.T) {
	mock := []byte(`{"id": "7", "user": {"name": "Ada", "address": {"city": "London"}, "tags": [{"k": 1}]}, "note": null, "items": [{"sku": "A", "qty": 1}]}`)
	real := []byte(`{"id": 7, "user": {"name": "Ada", "email": "a@x", "address": {"city": "London", "zip": "N1"}, "tags": [{"k": "1"}]}, "note": "hi", "items": [{"sku": "A"}]}`)
	got := compareJSONFields(mock, real, 3)
	want := []string{
		"json id: mock has a string, upstream a number",
		"json items[0]: only in mock: qty",
		"json user: missing from mock: email",
		"json user.address: missing from mock: zip",
		"json user.tags[0].k: mock has a number, upstream a string",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("depth 3:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	shallow := compareJSONFields(mock, real, 1)
	if strings.Join(shallow, "\n") != "json id: mock has a string, upstream a number" {
		t.Errorf("depth 1: %q", shallow)
	}
	if got := compareJSONFields([]byte(`{"meta": {"a.b": {"x": 1}}}`), []byte(`{"meta": {"a.b": {"x": 1, "y": 2}}}`), 3); len(got) != 1 || got[0] != `json meta["a.b"]: missing from mock: y` {
		t.Errorf("quoted key: %q", got)
	}
}

func TestCompareResponseHeaders(t *testing.T) {
	mock := &checkResponse{Status: 200, ContentType: "text/plain", headers: http.Header{"Cache-Control": {"no-store"}}}
	real := &checkResponse{Status: 200, ContentType: "text/plain", headers: http.Header{"Cache-Control": {"max-age=60"}, "X-Request-Id": {"r1"}}}
	got := compareResponses("GET", mock, real, compareOptions{depth: 3, headers: []string{"cache-control", "X-Request-Id", "Etag"}})
	want := []string{`header Cache-Control: mock "no-store", upstream "max-age=60"`, `header X-Request-Id: mock (none), upstream "r1"`}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got %q", got)
	}
}
