package cli

import (
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseProjectConfig(t *testing.T) {
	pc, err := parseProjectConfig(".mockrc", `# project flags
--port 9000
-R
--upstream-header 'Authorization: Bearer dev token'
-v name=value

api.http  more.http
--strict=unused
`)
	if err != nil {
		t.Fatal(err)
	}
	wantFlags := []string{"--port", "9000", "-R", "--upstream-header", "Authorization: Bearer dev token", "-v", "name=value", "--strict=unused"}
	if !slices.Equal(pc.flags, wantFlags) {
		t.Errorf("flags = %q, want %q", pc.flags, wantFlags)
	}
	if !slices.Equal(pc.files, []string{"api.http", "more.http"}) {
		t.Errorf("files = %q", pc.files)
	}
}

func TestParseProjectConfigErrors(t *testing.T) {
	for _, test := range []struct{ text, want string }{
		{"-p 9000 -R\n", ".mockrc:1: put one flag on each line"},
		{"\n-H X: y\n", ".mockrc:2: -H takes one value"},
		{"--config other\n", "cannot be used in a config file"},
		{"-v 'unterminated\n", ".mockrc:1:"},
		{"-v a;b\n", "is not allowed"},
	} {
		_, err := parseProjectConfig(".mockrc", test.text)
		if err == nil || !strings.Contains(err.Error(), test.want) || ExitCode(err) != 2 {
			t.Errorf("%q: err = %v, want a usage error with %q", test.text, err, test.want)
		}
	}
}

func TestProjectConfigPrecedence(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".mockrc", []byte("-p 9000\n-a 9001\n-H 'X-From: config'\n-d mocks\napi.http\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pc, args, err := loadProjectConfig([]string{"-p", "7000", "-H", "X-Cli: yes"})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := parseConfigWith(pc, args)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 7000 || cfg.AdminPort != 9001 {
		t.Errorf("ports = %d, %d; want the command line's 7000 and the config's 9001", cfg.Port, cfg.AdminPort)
	}
	if len(cfg.Headers) != 2 || !slices.Equal(cfg.Dirs, []string{"mocks"}) {
		t.Errorf("headers = %v, dirs = %v; repeatable flags should combine", cfg.Headers, cfg.Dirs)
	}
	if !slices.Equal(cfg.Args, []string{"api.http"}) || cfg.ConfigFile != ".mockrc" {
		t.Errorf("args = %q, config = %q", cfg.Args, cfg.ConfigFile)
	}

	// Files on the command line replace the config's files.
	cfg, err = parseConfigWith(pc, []string{"other.http"})
	if err != nil || !slices.Equal(cfg.Args, []string{"other.http"}) {
		t.Errorf("args = %q, %v; want only other.http", cfg.Args, err)
	}

	// --no-config ignores the file; --config reads another.
	pc, args, err = loadProjectConfig([]string{"--no-config", "x.http"})
	if err != nil || pc.path != "" || !slices.Equal(args, []string{"x.http"}) {
		t.Errorf("--no-config: %+v %q %v", pc, args, err)
	}
	if err := os.WriteFile("ci.rc", []byte("-s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pc, args, err = loadProjectConfig([]string{"--config=ci.rc", "x.http"})
	if err != nil || pc.path != "ci.rc" || !slices.Equal(pc.flags, []string{"-s"}) || !slices.Equal(args, []string{"x.http"}) {
		t.Errorf("--config=ci.rc: %+v %q %v", pc, args, err)
	}
	if _, _, err := loadProjectConfig([]string{"-config", "missing.rc"}); err == nil {
		t.Error("a missing --config file should be an error")
	}
	if _, _, err := loadProjectConfig([]string{"--config", "ci.rc", "--no-config"}); ExitCode(err) != 2 {
		t.Errorf("--config with --no-config: %v", err)
	}
	// mock run's command after -- is not searched.
	_, args, err = loadProjectConfig([]string{"--no-config", "--", "tool", "--config", "x"})
	if err != nil || !slices.Equal(args, []string{"--", "tool", "--config", "x"}) {
		t.Errorf("args after --: %q, %v", args, err)
	}
}

func TestProjectConfigNamesFileInFlagErrors(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(".mockrc", []byte("--no-such-flag\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run(context.Background(), []string{"api.http"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), ".mockrc: ") || ExitCode(err) != 2 {
		t.Fatalf("err = %v, want a usage error naming .mockrc", err)
	}
	// Help still works with a broken config file.
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"-h"}, strings.NewReader(""), &out, &bytes.Buffer{}, nil); err != nil || !strings.Contains(out.String(), "--no-config") {
		t.Fatalf("help: %v", err)
	}
}

// A check between flags that the command line completes is not blamed on
// the config file.
func TestProjectConfigCrossFlagCheck(t *testing.T) {
	pc := projectConfig{path: ".mockrc", flags: []string{"-r", "rec.http"}}
	if _, err := parseConfigWith(pc, []string{"-u", "http://example.com", "api.http"}); err != nil {
		t.Fatal(err)
	}
	_, err := parseConfigWith(pc, []string{"api.http"})
	if err == nil || strings.HasPrefix(err.Error(), ".mockrc") {
		t.Fatalf("err = %v, want the -r check without the file name", err)
	}
}
