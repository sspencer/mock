package mockhttp

import (
	"bytes"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mock/restclient"
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
	input, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("error reading source file: %w", err)
	}

	if output := legacyValidationError(string(input)); output != "" {
		return output, nil
	}

	normalized := normalizeLegacyFixture(string(input))
	methods, err := restclient.Parse(path, strings.NewReader(normalized))
	if err != nil {
		return err.Error(), nil
	}

	var output strings.Builder
	for _, method := range methods {
		if output.Len() > 0 {
			output.WriteByte('\n')
		}
		bodySize, err := fixtureBodySize(path, method)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&output, "%s: %s %s delay=%s status=%d header=%d body=%d",
			method.Name,
			method.Method,
			method.Path,
			fixtureDelay(method),
			fixtureStatus(method),
			fixtureHeaderSize(method),
			bodySize,
		)
	}
	return output.String(), nil
}

func legacyValidationError(input string) string {
	for i, line := range strings.Split(input, "\n") {
		trimmed := strings.TrimSpace(line)
		if raw, ok := strings.CutPrefix(trimmed, "# @status="); ok {
			status, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil || status < 100 || status > 599 {
				return fmt.Sprintf("invalid status, line %d: %s", i+1, line)
			}
		}
		if raw, ok := strings.CutPrefix(trimmed, "# @delay="); ok {
			if _, err := time.ParseDuration(strings.TrimSpace(raw)); err != nil {
				return fmt.Sprintf("invalid duration, line %d: %s", i+1, line)
			}
		}
	}
	return ""
}

func normalizeLegacyFixture(input string) string {
	lines := strings.Split(input, "\n")
	var normalized []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@") {
			continue
		}
		if trimmed == "###" && onlyBlankLines(lines[i+1:]) {
			continue
		}
		normalized = append(normalized, strings.Replace(line, "# @", "# $", 1))
	}
	return strings.Join(normalized, "\n")
}

func onlyBlankLines(lines []string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	return true
}

func fixtureDelay(method restclient.Method) time.Duration {
	delay, _ := time.ParseDuration(method.Variables["delay"])
	return delay
}

func fixtureStatus(method restclient.Method) int {
	status, err := strconv.Atoi(method.Variables["status"])
	if err != nil || status == 0 {
		return 200
	}
	return status
}

func fixtureHeaderSize(method restclient.Method) int {
	size := 0
	for name, values := range method.Headers {
		for _, value := range values {
			size += len(name) + len(value)
		}
	}
	if method.Headers.Get("Content-Type") != "" {
		return size
	}
	if filePath := method.Variables["file"]; filePath != "" {
		if contentType := mime.TypeByExtension(filepath.Ext(filePath)); contentType != "" {
			return len("Content-Type") + len(contentType)
		}
	}
	if method.Headers.Get("Content-Type") == "" {
		size += 36
	}
	return size
}

func fixtureBodySize(source string, method restclient.Method) (int, error) {
	if filePath := method.Variables["file"]; filePath != "" {
		body, err := os.ReadFile(filepath.Join(filepath.Dir(source), filePath))
		if err != nil {
			return 0, err
		}
		return len(body), nil
	}
	return len(method.Body) + strings.Count(method.Body, "\n"), nil
}
