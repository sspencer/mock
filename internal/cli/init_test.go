package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesValidStarter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "api")
	var out bytes.Buffer
	if err := runInit([]string{dir}, &out); err != nil {
		t.Fatal(err)
	}
	for _, name := range starterNames {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
	if !strings.Contains(out.String(), "mock "+filepath.Join(dir, "api.http")) {
		t.Errorf("output = %q", out.String())
	}

	var report bytes.Buffer
	if err := runValidate([]string{"-strict", filepath.Join(dir, "api.http")}, &report); err != nil {
		t.Fatalf("starter does not validate: %v\n%s", err, report.String())
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "api.http")
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runInit([]string{dir}, &bytes.Buffer{})
	if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "-force") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Error("existing file was overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "users.json")); err == nil {
		t.Error("users.json written despite the refusal")
	}
	if err := runInit([]string{"-force", dir}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) == "mine" {
		t.Error("-force did not overwrite")
	}
	if err := runInit([]string{"a", "b"}, &bytes.Buffer{}); ExitCode(err) != 2 {
		t.Errorf("two dirs: %v", err)
	}
}
