package restclient

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Method is one mock request section from a REST Client-style .http file.
//
// Headers on the Method are response headers. Use MatchHeaders (from
// @header.Name = value variables) to require request headers when matching.
//
// Target is the request target as written, with any {{name}} references
// unexpanded. It may be a full URL such as "{{host}}/users", so the same file
// can drive a REST client; only the path and query are kept in Path and Query.
//
// File is the external response body path, as written after "< " at the start
// of the body; Body is empty then. Use ResolveFile to locate it on disk.
//
// Variables holds the raw values of every @name = value variable in scope for
// the method: file-scoped definitions overridden by method-scoped ones.
// VariableLines maps each of those names to the file line of its effective
// definition. Use Variable to read a value with {{name}} references expanded.
type Method struct {
	Name          string
	Method        string
	Target        string
	Path          string
	EscapedPath   string
	Query         url.Values
	Comments      []string
	Variables     map[string]string
	VariableLines map[string]int
	MatchHeaders  http.Header
	Headers       http.Header
	Body          string
	File          string
	Source        string
	Line          int
	Status        int
	Delay         time.Duration
}

// legacyVariablePattern matches the removed "# $name=value" comment variables.
var legacyVariablePattern = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(.*)$`)

var variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// definition is one "@name = value" line. The value is trimmed and unescaped
// but still holds any {{name}} references.
type definition struct {
	name  string
	value string
	line  int
}

// directives is the leading run of blank, "#" comment, and "@name = value"
// lines of a section (or of the file, before its first ### line).
type directives struct {
	defs     []definition
	comments []string
	next     int
}

// pendingSection is a ### section waiting for the file-scoped variables, which
// may be defined after it, to be known.
type pendingSection struct {
	method        Method
	lines         []string
	directives    directives
	bodyStartLine int
	nameLine      int
}

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

	var sections []pendingSection
	var fileDefs []definition
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
		lines := append([]string(nil), section...)
		lineAt := func(index int) int { return bodyStartLine + index }
		found, err := scanDirectives(lines, lineAt, source, current.Name)
		if err != nil {
			return err
		}
		if found.next == len(lines) && len(found.defs) > 0 {
			// A section holding only variable definitions has no request; its
			// variables are file-scoped.
			fileDefs = append(fileDefs, found.defs...)
			return nil
		}
		sections = append(sections, pendingSection{
			method:        *current,
			lines:         lines,
			directives:    found,
			bodyStartLine: bodyStartLine,
			nameLine:      sectionNameLine,
		})
		return nil
	}

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "###"); ok {
			if err := flush(); err != nil {
				return nil, err
			}
			name, status, err := parseSectionName(strings.TrimSpace(after))
			if err != nil {
				return nil, parseErrorf(source, lineNumber, "%v", err)
			}
			current = &Method{
				Name:          name,
				Status:        status,
				Variables:     make(map[string]string),
				VariableLines: make(map[string]int),
				MatchHeaders:  make(http.Header),
				Headers:       make(http.Header),
				Source:        source,
				Line:          lineNumber,
			}
			section = section[:0]
			sectionNameLine = lineNumber
			bodyStartLine = lineNumber + 1
			continue
		}
		if current == nil {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "@") {
				def, err := parseDefinition(trimmed, source, lineNumber)
				if err != nil {
					return nil, err
				}
				fileDefs = append(fileDefs, def)
				continue
			}
			return nil, parseErrorf(source, lineNumber,
				"content before first ### section: %s (start each mock with ### Name, or define a file variable with @name = value)", quoteSnippet(trimmed))
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

	var methods []Method
	for _, pending := range sections {
		method, err := parseSection(pending, fileDefs, source)
		if err != nil {
			return nil, err
		}
		methods = append(methods, method)
	}
	return methods, nil
}

// sectionStatusPattern matches a leading "[code]" in a ### section name.
var sectionStatusPattern = regexp.MustCompile(`^\[\s*(\d+)\s*\]\s*`)

// parseSectionName splits an optional leading "[status]" from a ### section
// name, as in "### [204] Delete a user". Status is 0 when none is given.
// Brackets that do not hold only digits, like "[beta]", stay part of the name.
func parseSectionName(text string) (name string, status int, err error) {
	if text == "" {
		return "", 0, fmt.Errorf(`method name is required after ### (example: "### List users")`)
	}
	match := sectionStatusPattern.FindStringSubmatch(text)
	if match == nil {
		return text, 0, nil
	}
	status, convErr := strconv.Atoi(match[1])
	if convErr != nil || status < 200 || status > 999 {
		return "", 0, fmt.Errorf(`invalid response status [%s] in section name (expected a final HTTP status between 200 and 999, example: "### [204] Delete a user")`, match[1])
	}
	name = text[len(match[0]):]
	if name == "" {
		return "", 0, fmt.Errorf(`method name is required after the status (example: "### [204] Delete a user")`)
	}
	return name, status, nil
}

