package cli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestClientGeneratesRESTClientFile(t *testing.T) {
	dir := t.TempDir()
	apiPath := filepath.Join(dir, "api.http")
	source := `
@token = secret-token

### List admins
@tag = users
@query.role = admin
GET /users
Content-Type: application/json

[{"id": 1}]

### Get a user
@tag = users
@header.Authorization = Bearer {{token}}
@cookie.session = abc
GET /users/:id
Content-Type: application/json

{"id": {{$id}}}

### [201] Create a user
@tag = writes
@json.name = Ada
@json.age = >20
POST /users
Content-Type: application/json

{"name": "{{$json.name}}"}

### Submit form
@form.email = ada@example.test
POST /subscribe

ok

### Host route
@match.host = api.internal.test
GET /internal

ok
`
	if err := os.WriteFile(apiPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	logger := slog.New(slog.DiscardHandler)
	err := Run(context.Background(), []string{"client", "-port", "9000", "-H", "X-Client: dev", apiPath}, strings.NewReader(""), &out, io.Discard, logger)
	if err != nil {
		t.Fatalf("mock client error = %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"@baseUrl = http://127.0.0.1:9000\n",
		"### List admins\nGET {{baseUrl}}/users?role=admin\nX-Client: dev\n",
		"### Get a user\nGET {{baseUrl}}/users/1\nAuthorization: Bearer secret-token\nCookie: session=abc\nX-Client: dev\n",
		"### Create a user\nPOST {{baseUrl}}/users\nContent-Type: application/json\nX-Client: dev\n\n{\n  \"age\": 21,\n  \"name\": \"Ada\"\n}\n",
		"### Submit form\nPOST {{baseUrl}}/subscribe\nContent-Type: application/x-www-form-urlencoded\nX-Client: dev\n\nemail=ada%40example.test\n",
		"### Host route\nGET {{baseUrl}}/internal\nHost: api.internal.test\nX-Client: dev\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing:\n%s\ngot:\n%s", want, got)
		}
	}
	// Mock response bodies must not leak into the generated client requests.
	if strings.Contains(got, `[{"id": 1}]`) {
		t.Errorf("output contains mock response body:\n%s", got)
	}
}

func TestClientMultiPortAndFilters(t *testing.T) {
	dir := t.TempDir()
	apiPath := filepath.Join(dir, "ports.http")
	source := `
### Main
@tag = core
GET /main

ok

### Admin metrics
@tag = ops
@port = 9100
GET /metrics

ok
`
	if err := os.WriteFile(apiPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MOCK_URL", "http://127.0.0.1:8088")
	var out bytes.Buffer
	if err := runClient([]string{apiPath}, &out); err != nil {
		t.Fatalf("runClient() error = %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"@baseUrl = http://127.0.0.1:8088\n",
		"@baseUrl_9100 = http://127.0.0.1:9100\n",
		"GET {{baseUrl}}/main\n",
		"GET {{baseUrl_9100}}/metrics\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}

	// Explicit -url overrides all ports and -only filters by @tag.
	out.Reset()
	if err := runClient([]string{"-url", "https://api.example.test", "-only", "ops", apiPath}, &out); err != nil {
		t.Fatalf("runClient(-url -only) error = %v", err)
	}
	got = out.String()
	if !strings.Contains(got, "@baseUrl = https://api.example.test\n") || !strings.Contains(got, "GET {{baseUrl}}/metrics\n") {
		t.Errorf("unexpected filtered output:\n%s", got)
	}
	if strings.Contains(got, "/main") || strings.Contains(got, "baseUrl_9100") {
		t.Errorf("filtered output kept excluded route or port:\n%s", got)
	}
}

func TestClientUsageErrorsAndHelp(t *testing.T) {
	var out bytes.Buffer
	if err := runClient([]string{"-h"}, &out); err != nil {
		t.Fatalf("-h error = %v", err)
	}
	if !strings.Contains(out.String(), "usage: mock client") {
		t.Errorf("help output missing usage:\n%s", out.String())
	}

	cases := [][]string{
		{},
		{"-url", "http://127.0.0.1:8080", "-port", "8080", "a.http"},
		{"-url", "ftp://example.com", "a.http"},
		{"-port", "0", "a.http"},
		{"-env-file", "envs.json", "a.http"},
	}
	for _, args := range cases {
		if err := runClient(args, io.Discard); ExitCode(err) != 2 {
			t.Errorf("args %v: ExitCode = %d (%v), want 2", args, ExitCode(err), err)
		}
	}
}

func TestClientBaseURLNamesAreUnique(t *testing.T) {
	source := "### A\n@port = 9001\nGET /a\n\nok\n\n### B\nGET /b\n\nok\n\n### C\n@port = 8080\nGET /c\n\nok\n\n### D\n@port = 9000\nGET /d\n\nok\n"
	methods, err := restclient.Parse("dup.http", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	base, err := parseSendURL("MOCK_URL", "http://127.0.0.1:9000")
	if err != nil {
		t.Fatal(err)
	}
	got := formatRESTClientFile(methods, base, false, 8080, nil)
	for _, want := range []string{
		"@baseUrl = http://127.0.0.1:9001\n",
		"@baseUrl_9000 = http://127.0.0.1:9000\n",
		"@baseUrl_8080 = http://127.0.0.1:8080\n",
		"GET {{baseUrl_9000}}/b\n",
		"GET {{baseUrl_8080}}/c\n",
		"GET {{baseUrl_9000}}/d\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "@baseUrl_8080 =") != 1 {
		t.Errorf("baseUrl_8080 defined more than once:\n%s", got)
	}

	// Base URLs that differ but resolve to the same port get a suffix.
	ports, err := restclient.Parse("ports.http", strings.NewReader("### X\n@port = 9001\nGET /x\n\nok\n\n### Y\nGET /y\n\nok\n\n### Z\n@port = 80\nGET /z\n\nok\n"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := parseSendURL("MOCK_URL", "http://api.test")
	if err != nil {
		t.Fatal(err)
	}
	got = formatRESTClientFile(ports, plain, false, 8080, nil)
	for _, want := range []string{"@baseUrl_80 = http://api.test\n", "@baseUrl_80_2 = http://api.test:80\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}
