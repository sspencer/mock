package restclient

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"
)

// Method is one mock request section from a REST Client-style .http file.
//
// Headers on the Method are response headers. Use MatchHeaders (from
// # $header.Name=value comments) to require request headers when matching.
type Method struct {
	Name         string
	Method       string
	Path         string
	Query        url.Values
	Comments     []string
	Variables    map[string]string
	MatchHeaders http.Header
	Headers      http.Header
	Body         string
	Source       string
}

var commentVariablePattern = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(.*)$`)

func Load(paths []string) ([]Method, error) {
	var methods []Method
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		parsed, err := Parse(path, file)
		closeErr := file.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, fmt.Errorf("%s: %w", path, closeErr)
		}
		methods = append(methods, parsed...)
	}
	return methods, nil
}

func Parse(source string, r io.Reader) ([]Method, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var methods []Method
	var current *Method
	var section []string
	// bodyStartLine is the 1-based file line number of section[0], when section is non-empty.
	// When the section is empty, it is the line after the ### line.
	var bodyStartLine int
	var sectionNameLine int
	lineNumber := 0

	flush := func() error {
		if current == nil {
			return nil
		}
		method, err := parseSection(*current, section, bodyStartLine, sectionNameLine, source)
		if err != nil {
			return err
		}
		methods = append(methods, method)
		return nil
	}

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "###"); ok {
			if err := flush(); err != nil {
				return nil, err
			}
			name := strings.TrimSpace(after)
			if name == "" {
				return nil, parseErrorf(source, lineNumber,
					`method name is required after ### (example: "### List users")`)
			}
			current = &Method{
				Name:         name,
				Variables:    make(map[string]string),
				MatchHeaders: make(http.Header),
				Headers:      make(http.Header),
				Source:       source,
			}
			section = section[:0]
			sectionNameLine = lineNumber
			bodyStartLine = lineNumber + 1
			continue
		}
		if current == nil {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				return nil, parseErrorf(source, lineNumber,
					"content before first ### section: %s (start each mock with ### Name)", quoteSnippet(trimmed))
			}
			continue
		}
		if len(section) == 0 {
			bodyStartLine = lineNumber
		}
		section = append(section, line)
	}
	if err := scanner.Err(); err != nil {
		// Scanner errors (e.g. token too long) are not always line-precise.
		if lineNumber > 0 {
			return nil, parseErrorf(source, lineNumber, "failed to read file: %v", err)
		}
		return nil, fmt.Errorf("%s: failed to read file: %w", source, err)
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return methods, nil
}

