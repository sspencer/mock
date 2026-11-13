package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sspencer/mock/restclient"
)

const fmtUsage = "usage: mock fmt [-check] <file.http|dir|-> [file.http|dir...]"

// runFmt rewrites request files in the layout restclient.Format produces and
// prints the name of each file it changed. With -check it changes nothing
// and fails when a file would change; "-" formats stdin to stdout.
func runFmt(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mock fmt", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	check := flags.Bool("check", false, "list files that are not formatted and exit 1, without changing them")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, fmtUsage)
			fmt.Fprint(stdout, "\nWithout -check, each file that is not formatted is rewritten in place and named on stdout.\n")
			return nil
		}
		return usageError("%v\n%s", err, fmtUsage)
	}
	if flags.NArg() == 0 {
		return usageError("missing request files\n%s", fmtUsage)
	}
	if flags.NArg() == 1 && flags.Arg(0) == "-" {
		src, err := io.ReadAll(stdin)
		if err != nil {
			return runError("mock fmt: %v", err)
		}
		out, err := restclient.Format("<stdin>", src)
		if err != nil {
			return runError("%v", err)
		}
		if *check {
			if !bytes.Equal(src, out) {
				fmt.Fprintln(stdout, "<stdin>")
				return runError("mock fmt: <stdin> is not formatted")
			}
			return nil
		}
		_, err = stdout.Write(out)
		return err
	}

	var files []string
	for _, arg := range flags.Args() {
		info, err := os.Stat(arg)
		if err != nil {
			return runError("mock fmt: %v", err)
		}
		if !info.IsDir() {
			files = append(files, arg)
			continue
		}
		found, err := findRequestFiles(arg)
		if err != nil {
			return runError("mock fmt: %v", err)
		}
		files = append(files, found...)
	}
	var failed, unformatted int
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(stderr, "mock fmt: %v\n", err)
			failed++
			continue
		}
		out, err := restclient.Format(file, src)
		if err != nil {
			fmt.Fprintln(stderr, err)
			failed++
			continue
		}
		if bytes.Equal(src, out) {
			continue
		}
		fmt.Fprintln(stdout, file)
		if *check {
			unformatted++
			continue
		}
		if err := replaceFile(file, out); err != nil {
			fmt.Fprintf(stderr, "mock fmt: %v\n", err)
			failed++
		}
	}
	switch {
	case failed > 0:
		return runError("mock fmt: %s could not be formatted", plural(uint64(failed), "file"))
	case unformatted > 0:
		return runError("mock fmt: %s not formatted; run mock fmt to fix", plural(uint64(unformatted), "file"))
	}
	return nil
}

// replaceFile writes content to file through a temporary file in the same
// directory, so a running mock never reloads half a file. It keeps the
// file's permissions, and a symlink keeps pointing at the rewritten target.
func replaceFile(file string, content []byte) error {
	if target, err := filepath.EvalSymlinks(file); err == nil {
		file = target
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(file); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, file)
}
