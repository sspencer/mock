package cli

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestLoadMethodsLoadsFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.http")
	if err := os.WriteFile(path, []byte(`### User
GET /users

ok
`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	methods, err := inputSource{Files: []string{path}}.load(strings.NewReader(""), restclient.Options{})
	if err != nil {
		t.Fatalf("loadMethods() error = %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(methods))
	}
	if methods[0].Source != path {
		t.Fatalf("Source = %q, want %q", methods[0].Source, path)
	}
	if methods[0].Method != http.MethodGet || methods[0].Path != "/users" {
		t.Fatalf("method = %#v, want GET /users", methods[0])
	}
}

func TestLoadMethodsParsesStdinWhenNoFiles(t *testing.T) {
	methods, err := inputSource{}.load(strings.NewReader(`### User
POST /users

created
`), restclient.Options{})
	if err != nil {
		t.Fatalf("loadMethods() error = %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("len(methods) = %d, want 1", len(methods))
	}
	if methods[0].Source != "<stdin>" {
		t.Fatalf("Source = %q, want <stdin>", methods[0].Source)
	}
	if methods[0].Method != http.MethodPost || methods[0].Path != "/users" {
		t.Fatalf("method = %#v, want POST /users", methods[0])
	}
}

func TestLoadInputUsesSingleDirectoryAsStaticRoot(t *testing.T) {
	dir := t.TempDir()
	input, err := loadInput([]string{dir}, nil, strings.NewReader(""), restclient.Options{}, tagFilter{})
	if err != nil {
		t.Fatalf("loadInput() error = %v", err)
	}
	if input.StaticDir != dir {
		t.Fatalf("StaticDir = %q, want %q", input.StaticDir, dir)
	}
	if len(input.Methods) != 0 {
		t.Fatalf("len(Methods) = %d, want 0", len(input.Methods))
	}
}

func TestLoadInputRejectsDirectoryMixedWithRequestFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "user.http")
	if err := os.WriteFile(path, []byte("### User\nGET /users\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadInput([]string{dir, path}, nil, strings.NewReader(""), restclient.Options{}, tagFilter{})
	if err == nil {
		t.Fatal("loadInput() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cannot mix static directory") {
		t.Fatalf("error = %q, want mixed directory error", err)
	}
}

func TestValidateMethodsAllowsParsedRequests(t *testing.T) {
	err := validateMethods([]restclient.Method{{Name: "User"}}, "stdin")
	if err != nil {
		t.Fatalf("validateMethods() error = %v", err)
	}
}

func TestValidateMethodsExplainsEmptyFileInput(t *testing.T) {
	err := validateMethods(nil, "empty.http")
	if err == nil {
		t.Fatal("validateMethods() error = nil, want error")
	}
	for _, want := range []string{"no mock requests found", "empty.http", "###", "HTTP request line"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want to contain %q", err, want)
		}
	}
}

func TestValidateMethodsExplainsEmptyStdinInput(t *testing.T) {
	err := validateMethods(nil, inputSource{}.describe())
	if err == nil {
		t.Fatal("validateMethods() error = nil, want error")
	}
	for _, want := range []string{"no mock requests found", "stdin", "###", "HTTP request line"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want to contain %q", err, want)
		}
	}
}

func TestPrintMethods(t *testing.T) {
	methods := []restclient.Method{
		{
			Name:   "Create User",
			Method: http.MethodPost,
			Path:   "/users",
		},
		{
			Name:   "Get Cats",
			Method: http.MethodGet,
			Path:   "/names",
			Query:  url.Values{"type": []string{"cat"}},
		},
	}

	var output bytes.Buffer
	printMethods(&output, methods)

	got := output.String()
	for _, want := range []string{
		"Available mock methods:",
		"POST    /users",
		"Create User",
		"GET     /names?type=cat",
		"Get Cats",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("printMethods() output = %q, want to contain %q", got, want)
		}
	}
}

func TestTagFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api.http")
	if err := os.WriteFile(path, []byte("### A\n@tag = billing\nGET /a\n\n### B\n@tag = billing, slow\nGET /b\n\n### C\nGET /c\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	names := func(filter tagFilter) string {
		methods, err := inputSource{Files: []string{path}, Tags: filter}.load(nil, restclient.Options{})
		if err != nil {
			return "error: " + err.Error()
		}
		var out []string
		for _, m := range methods {
			out = append(out, m.Name)
		}
		return strings.Join(out, ",")
	}
	for _, tt := range []struct {
		filter tagFilter
		want   string
	}{
		{tagFilter{}, "A,B,C"},
		{tagFilter{Only: []string{"Billing"}}, "A,B"},
		{tagFilter{Skip: []string{"slow"}}, "A,C"},
		{tagFilter{Only: []string{"billing"}, Skip: []string{"slow"}}, "A"},
		{tagFilter{Only: []string{"nothing"}}, "error: --only and --skip left none of the 3 routes to serve (only nothing)"},
	} {
		if got := names(tt.filter); got != tt.want {
			t.Errorf("%+v: got %q, want %q", tt.filter, got, tt.want)
		}
	}
}

func TestParseConfigTags(t *testing.T) {
	cfg, err := parseConfig([]string{"--only", "billing, orders", "--only", "users", "--skip", "slow", "api.http"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Tags.Only, ",") != "billing,orders,users" || strings.Join(cfg.Tags.Skip, ",") != "slow" {
		t.Errorf("tags = %+v", cfg.Tags)
	}
	if _, err := parseConfig([]string{"--only", "a b", "api.http"}); err == nil {
		t.Error("--only with a space: want an error")
	}
}