// scanDirectives reads the leading blank, "#" comment, and "@name = value"
// lines of lines. next is the index of the first line that is none of those.
func scanDirectives(lines []string, lineAt func(int) int, source, sectionName string) (directives, error) {
	var found directives
	for found.next < len(lines) {
		i := found.next
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "":
		case strings.HasPrefix(line, "@"):
			def, err := parseDefinition(line, source, lineAt(i))
			if err != nil {
				return found, err
			}
			found.defs = append(found.defs, def)
		case strings.HasPrefix(line, "#"):
			comment := strings.TrimSpace(line[1:])
			found.comments = append(found.comments, comment)
			if matches := legacyVariablePattern.FindStringSubmatch(comment); matches != nil {
				if matches[1] == "status" {
					return found, parseErrorf(source, lineAt(i),
						`section %q: $status is no longer supported; put the status in the section name instead (example: "### [204] Delete a user")`, sectionName)
				}
				if matches[1] == "file" {
					return found, parseErrorf(source, lineAt(i), "section %q: %s", sectionName, fileVariableRemoved)
				}
				return found, parseErrorf(source, lineAt(i),
					`section %q: "# $%s=…" variables are no longer supported; use "@%s = %s" instead`,
					sectionName, matches[1], matches[1], strings.TrimSpace(matches[2]))
			}
		default:
			return found, nil
		}
		found.next++
	}
	return found, nil
}

// parseDefinition parses "@name = value". The name cannot contain spaces. The
// value may hold any characters; surrounding whitespace is trimmed and the
// escapes \n, \r, \t and \\ are replaced.
func parseDefinition(line, source string, lineNumber int) (definition, error) {
	name, value, ok := strings.Cut(line[1:], "=")
	if !ok {
		return definition{}, parseErrorf(source, lineNumber,
			"invalid variable definition %s (expected \"@name = value\")", quoteSnippet(line))
	}
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return definition{}, parseErrorf(source, lineNumber,
			"invalid variable definition %s (variable name is required after @)", quoteSnippet(line))
	case strings.ContainsFunc(name, unicode.IsSpace):
		return definition{}, parseErrorf(source, lineNumber,
			"invalid variable name %q (names cannot contain spaces)", name)
	case !variableNamePattern.MatchString(name):
		return definition{}, parseErrorf(source, lineNumber,
			"invalid variable name %q (use letters, digits, '_', '.', '-', starting with a letter or '_')", name)
	}
	return definition{name: name, value: unescapeValue(strings.TrimSpace(value)), line: lineNumber}, nil
}

