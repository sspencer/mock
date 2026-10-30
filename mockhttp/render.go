package mockhttp

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sspencer/mock/restclient"

	"github.com/jaswdr/faker"
)

var placeholderPattern = restclient.PlaceholderPattern

var fakerPool = sync.Pool{
	New: func() any {
		return faker.New()
	},
}

func statusAllowsBody(status int) bool {
	return status != http.StatusNoContent && status != http.StatusNotModified
}

func responseHeaders(method restclient.Method, values map[string]string, filePath string) http.Header {
	headers := method.Headers.Clone()
	for name, headerValues := range headers {
		for i, value := range headerValues {
			headerValues[i] = expandPlaceholders(value, method, values)
		}
		headers[name] = headerValues
	}
	if headers.Get("Content-Type") != "" || method.Body != "" || filePath == "" {
		return headers
	}
	if contentType := mime.TypeByExtension(filepath.Ext(filePath)); contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	return headers
}

func renderBody(method restclient.Method, values map[string]string, filePath string, hasFile bool) ([]byte, error) {
	if method.Body == "" {
		if hasFile {
			body, err := readResponseFile(method)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filePath, err)
			}
			// Expand placeholders only when the file looks like text.
			if fileTemplatesEnabled(method, filePath) && isMostlyText(body) {
				return []byte(expandBodyPlaceholders(string(body), method, values)), nil
			}
			return body, nil
		}
		return nil, nil
	}

	return []byte(expandBodyPlaceholders(method.Body, method, values)), nil
}

func expandPlaceholders(input string, method restclient.Method, values map[string]string) string {
	return restclient.Expand(input, method.Variables, func(key string, args []string) (string, bool) {
		if len(args) > 0 {
			value, err := callFunction(key, args, time.Now())
			return value, err == nil
		}
		if value, ok := values[key]; ok {
			return value, true
		}
		if value, err := callFunction(key, nil, time.Now()); err == nil {
			return value, true
		}
		if value := generatedValue(key); value != "" {
			return value, true
		}
		return "", false
	})
}

func isMostlyText(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	if !utf8.Valid(body) {
		return false
	}
	for _, b := range body {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' || b == 127 {
			return false
		}
	}
	return true
}

func resolveFilePath(method *restclient.Method) (string, bool) {
	if method.File == "" {
		return "", false
	}
	return restclient.ResolveFile(method.Source, method.File), true
}

func generatedValue(key string) string {
	f := fakerPool.Get().(faker.Faker)
	defer fakerPool.Put(f)

	switch key {
	case "integer":
		return fmt.Sprint(f.UInt16())
	case "float":
		return fmt.Sprint(f.Float32(2, 0, 100_000))
	case "bool":
		return fmt.Sprint(f.Boolean().Bool())
	case "uuid":
		return f.UUID().V4()
	case "guid":
		return f.UUID().V4()
	case "timestamp":
		return fmt.Sprint(f.Time().Unix(time.Now()))
	case "isoTimestamp":
		return f.Time().ISO8601(time.Now())
	case "name":
		return f.Person().Name()
	case "firstName":
		return f.Person().FirstName()
	case "lastName":
		return f.Person().LastName()
	case "phone":
		return f.Phone().Number()
	case "user":
		return f.Internet().User()
	case "email":
		return f.Internet().Email()
	case "url":
		return f.Internet().URL()
	case "server":
		return f.Internet().Domain()
	case "hash":
		return f.Hash().MD5()
	case "file":
		return f.File().AbsoluteFilePath(3 + rand.IntN(4))
	case "sentence":
		return f.Lorem().Sentence(8 + rand.IntN(9))
	case "paragraph":
		return f.Lorem().Paragraph(3 + rand.IntN(2))
	case "article":
		return f.Lorem().Paragraph(5 + rand.IntN(3))
	default:
		return ""
	}
}

func isGeneratedKey(key string) bool {
	switch key {
	case "integer", "float", "bool", "uuid", "guid", "timestamp", "isoTimestamp", "name", "firstName", "lastName", "phone", "user", "email", "url", "server", "hash", "file", "sentence", "paragraph", "article":
		return true
	default:
		return false
	}
}

// readResponseFile uses a rooted filesystem to prevent symlinks from escaping
// the root the file is found in (the .http file's directory or the workspace
// root), including when a dependency is replaced during reload.
func readResponseFile(method restclient.Method) ([]byte, error) {
	var firstErr error
	for _, dir := range restclient.BodyFileRoots(method.Source) {
		body, err := readRooted(dir, method.File)
		if err == nil {
			return body, nil
		}
		if firstErr == nil || os.IsNotExist(firstErr) && !os.IsNotExist(err) {
			firstErr = err
		}
	}
	return nil, firstErr
}

func readRooted(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.Clean(name))
}

// JSON string interpolation must escape request values without changing raw
// numeric/boolean placeholders outside strings. Other body formats stay literal.
func expandBodyPlaceholders(input string, method restclient.Method, values map[string]string) string {
	contentType, _, _ := strings.Cut(method.Headers.Get("Content-Type"), ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if contentType == "" {
		if path, ok := resolveFilePath(&method); ok {
			contentType, _, _ = strings.Cut(mime.TypeByExtension(filepath.Ext(path)), ";")
		}
	}
	if contentType != "application/json" && !strings.HasSuffix(contentType, "+json") {
		return expandPlaceholders(input, method, values)
	}
	var out strings.Builder
	inString, escaped := false, false
	previous := 0
	for _, index := range placeholderPattern.FindAllStringIndex(input, -1) {
		prefix := input[previous:index[0]]
		for _, c := range prefix {
			if escaped {
				escaped = false
				continue
			}
			if inString && c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = !inString
			}
		}
		out.WriteString(prefix)
		value := expandPlaceholders(input[index[0]:index[1]], method, values)
		if inString {
			encoded, _ := json.Marshal(value)
			out.Write(encoded[1 : len(encoded)-1])
		} else {
			out.WriteString(value)
		}
		previous = index[1]
	}
	out.WriteString(input[previous:])
	return out.String()
}

func fileTemplatesEnabled(method restclient.Method, path string) bool {
	contentType := method.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(path))
	}
	contentType, _, _ = strings.Cut(contentType, ";")
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(contentType, "text/") || contentType == "application/json" || strings.HasSuffix(contentType, "+json") || contentType == "application/xml" || strings.HasSuffix(contentType, "+xml") || contentType == "application/javascript"
}
