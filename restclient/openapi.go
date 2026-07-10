package restclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// OpenAPI is a minimal OpenAPI 3 document subset used to seed mock routes.
type OpenAPI struct {
	OpenAPI string                    `json:"openapi" yaml:"openapi"`
	Paths   map[string]map[string]any `json:"paths" yaml:"paths"`
}

// LoadOpenAPI reads an OpenAPI 3 JSON or YAML file and returns stub Methods
// (one per path+operation) with empty 200 responses.
func LoadOpenAPI(path string) ([]Method, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc OpenAPI
	if err := json.Unmarshal(data, &doc); err != nil {
		if yerr := yaml.Unmarshal(data, &doc); yerr != nil {
			return nil, fmt.Errorf("%s: not valid OpenAPI JSON/YAML: %v / %v", path, err, yerr)
		}
	}
	if doc.Paths == nil {
		return nil, fmt.Errorf("%s: OpenAPI document has no paths", path)
	}

	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var methods []Method
	httpMethods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions,
	}
	for _, p := range paths {
		ops := doc.Paths[p]
		// Convert OpenAPI {id} path params to :id for this server.
		mockPath := openAPIPathToMock(p)
		for _, methodName := range httpMethods {
			op, ok := ops[strings.ToLower(methodName)]
			if !ok {
				continue
			}
			name := fmt.Sprintf("%s %s", methodName, p)
			if opMap, ok := op.(map[string]any); ok {
				if opID, ok := opMap["operationId"].(string); ok && opID != "" {
					name = opID
				} else if summary, ok := opMap["summary"].(string); ok && summary != "" {
					name = summary
				}
			}
			methods = append(methods, Method{
				Name:         name,
				Method:       methodName,
				Path:         mockPath,
				Query:        url.Values{},
				Variables:    map[string]string{"status": "200"},
				MatchHeaders: make(http.Header),
				Headers:      http.Header{"Content-Type": []string{"application/json"}},
				Body:         `{"ok":true,"path":"` + mockPath + `"}`,
				Source:       path,
			})
		}
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("%s: no HTTP operations found in OpenAPI paths", path)
	}
	return methods, nil
}

func openAPIPathToMock(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); {
		if path[i] == '{' {
			end := strings.IndexByte(path[i:], '}')
			if end < 0 {
				b.WriteByte(path[i])
				i++
				continue
			}
			name := path[i+1 : i+end]
			b.WriteByte(':')
			b.WriteString(name)
			i += end + 1
			continue
		}
		b.WriteByte(path[i])
		i++
	}
	return b.String()
}
