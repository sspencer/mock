package mockhttp

import (
	"fmt"
	"log/slog"
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

	"github.com/sspencer/mock/restclient"

	"github.com/jaswdr/faker"
)

var placeholderPattern = regexp.MustCompile(`\{\{\$([A-Za-z_][A-Za-z0-9_]*)}}`)

var fakerPool = sync.Pool{
	New: func() any {
		return faker.New()
	},
}

func statusFromVariables(logger *slog.Logger, variables map[string]string) int {
	raw, ok := variables["status"]
	if !ok {
		return http.StatusOK
	}
	status, err := parseStatusCode(raw)
	if err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("ignoring invalid $status", "status", raw, "error", err)
		return http.StatusOK
	}
	return status
}

func parseStatusCode(raw string) (int, error) {
	status, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if status < 100 || status > 999 {
		return 0, fmt.Errorf("status %d out of range", status)
	}
	return status, nil
}

func statusAllowsBody(status int) bool {
	return status != http.StatusNoContent && status != http.StatusNotModified && (status < 100 || status >= 200)
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
			body, err := os.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", filePath, err)
			}
			// Expand placeholders only when the file looks like text.
			if isMostlyText(body) {
				return []byte(expandPlaceholders(string(body), method, values)), nil
			}
			return body, nil
		}
		return nil, nil
	}

	return []byte(expandPlaceholders(method.Body, method, values)), nil
}

func expandPlaceholders(input string, method restclient.Method, values map[string]string) string {
	return placeholderPattern.ReplaceAllStringFunc(input, func(match string) string {
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
	})
}

func isMostlyText(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	sample := body
	if len(sample) > 512 {
		sample = sample[:512]
	}
	var nonPrintable int
	for _, b := range sample {
		if b == 0 {
			return false
		}
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			nonPrintable++
		}
	}
	return nonPrintable*10 <= len(sample)
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
