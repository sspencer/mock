package restclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestImport(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"shared/common.http": "@import = base.http\n@host = api.example.com\n@token = shared-token\n@unused = 1\n@delay = 50ms\n\n### Shared route (not imported)\nGET /shared\n",
		"shared/base.http":   "@scheme = https\n@host = base.example.com\n",
		"api.http":           "@import = shared/common.http\n@token = local-token\n\n### Profile\nGET /me\nX-Url: {{scheme}}://{{host}}\nX-Token: {{token}}\n",
	})
	methods, err := Load([]string{filepath.Join(dir, "api.http")})
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 {
		t.Fatalf("imported routes as well: %d routes", len(methods))
	}
	m := methods[0]
	got := map[string]string{"scheme": m.Variables["scheme"], "host": m.Variables["host"], "token": m.Variables["token"]}
	want := map[string]string{"scheme": "https", "host": "api.example.com", "token": "local-token"}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("@%s = %q, want %q", name, got[name], value)
		}
	}
	if m.Delay.Milliseconds() != 50 {
		t.Errorf("imported @delay = %s", m.Delay)
	}
	if len(m.Imports) != 2 || !strings.HasSuffix(m.Imports[0], filepath.Join("shared", "common.http")) || !strings.HasSuffix(m.Imports[1], filepath.Join("shared", "base.http")) {
		t.Errorf("Imports = %v", m.Imports)
	}
	if deps := ImportDependencies(methods); len(deps) != 2 {
		t.Errorf("ImportDependencies = %v", deps)
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Errorf("imported variables reported unused: %v", unused)
	}
}

func TestImportErrors(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"a.http":        "@import = b.http\n### A\nGET /a\n",
		"b.http":        "@import = a.http\n",
		"bad.http":      "@delay = soon\n",
		"uses.http":     "@import = bad.http\n### U\nGET /u\n",
		"missing.http":  "\n@import = nowhere.http\n### M\nGET /m\n",
		"section.http":  "### S\n@import = b.http\nGET /s\n",
		"template.http": "@import = {{dir}}/b.http\n### T\nGET /t\n",
	})
	for file, want := range map[string]string{
		"a.http":        "@import cycle:",
		"uses.http":     "bad.http:1:",
		"missing.http":  "missing.http:2: @import nowhere.http:",
		"section.http":  "@import goes at file scope",
		"template.http": "without placeholders",
	} {
		_, err := Load([]string{filepath.Join(dir, file)})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error = %v, want %q", file, err, want)
		}
	}
	if err := CheckVariableOverride("import"); err == nil {
		t.Error("-v import=... accepted")
	}
}
