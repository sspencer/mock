package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sspencer/mock/skills"
)

const skillUsage = "usage: mock skill [-force] [dir]"

// runSkill writes the agent skill for this version of mock. With no
// directory it prints SKILL.md to stdout; with one it writes
// dir/mock/SKILL.md, the layout agent tools read skills from (such as
// .claude/skills). It refuses to replace a different file unless -force is
// given; an identical file is left alone.
func runSkill(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock skill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "replace an existing SKILL.md")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fmt.Fprintln(stdout, skillUsage)
			fs.PrintDefaults()
			return nil
		}
		return usageError("%v\n%s", err, skillUsage)
	}
	if fs.NArg() > 1 {
		return usageError("too many arguments\n%s", skillUsage)
	}
	if fs.NArg() == 0 {
		if *force {
			return usageError("-force needs a directory\n%s", skillUsage)
		}
		_, err := stdout.Write(skills.Mock)
		return err
	}

	path := filepath.Join(fs.Arg(0), "mock", "SKILL.md")
	old, err := os.ReadFile(path)
	switch {
	case err == nil && bytes.Equal(old, skills.Mock):
		fmt.Fprintf(stdout, "%s is up to date\n", path)
		return nil
	case err == nil && !*force:
		return runError("%s already exists; use -force to replace it", path)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return runError("read %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return runError("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, skills.Mock, 0o644); err != nil {
		return runError("write %s: %v", path, err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	return nil
}
