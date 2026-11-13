package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// Environment files written for the REST Client extensions. A JetBrains
// file maps environment names to variables, and its private sibling
// overrides it; VS Code keeps the same map under one settings key.
const (
	envFileName        = "http-client.env.json"
	privateEnvFileName = "http-client.private.env.json"
	vscodeSettingsPath = ".vscode/settings.json"
	vscodeEnvKey       = "rest-client.environmentVariables"
	sharedEnvName      = "$shared"
)

// envFlags select a REST Client environment (-e) and, optionally, the file
// that defines it (-E).
type envFlags struct {
	Name, File string
}

// options returns the parse options for the -v variables and the selected
// environment, whose values fill in variables no @name defines.
func (e envFlags) options(vars varFlag) (restclient.Options, error) {
	defaults, err := e.load()
	if err != nil {
		return restclient.Options{}, err
	}
	return restclient.Options{Vars: vars, Defaults: defaults}, nil
}

// paths lists the files the selected environment may be read from, for the
// reload watcher: -E's file (with its private sibling), or every default
// location. Nil without -e.
func (e envFlags) paths() []string {
	if e.Name == "" {
		return nil
	}
	if e.File == "" {
		return []string{envFileName, privateEnvFileName, vscodeSettingsPath}
	}
	paths := []string{e.File}
	if filepath.Base(e.File) == envFileName {
		paths = append(paths, filepath.Join(filepath.Dir(e.File), privateEnvFileName))
	}
	return paths
}

// check reports a usage error for -E without -e.
func (e envFlags) check(short, long string) error {
	if e.File != "" && e.Name == "" {
		return usageError("%s needs %s: name the environment to use from %s", short, long, e.File)
	}
	return nil
}

// load returns the selected environment's variables, nil without -e.
func (e envFlags) load() (map[string]string, error) {
	if e.Name == "" {
		return nil, nil
	}
	envs, source, err := readEnvironments(e.File)
	if err != nil {
		return nil, err
	}
	return selectEnvironment(envs, e.Name, source)
}

// readEnvironments reads the environment map from file, or with no file
// from http-client.env.json (and its private sibling) in the working
// directory, else from .vscode/settings.json.
func readEnvironments(file string) (map[string]map[string]any, string, error) {
	if file == "" {
		if _, err := os.Stat(envFileName); err == nil {
			file = envFileName
		} else if _, perr := os.Stat(privateEnvFileName); perr == nil {
			file = envFileName
		} else if _, verr := os.Stat(vscodeSettingsPath); verr == nil {
			file = vscodeSettingsPath
		} else {
			return nil, "", fmt.Errorf("no REST Client environments: %s, %s and %s are missing", envFileName, privateEnvFileName, vscodeSettingsPath)
		}
	}
	paths := []string{file}
	if filepath.Base(file) == envFileName {
		paths = append(paths, filepath.Join(filepath.Dir(file), privateEnvFileName))
	}
	merged := map[string]map[string]any{}
	found := false
	for i, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) && (i > 0 || len(paths) > 1) {
			continue // Either the shared or the private file may be missing.
		}
		if err != nil {
			return nil, "", err
		}
		found = true
		envs, err := parseEnvironments(path, data)
		if err != nil {
			return nil, "", err
		}
		for name, vars := range envs {
			if merged[name] == nil {
				merged[name] = map[string]any{}
			}
			maps.Copy(merged[name], vars)
		}
	}
	if !found {
		return nil, "", fmt.Errorf("%s: %w", file, os.ErrNotExist)
	}
	return merged, file, nil
}

// parseEnvironments decodes an environment file, or the environments in a
// VS Code settings file. Both may hold comments and trailing commas.
func parseEnvironments(path string, data []byte) (map[string]map[string]any, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stripJSONC(data), &top); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if raw, ok := top[vscodeEnvKey]; ok {
		if err := json.Unmarshal(raw, &top); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", path, vscodeEnvKey, err)
		}
	}
	envs := map[string]map[string]any{}
	for name, raw := range top {
		var vars map[string]any
		if err := json.Unmarshal(raw, &vars); err != nil {
			continue // A VS Code setting or a value that is not an environment.
		}
		envs[name] = vars
	}
	return envs, nil
}

// sharedReference is {{$shared name}} in a value, which REST Client resolves
// from the $shared environment.
var sharedReference = regexp.MustCompile(`\{\{\s*\$shared\s+([^{}\s]+)\s*\}\}`)

// selectEnvironment merges $shared under the named environment. Values that
// are not strings, numbers or booleans, and names mock cannot set (request
// matchers and control variables such as port), are left out.
func selectEnvironment(envs map[string]map[string]any, name, source string) (map[string]string, error) {
	env, ok := envs[name]
	if !ok || name == sharedEnvName {
		var names []string
		for n := range envs {
			if n != sharedEnvName {
				names = append(names, n)
			}
		}
		slices.Sort(names)
		return nil, fmt.Errorf("%s: no environment %q (have %s)", source, name, strings.Join(names, ", "))
	}
	shared := scalarValues(envs[sharedEnvName])
	values := maps.Clone(shared)
	for key, value := range scalarValues(env) {
		values[key] = sharedReference.ReplaceAllStringFunc(value, func(ref string) string {
			if v, ok := shared[sharedReference.FindStringSubmatch(ref)[1]]; ok {
				return v
			}
			return ref
		})
	}
	maps.DeleteFunc(values, func(key, _ string) bool { return !restclient.IsDefaultVariable(key) })
	return values, nil
}

// scalarValues converts the string, number and boolean values to text.
func scalarValues(vars map[string]any) map[string]string {
	out := map[string]string{}
	for key, value := range vars {
		switch v := value.(type) {
		case string:
			out[key] = v
		case float64:
			out[key] = strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			out[key] = strconv.FormatBool(v)
		}
	}
	return out
}

// stripJSONC removes // and /* */ comments and trailing commas outside
// strings, as VS Code settings allow.
func stripJSONC(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case inString:
			out = append(out, c)
			if c == '\\' && i+1 < len(data) {
				i++
				out = append(out, data[i])
			} else if c == '"' {
				inString = false
			}
		case c == '"':
			inString = true
			out = append(out, c)
		case c == '/' && i+1 < len(data) && data[i+1] == '/':
			for i < len(data) && data[i] != '\n' {
				i++
			}
			i--
		case c == '/' && i+1 < len(data) && data[i+1] == '*':
			end := strings.Index(string(data[i+2:]), "*/")
			if end < 0 {
				return out
			}
			i += end + 3
		case c == '}' || c == ']':
			// Drop a comma that only whitespace separates from the close.
			j := len(out) - 1
			for j >= 0 && strings.ContainsRune(" \t\r\n", rune(out[j])) {
				j--
			}
			if j >= 0 && out[j] == ',' {
				out = append(out[:j], out[j+1:]...)
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}
