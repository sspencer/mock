package mock

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParser(t *testing.T) {

	// Find the paths of all input files in the data directory.
	paths, err := filepath.Glob(filepath.Join("testdata", "*.http"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range paths {
		_, filename := filepath.Split(path)
		testname := filename[:len(filename)-len(filepath.Ext(path))]

		// Each path turns into a test: the test name is the filename without the
		// extension.
		t.Run(testname, func(t *testing.T) {
			source, err := os.Open(path)
			if err != nil {
				t.Fatal("error reading source file:", err)
			}

			delay := time.Duration(0)
			sp := &parser{baseDir: "testdata", defaultDelay: delay}
			var output string
			err = sp.parse(source)

			if err != nil {
				output = err.Error()
			} else {
				output = sp.String()
			}

			gold := filepath.Join("testdata", testname+".golden")
			want, err := os.ReadFile(gold)
			if err != nil {
				t.Fatal("error reading good file:", err)
			}

			if !bytes.Equal([]byte(strings.TrimSpace(output)), bytes.TrimSpace(want)) {
				t.Errorf("\n==== got:\n%s\n==== want:\n%s\n", output, want)
			}
		})
	}
}
