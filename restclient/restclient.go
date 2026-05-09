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
)

type Method struct {
	Name      string
	Method    string
	Path      string
	Query     url.Values
	Comments  []string
	Variables map[string]string
	Headers   http.Header
	Body      string
	Source    string
}

var commentVariablePattern = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)`)

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
	lineNumber := 0

	flush := func(endLine int) error {
		if current == nil {
			return nil
		}
		method, err := parseSection(*current, section)
		if err != nil {
			return fmt.Errorf("%s:%d: %w", source, endLine-len(section), err)
		}
		methods = append(methods, method)
		return nil
	}

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "###"); ok {
			if err := flush(lineNumber); err != nil {
				return nil, err
			}
			name := strings.TrimSpace(after)
			if name == "" {
				return nil, fmt.Errorf("%s:%d: method name is required after ###", source, lineNumber)
			}
			current = &Method{
				Name:      name,
				Variables: make(map[string]string),
				Headers:   make(http.Header),
				Source:    source,
			}
			section = section[:0]
			continue
		}
		if current == nil {
			if strings.TrimSpace(line) != "" {
				return nil, fmt.Errorf("%s:%d: content before first ###", source, lineNumber)
			}
			continue
		}
		section = append(section, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if err := flush(lineNumber + 1); err != nil {
		return nil, err
	}
	return methods, nil
}

func parseSection(method Method, lines []string) (Method, error) {
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
			method.Variables[matches[1]] = strings.TrimSpace(matches[2])
		}
		i++
	}

	if i >= len(lines) {
		return method, fmt.Errorf("%q is missing an HTTP request line", method.Name)
	}

	requestLine := strings.Fields(strings.TrimSpace(lines[i]))
	if len(requestLine) < 2 {
		return method, fmt.Errorf("%q has an invalid HTTP request line", method.Name)
	}
	method.Method = strings.ToUpper(requestLine[0])
	target, err := url.ParseRequestURI(requestLine[1])
	if err != nil {
		return method, fmt.Errorf("%q has an invalid request target: %w", method.Name, err)
	}
	method.Path = target.Path
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
			return method, fmt.Errorf("%q has an invalid header line %q", method.Name, line)
		}
		method.Headers.Add(strings.TrimSpace(name), strings.TrimSpace(value))
		i++
	}

	bodyLines := trimTrailingBlankLines(lines[i:])
	method.Body = strings.Join(bodyLines, "\n")
	return method, nil
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}