func parseSection(method Method, lines []string, bodyStartLine, sectionNameLine int, source string) (Method, error) {
	lineAt := func(index int) int {
		if index < 0 {
			return sectionNameLine
		}
		if bodyStartLine <= 0 {
			return sectionNameLine
		}
		return bodyStartLine + index
	}

	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			continue
		}
		after, ok := strings.CutPrefix(line, "#")
		if !ok {
			break
		}

		comment := strings.TrimSpace(after)
		method.Comments = append(method.Comments, comment)
		if matches := commentVariablePattern.FindStringSubmatch(comment); len(matches) == 3 {
			key := matches[1]
			value := strings.TrimSpace(matches[2])
			if headerName, ok := strings.CutPrefix(key, "header."); ok {
				if headerName == "" {
					return method, parseErrorf(source, lineAt(i),
						`section %q: $header. requires a header name (example: "# $header.Authorization=Bearer token")`, method.Name)
				}
				method.MatchHeaders.Add(headerName, value)
			} else {
				method.Variables[key] = value
			}
		}
		i++
	}

	if i >= len(lines) {
		// Point at the section header when there is no request line at all.
		hintLine := sectionNameLine
		if len(lines) > 0 {
			// Point at the last non-empty comment/blank area when the section has content but no request.
			hintLine = lineAt(len(lines) - 1)
		}
		return method, parseErrorf(source, hintLine,
			`section %q is missing an HTTP request line (expected "METHOD /path", example: "GET /users")`, method.Name)
	}

	rawRequest := strings.TrimSpace(lines[i])
	requestLine := strings.Fields(rawRequest)
	switch {
	case len(requestLine) == 0:
		return method, parseErrorf(source, lineAt(i),
			`section %q is missing an HTTP request line (expected "METHOD /path")`, method.Name)
	case len(requestLine) == 1:
		return method, parseErrorf(source, lineAt(i),
			`section %q has an incomplete HTTP request line %s (expected "METHOD /path", example: "GET /users")`,
			method.Name, quoteSnippet(rawRequest))
	case len(requestLine) > 2:
		return method, parseErrorf(source, lineAt(i),
			`section %q has an invalid HTTP request line %s (expected exactly "METHOD /path"; HTTP version tokens are not supported)`,
			method.Name, quoteSnippet(rawRequest))
	}

	method.Method = strings.ToUpper(requestLine[0])
	if !isHTTPMethod(method.Method) {
		return method, parseErrorf(source, lineAt(i),
			`section %q has an unrecognized HTTP method %q (use GET, POST, PUT, PATCH, DELETE, HEAD, or OPTIONS)`,
			method.Name, requestLine[0])
	}

	target, err := url.ParseRequestURI(requestLine[1])
	if err != nil {
		return method, parseErrorf(source, lineAt(i),
			`section %q has an invalid request target %s: %v (use a path like "/users/:id" or a full URL)`,
			method.Name, quoteSnippet(requestLine[1]), err)
	}
	if target.Path == "" && !strings.HasPrefix(requestLine[1], "/") {
		// ParseRequestURI can accept some odd forms; require a usable path for matching.
		return method, parseErrorf(source, lineAt(i),
			`section %q has an invalid request target %s (path is required, example: "/users")`,
			method.Name, quoteSnippet(requestLine[1]))
	}
	method.Path = target.Path
	if method.Path == "" {
		method.Path = "/"
	}
	method.Query = target.Query()
	i++

	for i < len(lines) {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			i++
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return method, parseErrorf(source, lineAt(i),
				`section %q has an invalid response header line %s (expected "Name: value", example: "Content-Type: application/json")`,
				method.Name, quoteSnippet(line))
		}
		headerName := strings.TrimSpace(name)
		if headerName == "" {
			return method, parseErrorf(source, lineAt(i),
				`section %q has an invalid response header line %s (header name is required before ":")`,
				method.Name, quoteSnippet(line))
		}
		method.Headers.Add(headerName, strings.TrimSpace(value))
		i++
	}

	bodyLines := trimTrailingBlankLines(lines[i:])
	method.Body = strings.Join(bodyLines, "\n")
	return method, nil
}

func parseErrorf(source string, line int, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if line > 0 {
		return fmt.Errorf("%s:%d: %s", source, line, msg)
	}
	return fmt.Errorf("%s: %s", source, msg)
}

func quoteSnippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return `""`
	}
	// Keep messages short when users paste huge accidental lines.
	const max = 80
	runes := []rune(s)
	if len(runes) > max {
		s = string(runes[:max]) + "…"
	}
	return fmt.Sprintf("%q", s)
}

func isHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace:
		return true
	default:
		// Allow uncommon but syntactically valid tokens (e.g. custom methods) that look like HTTP methods.
		if method == "" {
			return false
		}
		for _, r := range method {
			if !unicode.IsUpper(r) && r != '_' && r != '-' {
				return false
			}
		}
		return true
	}
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

// FileDependencies returns relative $file paths referenced by methods, for watching.
func FileDependencies(methods []Method) []string {
	seen := make(map[string]struct{})
	var deps []string
	for _, method := range methods {
		raw, ok := method.Variables["file"]
		if !ok {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		deps = append(deps, raw)
	}
	return deps
}
