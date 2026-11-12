package restclient

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// expandImports replaces each file-scoped @import in defs with the
// file-scoped definitions of the file it names, recursively, so they apply
// where the @import stands: definitions after it override imported ones. It
// returns the definitions and every imported file. chain holds the files
// being imported, to report cycles.
func expandImports(source string, defs []definition, chain []string) ([]definition, []string, error) {
	var out []definition
	var imports []string
	for _, def := range defs {
		if def.name != "import" {
			out = append(out, def)
			continue
		}
		raw := strings.TrimSpace(def.value)
		if raw == "" || strings.Contains(raw, "{{") {
			return nil, nil, parseErrorf(def.in(source), def.line, `@import needs a file path without placeholders, such as "@import = ./common.http"`)
		}
		path := raw
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(def.in(source)), raw)
		}
		key := importKey(path)
		if slices.Contains(chain, key) {
			names := append(append([]string(nil), chain...), key)
			return nil, nil, parseErrorf(def.in(source), def.line, "@import cycle: %s", strings.Join(names, " -> "))
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, nil, parseErrorf(def.in(source), def.line, "@import %s: %v", raw, err)
		}
		_, imported, err := scanFile(path, file)
		file.Close()
		if err != nil {
			return nil, nil, err
		}
		imported, nested, err := expandImports(path, imported, append(chain, key))
		if err != nil {
			return nil, nil, err
		}
		out = append(out, imported...)
		for _, p := range append([]string{path}, nested...) {
			if !slices.Contains(imports, p) {
				imports = append(imports, p)
			}
		}
	}
	return out, imports, nil
}

// importKey identifies a file for cycle detection: its absolute, cleaned
// path.
func importKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// ImportDependencies returns every file brought in with @import by methods,
// once each.
func ImportDependencies(methods []Method) []string {
	var deps []string
	for _, method := range methods {
		for _, path := range method.Imports {
			if !slices.Contains(deps, path) {
				deps = append(deps, path)
			}
		}
	}
	return deps
}