func unescapeValue(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			if replacement, ok := valueEscapes[value[i+1]]; ok {
				out.WriteByte(replacement)
				i++
				continue
			}
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

var valueEscapes = map[byte]byte{'n': '\n', 'r': '\r', 't': '\t', '\\': '\\'}

// scopeDefinitions merges file-scoped and method-scoped definitions. A later
// definition replaces an earlier one with the same name (header matcher names
// compare case-insensitively), so a method overrides its file.
func scopeDefinitions(fileDefs, methodDefs []definition) []definition {
	var merged []definition
	index := make(map[string]int)
	for _, def := range slices.Concat(fileDefs, methodDefs) {
		key := def.name
		if headerName, ok := strings.CutPrefix(key, "header."); ok {
			key = "header." + http.CanonicalHeaderKey(headerName)
		}
		if i, ok := index[key]; ok {
			merged[i] = def
			continue
		}
		index[key] = len(merged)
		merged = append(merged, def)
	}
	return merged
}

func parseSection(pending pendingSection, fileDefs []definition, source string) (Method, error) {
	method := pending.method
	lines := pending.lines
	sectionNameLine := pending.nameLine
	if method.Status == 0 {
		method.Status = http.StatusOK
	}
	lineAt := func(index int) int {
		if index < 0 {
			return sectionNameLine
		}
		if pending.bodyStartLine <= 0 {
			return sectionNameLine
		}
		return pending.bodyStartLine + index
	}

	method.Comments = pending.directives.comments
	i := pending.directives.next

	defs := scopeDefinitions(fileDefs, pending.directives.defs)
	for _, def := range defs {
		if _, isHeader := strings.CutPrefix(def.name, "header."); isHeader {
			continue
		}
		method.Variables[def.name] = def.value
		method.VariableLines[def.name] = def.line
	}
	for _, def := range defs {
		if err := applyControlVariable(&method, def, source); err != nil {
			return method, err
		}
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
			`section %q has an unrecognized HTTP method %q (supported: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS, CONNECT, TRACE)`,
			method.Name, requestLine[0])
	}

	method.Target = requestLine[1]
	rawTarget := Expand(method.Target, method.Variables, nil)
	target, err := url.ParseRequestURI(rawTarget)
	if err != nil {
		return method, parseErrorf(source, lineAt(i),
			`section %q has an invalid request target %s: %v (use a path like "/users/:id" or a full URL)`,
			method.Name, quoteSnippet(rawTarget), err)
	}
	// A full URL may omit the path ("https://host"), which means "/".
	fullURL := target.Scheme != "" && target.Host != ""
	if target.Path == "" && !fullURL && !strings.HasPrefix(rawTarget, "/") {
		// ParseRequestURI can accept some odd forms; require a usable path for matching.
		return method, parseErrorf(source, lineAt(i),
			`section %q has an invalid request target %s (path is required, example: "/users")`,
			method.Name, quoteSnippet(rawTarget))
	}
	method.Path = target.Path
	method.EscapedPath = target.EscapedPath()
	if method.Path == "" {
		method.Path = "/"
	}
	method.Query, err = url.ParseQuery(target.RawQuery)
	if err != nil {
		return method, parseErrorf(source, lineAt(i), "section %q: invalid query: %v", method.Name, err)
	}
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
		if !ValidHeaderName(headerName) || !ValidHeaderValue(value) {
			return method, parseErrorf(source, lineAt(i), "invalid response header %q", headerName)
		}
		method.Headers.Add(headerName, strings.TrimSpace(value))
		i++
	}

	bodyLines := trimTrailingBlankLines(lines[i:])
	if len(bodyLines) > 0 && strings.HasPrefix(bodyLines[0], "< ") {
		// The path is everything after "< ", whitespace included.
		path := bodyLines[0][2:]
		switch {
		case strings.TrimSpace(path) == "":
			return method, parseErrorf(source, lineAt(i),
				`section %q: "< " must be followed by a file path (example: "< ./users.json")`, method.Name)
		case ValidateBodyFile(path) != nil:
			return method, parseErrorf(source, lineAt(i), "section %q: %v", method.Name, ValidateBodyFile(path))
		case len(bodyLines) > 1:
			return method, parseErrorf(source, lineAt(i+1),
				`section %q: a body starting with "< " must be only the file path line; found more text after %s`,
				method.Name, quoteSnippet(bodyLines[0]))
		}
		method.File = path
		return method, nil
	}
	method.Body = strings.Join(bodyLines, "\n")
	return method, nil
}

