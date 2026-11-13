package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const unformattedFile = "###Users\nget  /users\nContent-Type:application/json\n\n[]\n\n\n### Other\nGET /other\n"

func TestFmtRewritesFiles(t *testing.T) {
	dir := t.TempDir()
	messy := filepath.Join(dir, "messy.http")
	clean := filepath.Join(dir, "sub", "clean.http")
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messy, []byte(unformattedFile), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clean, []byte(validFile), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	err := runFmt([]string{"-check", dir}, nil, &out, &errOut)
	if ExitCode(err) != 1 || strings.TrimSpace(out.String()) != messy {
		t.Fatalf("-check: err = %v, out = %q", err, out.String())
	}
	if data, _ := os.ReadFile(messy); string(data) != unformattedFile {
		t.Fatal("-check changed the file")
	}

	out.Reset()
	if err := runFmt([]string{dir}, nil, &out, &errOut); err != nil {
		t.Fatalf("fmt: %v (%s)", err, errOut.String())
	}
	if strings.TrimSpace(out.String()) != messy {
		t.Errorf("fmt listed %q, want only %s", out.String(), messy)
	}
	data, _ := os.ReadFile(messy)
	want := "### Users\nGET /users\nContent-Type: application/json\n\n[]\n\n### Other\nGET /other\n"
	if string(data) != want {
		t.Errorf("formatted:\n%q\nwant:\n%q", data, want)
	}
	if info, _ := os.Stat(messy); info.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v, want 0640 kept", info.Mode().Perm())
	}

	out.Reset()
	if err := runFmt([]string{"-check", dir}, nil, &out, &errOut); err != nil || out.Len() != 0 {
		t.Fatalf("-check after fmt: %v, %q", err, out.String())
	}
}

func TestFmtStdinAndErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := runFmt([]string{"-"}, strings.NewReader("###A\nget /a\n"), &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != "### A\nGET /a\n" {
		t.Errorf("stdin: %q", out.String())
	}

	broken := writeValidateFile(t, brokenFile)
	out.Reset()
	err := runFmt([]string{broken}, nil, &out, &errOut)
	if ExitCode(err) != 1 || !strings.Contains(errOut.String(), broken+":2") {
		t.Fatalf("broken file: err = %v, stderr = %q", err, errOut.String())
	}
	if err := runFmt(nil, nil, &out, &errOut); ExitCode(err) != 2 {
		t.Fatalf("no files: %v", err)
	}
}
