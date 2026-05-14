package mockhttp

import (
	"fmt"
	"math/rand/v2"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"mock/restclient"

	"github.com/jaswdr/faker"
)

var placeholderPattern = regexp.MustCompile(`\{\{\$([A-Za-z_][A-Za-z0-9_]*)}}`)

var (
	f       = faker.New()
	fakerMu sync.Mutex
)

func statusFromVariables(variables map[string]string) int {
	raw, ok := variables["status"]
	if !ok {
		return http.StatusOK
	}
	status, err := strconv.Atoi(raw)
	if err != nil || status < 100 || status > 999 {
		return http.StatusOK
	}
	return status
}

func statusAllowsBody(status int) bool {
	return status != http.StatusNoContent && status != http.StatusNotModified && (status < 100 || status >= 200)
}

func responseHeaders(method restclient.Method, filePath string) http.Header {
	headers := method.Headers.Clone()
	if headers.Get("Content-Type") != "" || method.Body != "" || filePath == "" {
		return headers
	}
	if contentType := mime.TypeByExtension(filepath.Ext(filePath)); contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	return headers
}

func renderBody(method restclient.Method, values map[string]string, filePath string, hasFile bool) (string, error) {
	if method.Body == "" {
		if hasFile {
			body, err := os.ReadFile(filePath)
			if err != nil {
				return "", fmt.Errorf("%s: %w", filePath, err)
			}
			return string(body), nil
		}
		return "", nil
	}

	return placeholderPattern.ReplaceAllStringFunc(method.Body, func(match string) string {
		parts := placeholderPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		key := parts[1]
		if value, ok := values[key]; ok {
			return value
		}
		if value, ok := method.Variables[key]; ok {
			return value
		}
		return generatedValue(key)
	}), nil
}

func resolveFilePath(method *restclient.Method) (string, bool) {
	raw, ok := method.Variables["file"]
	if !ok {
		return "", false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) {
		return "", false
	}
	cleaned := filepath.Clean(raw)
	sep := string(filepath.Separator)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+sep) {
		return "", false
	}
	return filepath.Join(filepath.Dir(method.Source), cleaned), true
}

func generatedValue(key string) string {
	fakerMu.Lock()
	defer fakerMu.Unlock()

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
