package cli

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestCommandLineModel checks that usageText, which completions and the man
// page are built from, documents every flag parseConfig defines, with the
// right kind (boolean or taking a value).
func TestCommandLineModel(t *testing.T) {
	model := commandLineModel()
	source, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]flagDoc{}
	for _, f := range model.flags {
		for _, name := range f.names() {
			documented[strings.TrimLeft(name, "-")] = f
		}
	}
	defined := map[string]bool{}
	for _, match := range regexp.MustCompile(`\[\]string\{"(\w+)", "([\w-]+)"\}`).FindAllStringSubmatch(string(source), -1) {
		defined[match[1]], defined[match[2]] = true, true
	}
	for _, match := range regexp.MustCompile(`flagSet\.\w+\([^,]+, "([\w-]+)"`).FindAllStringSubmatch(string(source), -1) {
		defined[match[1]] = true
	}
	if len(defined) < 40 {
		t.Fatalf("found only %d flags in config.go; update this test's patterns", len(defined))
	}
	for name := range defined {
		if _, ok := documented[name]; !ok {
			t.Errorf("flag -%s is not in usageText", name)
		}
	}
	for name, f := range documented {
		if name == "config" || name == "no-config" || name == "help" || name == "h" {
			continue // handled before parseConfig
		}
		if !defined[name] {
			t.Errorf("usageText documents -%s, which parseConfig does not define", name)
			continue
		}
		// A boolean flag followed by a non-flag word ends the flags; one
		// that takes a value consumes it.
		cfg, err := parseConfig([]string{"-" + name, "x", "y"})
		takesValue := err != nil || len(cfg.Args) == 1
		if takesValue != (f.value != "") {
			t.Errorf("-%s: usageText says value %q, but parseConfig takes a value: %v", name, f.value, takesValue)
		}
	}
	if !slices.Contains(model.subcommandNames(), "fmt") || !slices.Contains(model.subcommandNames(), "check") {
		t.Errorf("subcommands = %v", model.subcommandNames())
	}
}

func TestManCoversSubcommands(t *testing.T) {
	model := commandLineModel()
	for _, name := range model.subcommandNames() {
		if subcommandSummaries[name] == "" {
			t.Errorf("subcommand %s has no man page summary", name)
		}
	}
	page := manPage(model, "v1.2.3")
	for _, want := range []string{`.TH MOCK 1 "" "mock v1.2.3"`, `.B \-p, \-\-port PORT`, ".B mock fmt", ".B .mockrc"} {
		if !strings.Contains(page, want) {
			t.Errorf("man page lacks %q", want)
		}
	}
}

func TestCompletionScripts(t *testing.T) {
	for _, shell := range completionShells {
		var out bytes.Buffer
		if err := runCompletion([]string{shell}, &out); err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		script := out.String()
		wants := []string{"--upstream", "fmt", "-check"}
		if shell == "fish" {
			wants = []string{"-l upstream", "fmt", "-o check"}
		}
		for _, want := range wants {
			if !strings.Contains(script, want) {
				t.Errorf("%s script lacks %q", shell, want)
			}
		}
		// Check the syntax with the shell when it is installed.
		syntax := map[string][]string{"bash": {"bash", "-n"}, "zsh": {"zsh", "-n"}, "fish": {"fish", "-n"}}[shell]
		if syntax == nil {
			continue
		}
		if _, err := exec.LookPath(syntax[0]); err != nil {
			continue
		}
		cmd := exec.Command(syntax[0], syntax[1:]...)
		cmd.Stdin = strings.NewReader(script)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s -n: %v\n%s", shell, err, output)
		}
	}
	if err := runCompletion([]string{"tcsh"}, &bytes.Buffer{}); ExitCode(err) != 2 {
		t.Errorf("unknown shell: %v", err)
	}
}