const fileVariableRemoved = `$file and @file are no longer supported; start the response body with "< path" instead (example: "< ./users.json")`

// applyControlVariable interprets the variables the mock server itself
// consumes: @delay and @header.Name. Other variables are only
// referenced as {{name}} placeholders.
func applyControlVariable(method *Method, def definition, source string) error {
	value := Expand(def.value, method.Variables, nil)
	if headerName, ok := strings.CutPrefix(def.name, "header."); ok {
		if headerName == "" {
			return parseErrorf(source, def.line,
				`section %q: @header. requires a header name (example: "@header.Authorization = Bearer token")`, method.Name)
		}
		if !ValidHeaderName(headerName) || !ValidHeaderValue(value) {
			return parseErrorf(source, def.line, "invalid request header matcher %q", headerName)
		}
		method.MatchHeaders.Add(headerName, value)
		return nil
	}
	switch def.name {
	case "status":
		return parseErrorf(source, def.line,
			`section %q: @status is not supported; put the status in the section name instead (example: "### [204] Delete a user")`, method.Name)
	case "file":
		return parseErrorf(source, def.line, "section %q: %s", method.Name, fileVariableRemoved)
	case "delay":
		delay, err := time.ParseDuration(value)
		if err != nil || delay < 0 {
			return parseErrorf(source, def.line, "@delay must be a non-negative duration")
		}
		method.Delay = delay
	}
	return nil
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
		return false
	}
}

func trimTrailingBlankLines(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

// controlVariables are consumed by the mock server itself (not only as {{name}} references).
var controlVariables = map[string]struct{}{
	"delay": {},
}

// PlaceholderPattern matches {{name}}, {{%name}} and {{$name args}} placeholders
// in bodies and headers. Submatch 1 is the optional "%" (percent-encode) or "$"
// (request parameter or generated value) prefix; submatch 2 is the name;
// submatch 3 is the space-separated arguments, which only {{$name}} accepts.
var PlaceholderPattern = regexp.MustCompile(`\{\{([%$]?)([A-Za-z_][A-Za-z0-9_.-]*)(?:[ \t]+([^{}]*?))?[ \t]*}}`)

// Expand replaces placeholders in input. {{name}} and {{%name}} (percent-encoded)
// resolve from vars, whose values may themselves hold placeholders; a variable
// that refers back to itself is left unexpanded. {{$name}} resolves through
// dollar, when it is not nil, with the placeholder's arguments (such as "1 10"
// in {{$randomInt 1 10}}). Unresolved placeholders are kept literally.
func Expand(input string, vars map[string]string, dollar func(name string, args []string) (string, bool)) string {
	return expand(input, vars, dollar, make(map[string]bool))
}

func expand(input string, vars map[string]string, dollar func(string, []string) (string, bool), active map[string]bool) string {
	return PlaceholderPattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := PlaceholderPattern.FindStringSubmatch(match)
		prefix, name, args := parts[1], parts[2], strings.Fields(parts[3])
		if prefix == "$" {
			if dollar != nil {
				if value, ok := dollar(name, args); ok {
					return value
				}
			}
			return match
		}
		raw, ok := vars[name]
		if !ok || active[name] || len(args) > 0 {
			return match
		}
		active[name] = true
		value := expand(raw, vars, dollar, active)
		delete(active, name)
		if prefix == "%" {
			value = strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
		}
		return value
	})
}

// Variable returns the value of an @name variable in scope for the method,
// with its {{name}} references expanded.
func (m Method) Variable(name string) (string, bool) {
	raw, ok := m.Variables[name]
	if !ok {
		return "", false
	}
	return Expand(raw, m.Variables, nil), true
}

