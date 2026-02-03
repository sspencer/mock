package data

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParser(t *testing.T) {

	// Find the paths of all input files in the data directory.
	paths, err := filepath.Glob(filepath.Join("testdata", "*.http"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range paths {
		_, filename := filepath.Split(path)
		testName := filename[:len(filename)-len(filepath.Ext(path))]

		// Each path turns into a test: the test name is the filename without the
		// extension.
		t.Run(testName, func(t *testing.T) {
			source, err := os.Open(path)
			if err != nil {
				t.Fatal("error reading source file:", err)
			}

			sp := NewParser("testdata", "")

			var output string
			err = sp.Parse(source)

			if err != nil {
				output = err.Error()
			} else {
				// Type assert to access String() method on concrete parser type
				if p, ok := sp.(*parser); ok {
					output = p.String()
				}
			}

			gold := filepath.Join("testdata", testName+".golden")
			want, err := os.ReadFile(gold)
			if err != nil {
				t.Fatal("error reading good file:", err)
			}

			if !bytes.Equal([]byte(strings.TrimSpace(output)), bytes.TrimSpace(want)) {
				t.Errorf("test %q \n==== got:\n%s\n==== want:\n%s\n", testName, output, want)
			}
		})
	}
}
