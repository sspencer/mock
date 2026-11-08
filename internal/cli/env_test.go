package cli

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestEnvJetBrainsFilesWithPrivateOverride(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		envFileName:        `{"$shared":{"version":"v1","host":"shared"},"dev":{"host":"http://localhost:9000","base":"{{$shared version}}/api","retries":3,"debug":true,"nested":{"a":1},"port":"1"}}`,
		privateEnvFileName: `{"dev":{"token":"secret"}}`,
	})
	t.Chdir(dir)
	got, err := envFlags{Name: "dev"}.load()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"version": "v1", "host": "http://localhost:9000", "base": "v1/api", "retries": "3", "debug": "true", "token": "secret"}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if _, err := (envFlags{Name: "prod"}).load(); err == nil || !strings.Contains(err.Error(), `no environment "prod" (have dev)`) {
		t.Fatalf("unknown environment: %v", err)
	}
	if _, err := (envFlags{Name: "$shared"}).load(); err == nil {
		t.Fatal("$shared is not selectable")
	}
}

func TestEnvVSCodeSettings(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{
		vscodeSettingsPath: `{
  // Editor settings sit beside the environments.
  "editor.tabSize": 2,
  "rest-client.environmentVariables": {
    "$shared": {"token": "a//b"}, /* shared */
    "local": {"host": "http://localhost:9000",},
  },
}`,
	})
	t.Chdir(dir)
	got, err := envFlags{Name: "local"}.load()
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"token": "a//b", "host": "http://localhost:9000"}; !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEnvExplicitFileAndErrors(t *testing.T) {
	dir := writeEnvFiles(t, map[string]string{"envs/staging.json": `{"staging":{"host":"h"}}`})
	t.Chdir(t.TempDir())
	if _, err := (envFlags{Name: "staging"}).load(); err == nil || !strings.Contains(err.Error(), "no REST Client environments") {
		t.Fatalf("missing files: %v", err)
	}
	got, err := envFlags{Name: "staging", File: filepath.Join(dir, "envs/staging.json")}.load()
	if err != nil || got["host"] != "h" {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := (envFlags{Name: "x", File: filepath.Join(dir, "missing.json")}).load(); err == nil {
		t.Fatal("missing -E file should fail")
	}
	if err := (envFlags{File: "x.json"}).check("-E", "-e"); ExitCode(err) != 2 {
		t.Fatalf("-E without -e: %v", err)
	}
	if got, err := (envFlags{}).load(); got != nil || err != nil {
		t.Fatalf("no -e: %v, %v", got, err)
	}
}

func TestValidateEnvSuppliesVariable(t *testing.T) {
	path := writeValidateFile(t, "### Users\nGET /users\nX-Token: {{token}}\n\nok\n")
	envFile := filepath.Join(filepath.Dir(path), envFileName)
	if err := os.WriteFile(envFile, []byte(`{"dev":{"token":"abc"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := runValidateArgs(t, "-strict", "-env", "dev", "-env-file", envFile, path); err != nil {
		t.Fatalf("validate -env failed: %v\n%s", err, out)
	}
	if _, err := runValidateArgs(t, "-env", "prod", "-env-file", envFile, path); ExitCode(err) != 1 {
		t.Fatalf("unknown environment: %v", err)
	}
	if _, err := runValidateArgs(t, "-env-file", envFile, path); ExitCode(err) != 2 {
		t.Fatalf("-env-file without -env: %v", err)
	}
}

func TestParseConfigEnvFlags(t *testing.T) {
	cfg, err := parseConfig([]string{"-e", "dev", "--env-file", "x.json", "api.http"})
	if err != nil || cfg.Env != (envFlags{Name: "dev", File: "x.json"}) {
		t.Fatalf("got %+v, %v", cfg.Env, err)
	}
	if _, err := parseConfig([]string{"-E", "x.json", "api.http"}); ExitCode(err) != 2 {
		t.Fatalf("-E without -e: %v", err)
	}
}
