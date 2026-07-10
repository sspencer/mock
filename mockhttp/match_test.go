package mockhttp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
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
			output, err := parseFixture(path)
			if err != nil {
				t.Fatal(err)
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

func parseFixture(path string) (string, error) {
	source, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("error reading source file: %w", err)
	}
	defer source.Close()

	methods, err := restclient.Parse(path, source)
	if err != nil {
		return err.Error(), nil
	}
	return formatFixture(methods)
}

func formatFixture(methods []restclient.Method) (string, error) {
	var output strings.Builder
	for _, method := range methods {
		if output.Len() > 0 {
			output.WriteByte('\n')
		}

		filePath, hasFile := resolveFilePath(&method)
		body, err := renderBody(method, nil, filePath, hasFile)
		if err != nil {
			return "", err
		}
		delay, _ := time.ParseDuration(method.Variables["delay"])

		fmt.Fprintf(&output, "%s: %s %s delay=%s status=%d header=%d body=%d",
			method.Name,
			method.Method,
			method.Path,
			delay,
			statusFromVariables(nil, method.Variables),
			headerSize(responseHeaders(method, nil, filePath)),
			len(body),
		)
	}
	return output.String(), nil
}

func headerSize(headers map[string][]string) int {
	size := 0
	for name, values := range headers {
		for _, value := range values {
			size += len(name) + len(value)
		}
	}
	return size
}
