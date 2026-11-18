package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/sspencer/mock/skills"
)

func TestSkillPrints(t *testing.T) {
	var out bytes.Buffer
	if err := runSkill(nil, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), skills.Mock) {
		t.Error("mock skill did not print the embedded SKILL.md")
	}
	if err := runSkill([]string{"-force"}, &out); ExitCode(err) != 2 {
		t.Errorf("-force without a dir: %v", err)
	}
	if err := runSkill([]string{"a", "b"}, &out); ExitCode(err) != 2 {
		t.Errorf("two dirs: %v", err)
	}
}

func TestSkillWrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".claude", "skills")
	path := filepath.Join(dir, "mock", "SKILL.md")
	var out bytes.Buffer
	if err := runSkill([]string{dir}, &out); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, skills.Mock) {
		t.Fatalf("SKILL.md not written: %v", err)
	}
	if !strings.Contains(out.String(), "wrote "+path) {
		t.Errorf("output = %q", out.String())
	}

	out.Reset()
	if err := runSkill([]string{dir}, &out); err != nil {
		t.Fatalf("identical file: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("output = %q", out.String())
	}

	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runSkill([]string{dir}, &out)
	if err == nil || ExitCode(err) != 1 || !strings.Contains(err.Error(), "-force") {
		t.Fatalf("err = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "mine" {
		t.Error("existing file was replaced without -force")
	}
	if err := runSkill([]string{"-force", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !bytes.Equal(data, skills.Mock) {
		t.Error("-force did not replace the file")
	}
}

// TestSkillContent keeps the skill honest: frontmatter agent tools need,
// examples that validate cleanly, and only subcommands that exist.
func TestSkillContent(t *testing.T) {
	text := string(skills.Mock)
	front, _, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !strings.HasPrefix(text, "---\n") || !ok {
		t.Fatal("SKILL.md has no frontmatter")
	}
	if !strings.Contains(front, "name: mock\n") || !strings.Contains(front, "description: ") {
		t.Errorf("frontmatter lacks name or description:\n%s", front)
	}

	blocks := regexp.MustCompile("(?s)```http\n(.*?)```").FindAllStringSubmatch(text, -1)
	if len(blocks) == 0 {
		t.Fatal("no http examples")
	}
	for i, block := range blocks {
		path := filepath.Join(t.TempDir(), "example.http")
		if err := os.WriteFile(path, []byte(block[1]), 0o644); err != nil {
			t.Fatal(err)
		}
		var report bytes.Buffer
		if err := runValidate([]string{"-strict", path}, &report); err != nil {
			t.Errorf("example %d does not validate: %v\n%s", i+1, err, report.String())
		}
	}

	known := commandLineModel().subcommandNames()
	for _, m := range regexp.MustCompile("`mock ([a-z]+)[ `]").FindAllStringSubmatch(text, -1) {
		if !slices.Contains(known, m[1]) {
			t.Errorf("skill mentions unknown subcommand %q", m[1])
		}
	}
}