// FileDependencies returns the external body paths, as written, referenced by methods.
func FileDependencies(methods []Method) []string {
	seen := make(map[string]struct{})
	var deps []string
	for _, method := range methods {
		raw := method.File
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

// UnusedVariable is a variable definition that no method references.
type UnusedVariable struct {
	Name   string
	Source string
	Line   int
}

// UnusedVariables returns the @name definitions that are not control variables
// (@delay) and are never referenced as {{name}} in the request target, body, or
// response headers of a method they apply to, or in another variable's value.
// A file variable is used if any method it applies to references it. Callers
// should warn; these are not errors.
func UnusedVariables(methods []Method) []UnusedVariable {
	type site struct {
		source string
		line   int
	}
	names := make(map[site]string)
	used := make(map[site]bool)
	for _, method := range methods {
		referenced := placeholderNames(method)
		for _, value := range method.Variables {
			collectPlaceholders(value, referenced)
		}
		for name, line := range method.VariableLines {
			if _, ok := controlVariables[name]; ok {
				continue
			}
			key := site{method.Source, line}
			names[key] = name
			if referenced[name] {
				used[key] = true
			}
		}
	}
	var unused []UnusedVariable
	for key, name := range names {
		if !used[key] {
			unused = append(unused, UnusedVariable{Name: name, Source: key.source, Line: key.line})
		}
	}
	slices.SortFunc(unused, func(a, b UnusedVariable) int {
		return cmp.Or(strings.Compare(a.Source, b.Source), cmp.Compare(a.Line, b.Line))
	})
	return unused
}

// collectPlaceholders records the names of {{name}} and {{%name}} references in text.
func collectPlaceholders(text string, into map[string]bool) {
	for _, match := range PlaceholderPattern.FindAllStringSubmatch(text, -1) {
		if match[1] != "$" {
			into[match[2]] = true
		}
	}
}

func placeholderNames(method Method) map[string]bool {
	used := make(map[string]bool)
	collectPlaceholders(method.Target, used)
	collectPlaceholders(method.Body, used)
	for _, values := range method.Headers {
		for _, value := range values {
			collectPlaceholders(value, used)
		}
	}
	return used
}

// ValidateBodyFile checks an external body path. It must be relative and
// cannot contain .. segments, so it stays inside the directory it is resolved
// against.
func ValidateBodyFile(raw string) error {
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		return fmt.Errorf("external body path must be relative, not absolute")
	}
	for _, part := range strings.Split(filepath.ToSlash(raw), "/") {
		if part == ".." {
			return fmt.Errorf("external body path cannot contain .. path segments")
		}
	}
	if filepath.Clean(raw) == "." {
		return fmt.Errorf("external body path must name a file")
	}
	return nil
}

// BodyFileRoots returns the directories a relative external body path is
// looked up in, in order: the directory of the .http file that names it, then
// the workspace root (the directory mock runs from). A file must stay inside
// the root it is found in.
func BodyFileRoots(source string) []string {
	roots := []string{filepath.Dir(source)}
	if cwd, err := os.Getwd(); err == nil {
		if abs, err := filepath.Abs(roots[0]); err != nil || abs != cwd {
			roots = append(roots, cwd)
		}
	}
	return roots
}

// BodyFilePaths returns the candidate locations of a relative external body
// path, one per BodyFileRoots entry.
func BodyFilePaths(source, raw string) []string {
	var paths []string
	for _, root := range BodyFileRoots(source) {
		paths = append(paths, filepath.Join(root, raw))
	}
	return paths
}

// ResolveFile returns the first BodyFilePaths candidate that exists, or the
// first candidate when none does.
func ResolveFile(source, raw string) string {
	paths := BodyFilePaths(source, raw)
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return paths[0]
}

func ValidHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return false
		}
	}
	return true
}

func ValidHeaderValue(value string) bool {
	for _, c := range value {
		if c == 127 || c < 32 && c != '\t' {
			return false
		}
	}
	return true
}
