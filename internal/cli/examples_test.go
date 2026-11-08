package cli

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// knownExampleWarnings lists examples that warn on purpose, with the number
// of warnings each should report. The README documents small.http's.
var knownExampleWarnings = map[string]int{
	"small.http": 2,
}

// TestExamplesValidate runs `mock validate -strict` over every bundled
// example, alone and (for examples/matching) as the set the README serves.
func TestExamplesValidate(t *testing.T) {
	root := filepath.Join("..", "..", "examples")
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".http") {
			files = append(files, path)
		}
		return err
	})
	if err != nil || len(files) == 0 {
		t.Fatalf("no examples found under %s: %v", root, err)
	}

	for _, file := range files {
		rel, _ := filepath.Rel(root, file)
		t.Run(rel, func(t *testing.T) {
			wantWarnings := knownExampleWarnings[rel]
			report := validateExample(t, file)
			warnings := 0
			for _, d := range report.Diagnostics {
				if d.Level == "error" {
					t.Errorf("error: %s", d)
				} else {
					warnings++
				}
			}
			if warnings != wantWarnings {
				t.Errorf("warnings = %d, want %d: %+v", warnings, wantWarnings, report.Diagnostics)
			}
			if report.Routes == 0 && len(report.Diagnostics) == 0 {
				t.Error("example defines no routes")
			}
		})
	}

	matching, _ := filepath.Glob(filepath.Join(root, "matching", "*.http"))
	if out, err := runValidateArgs(t, append([]string{"-strict"}, matching...)...); err != nil {
		t.Errorf("examples/matching/*.http together: %v\n%s", err, out)
	}
}

func validateExample(t *testing.T, file string) validateReport {
	t.Helper()
	out, _ := runValidateArgs(t, "-json", file)
	var report validateReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	return report
}
