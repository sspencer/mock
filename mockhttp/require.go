package mockhttp

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/sspencer/mock/restclient"
)

// unavailable says why a route that matches the request still cannot answer
// it: its @require reference has no value, or its "< path" file is missing
// under @missing = next. It returns "" when the route can answer.
func unavailable(method *restclient.Method, values templateValues) string {
	if method.Require == "" && !(method.MissingNext && method.File != "") {
		return ""
	}
	render := newRenderer(method, values)
	if method.Require != "" {
		ref := templateNode{kind: placeholderNode, prefix: "$", name: method.Require}
		if _, ok := render.evaluate(ref, render.root); !ok {
			return "Required $" + method.Require + " has no value for this request"
		}
	}
	if method.MissingNext && method.File != "" {
		file, _, err := render.file()
		if err != nil || !responseFileExists(method.Source, file.name) {
			return "Response file " + method.File + " does not exist for this request (@missing = next)"
		}
	}
	return ""
}

// responseFileExists reports whether a "< path" file exists in one of the
// roots readResponseFile reads from.
func responseFileExists(source, name string) bool {
	for _, dir := range restclient.BodyFileRoots(source) {
		root, err := os.OpenRoot(dir)
		if err != nil {
			continue
		}
		info, err := root.Stat(filepath.Clean(name))
		root.Close()
		if err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func describeRequire(method restclient.Method) string {
	if method.Require == "" {
		return ""
	}
	return "$" + method.Require
}

func describeMissing(method restclient.Method) string {
	switch {
	case method.MissingNext:
		return "next"
	case method.Missing != 0:
		return strconv.Itoa(method.Missing)
	}
	return ""
}
