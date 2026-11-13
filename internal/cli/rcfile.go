package cli

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// projectConfigName is the project config file looked up in the working
// directory.
const projectConfigName = ".mockrc"

// projectConfig is a project config file (.mockrc, or --config FILE): the
// flags a project always passes, written once. Each line holds a flag and its
// value as on the command line, or request files and directories; blank lines
// and # comments are skipped. Words are split and quoted like shell words.
type projectConfig struct {
	// path is the file read, "" when there is none.
	path string
	// flags go before the command-line flags, so the command line wins for
	// a single-valued flag and adds to a repeatable one.
	flags []string
	// files are used when the command line names no request files.
	files []string
}

// loadProjectConfig reads the project config for a command line and returns
// the arguments without --config and --no-config, which it handles itself.
// args ends at "--" for mock run's command, which is not searched.
func loadProjectConfig(args []string) (projectConfig, []string, error) {
	path, explicit, disabled := "", false, false
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") || (name != "config" && name != "no-config") {
			rest = append(rest, arg)
			continue
		}
		if name == "no-config" {
			if hasValue {
				return projectConfig{}, nil, usageError("--no-config takes no value")
			}
			disabled = true
			continue
		}
		if !hasValue {
			if i+1 == len(args) {
				return projectConfig{}, nil, usageError("--config needs a file name")
			}
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return projectConfig{}, nil, usageError("--config needs a file name")
		}
		path, explicit = value, true
	}
	if disabled {
		if explicit {
			return projectConfig{}, nil, usageError("--config and --no-config cannot be used together")
		}
		return projectConfig{}, rest, nil
	}
	if !explicit {
		path = projectConfigName
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && errors.Is(err, fs.ErrNotExist) {
			return projectConfig{}, rest, nil
		}
		return projectConfig{}, nil, runError("cannot read config file: %v", err)
	}
	pc, err := parseProjectConfig(path, string(data))
	if err != nil {
		return projectConfig{}, nil, err
	}
	return pc, rest, nil
}

// parseProjectConfig splits a config file's lines into flags and files.
func parseProjectConfig(path, text string) (projectConfig, error) {
	pc := projectConfig{path: path}
	for number, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words, err := shellWords(line)
		if err != nil {
			return projectConfig{}, usageError("%s:%d: %v", path, number+1, err)
		}
		var texts []string
		for _, word := range words {
			if word.separator {
				return projectConfig{}, usageError("%s:%d: %q is not allowed; quote values that hold it", path, number+1, word.text)
			}
			texts = append(texts, word.text)
		}
		if len(texts) == 0 {
			continue
		}
		if !strings.HasPrefix(texts[0], "-") {
			pc.files = append(pc.files, texts...)
			continue
		}
		if texts[0] == "--" || strings.TrimLeft(texts[0], "-") == "config" || strings.TrimLeft(texts[0], "-") == "no-config" {
			return projectConfig{}, usageError("%s:%d: %s cannot be used in a config file", path, number+1, texts[0])
		}
		for _, word := range texts[1:] {
			if strings.HasPrefix(word, "-") && len(word) > 1 {
				return projectConfig{}, usageError("%s:%d: put one flag on each line (found %s after %s)", path, number+1, word, texts[0])
			}
		}
		if len(texts) > 2 {
			return projectConfig{}, usageError("%s:%d: %s takes one value; quote a value that holds spaces", path, number+1, texts[0])
		}
		pc.flags = append(pc.flags, texts...)
	}
	return pc, nil
}

// parseConfigWith parses a command line under a project config: its flags
// first, so the command line wins, and its files when the command line names
// none.
func parseConfigWith(pc projectConfig, args []string) (config, error) {
	cfg, err := parseConfig(append(append([]string(nil), pc.flags...), args...))
	if err != nil {
		if pc.path != "" {
			// Say so when the config file's own flags do not parse. Checks
			// between flags are left out: the command line may complete them.
			if _, own := parseConfig(pc.flags); own != nil && strings.HasPrefix(own.Error(), "failed to parse flags") {
				return config{}, usageError("%s: %v", pc.path, own)
			}
		}
		return config{}, err
	}
	if len(cfg.Args) == 0 {
		cfg.Args = pc.files
	}
	cfg.ConfigFile = pc.path
	return cfg, nil
}

// wantsHelp reports whether the flags before "--" ask for help, which is
// answered before a config file is read so a broken one cannot hide it.
func wantsHelp(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--":
			return false
		case "-h", "--h", "-help", "--help":
			return true
		}
	}
	return false
}
