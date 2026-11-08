package cli

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const initUsage = "usage: mock init [-force] [dir]"

// starterFiles is the commented starter API that mock init writes.
//
//go:embed starter/api.http starter/users.json
var starterFiles embed.FS

// starterNames are written in this order; api.http is the one to run.
var starterNames = []string{"api.http", "users.json"}

// runInit writes the starter files into dir (default the current
// directory). It refuses to overwrite existing files unless -force is given.
func runInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("force", false, "overwrite existing starter files")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fmt.Fprintln(stdout, initUsage)
			fs.PrintDefaults()
			return nil
		}
		return usageError("%v\n%s", err, initUsage)
	}
	if fs.NArg() > 1 {
		return usageError("too many arguments\n%s", initUsage)
	}
	dir := "."
	if fs.NArg() == 1 {
		dir = fs.Arg(0)
	}

	if !*force {
		var existing []string
		for _, name := range starterNames {
			if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
				existing = append(existing, filepath.Join(dir, name))
			}
		}
		if len(existing) > 0 {
			verb := "exists"
			if len(existing) > 1 {
				verb = "exist"
			}
			return runError("%s already %s; use -force to overwrite", strings.Join(existing, " and "), verb)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return runError("create %s: %v", dir, err)
	}
	for _, name := range starterNames {
		data, err := starterFiles.ReadFile("starter/" + name)
		if err != nil {
			return runError("read starter %s: %v", name, err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return runError("write %s: %v", path, err)
		}
		fmt.Fprintf(stdout, "wrote %s\n", path)
	}
	run := "mock api.http"
	if dir != "." {
		run = "mock " + filepath.Join(dir, "api.http")
	}
	fmt.Fprintf(stdout, "\nNext:\n  %s\n  curl localhost:8080/users/7\n", run)
	return nil
}
