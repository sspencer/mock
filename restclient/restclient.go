package restclient

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
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
// MatchQuery (@query.name), MatchPath (@path.name), MatchBody (@body) and
// MatchJSON (@json.field.path), MatchXML (@xml.element.path), MatchForm
// (@form.name) and MatchCookies (@cookie.name) add further request matchers. Matcher values
// are kept as written; see ParseMatcher and ParseBodyMatcher.
//
// Target is the request target as written, with any {{name}} references
// unexpanded. It may be a full URL such as "{{host}}/users", so the same file
// can drive a REST client; the path and query are kept in Path and Query.
//
// Port is the listener port the method is served on: the port of a full URL
// target, else the @port variable. It is 0 when neither is given, meaning the
// server's default port. The scheme and host name of a URL are ignored.
//
// File is the external response body path, as written after "< " at the start
// of the body; Body is empty then. Use ResolveFile to locate it on disk. It may
// hold placeholders, such as "./users/{{$id}}.json", which are expanded per
// request (see FileIsTemplate).
//
// Delay is the @delay before responding; with DelayMax it is the low end of
// a random range ("@delay = 100ms..800ms"). Stream is the @stream pause
// between body lines or SSE events. Fault is the @fault ("reset" or
// "truncate") injected on FaultRate of responses (1 means every response).
// Template is the @template body mode: "" (auto), "on" or "off".
//
// Variables holds the raw values of every @name = value variable in scope for
// the method: file-scoped definitions overridden by method-scoped ones.
// VariableLines maps each of those names to the file line of its effective
// definition. Use Variable to read a value with {{name}} references expanded.
type Method struct {
	Name string
	// Method is the request method the route answers, as written on the
	// request line and upper-cased: one method such as "GET", a
	// comma-separated list such as "GET,HEAD,OPTIONS", or "*" for any
	// method. Use Accepts to test a request method against it.
	Method        string
	Target        string
	Path          string
	EscapedPath   string
	Query         url.Values
	Comments      []string
	Variables     map[string]string
	VariableLines map[string]int
	MatchHeaders  http.Header
	MatchQuery    url.Values
	MatchPath     map[string]string
	MatchBody     string
	MatchJSON     map[string]string
	MatchXML      map[string]string
	MatchForm     url.Values
	MatchCookies  url.Values
	Headers       http.Header
	Body          string
	File          string
	Source        string
	Line          int
	Status        int
	Delay         time.Duration
	DelayMax      time.Duration
	Stream        time.Duration
	Fault         string
	FaultRate     float64
	Template      string
	Port          int
	// Proxy is the @proxy URL that matching requests are forwarded to. It may
	// hold request placeholders in its path and query.
	Proxy string
	// Fixture is the @fixture JSON file that {{$fixture.path}} placeholders
	// read, as written. Locate it with ResolveFile.
	Fixture string
	// Compress is the @compress mode: "" (off), "gzip", "deflate" or
	// "auto" (chosen from the request's Accept-Encoding).
	Compress string
	// Callbacks are the @callback and @callback.N requests sent after the
	// response, in Index order, each after the previous one finished.
	Callbacks []*Callback
	// CallbackRetries is the section's @callback.retries, the default for
	// callbacks that set no retries of their own.
	CallbackRetries int
	// RateLimit is the @rate-limit budget: at most RateLimit requests per
	// RateWindow, after which the route answers 429. 0 means no limit.
	RateLimit  int
	RateWindow time.Duration
	// RateLimitKey, from @rate-limit.key, is a template rendered per request
	// that splits the budget: each distinct value gets its own.
	RateLimitKey string
	// DelaySigma, when set, makes the delay lognormal with median Delay.
	DelaySigma float64
	// Throttle is the @throttle rate in bytes per second; 0 is unlimited.
	Throttle int64
	// DefaultContentType is the Content-Type from @default.header or
	// Options.Headers for a route that sets none. It may hold placeholders.
	// The server sends it only when neither a "< path" file's extension nor
	// sniffing the body gives a specific type.
	DefaultContentType string
	// StatusTemplate, when set, is rendered for each response to give its
	// status, from "### [{{...}}] Name" or @status = {{...}}. Status is then
	// only nominal (200).
	StatusTemplate string
	// Require is the reference (without "$") that @require needs to have a
	// value, such as "fixture.users[id=$path.id]"; without one, the route
	// does not match.
	Require string
	// Missing is the @missing status for a "< path" file that does not
	// exist; 0 keeps the default (404 for a templated path, else 500).
	// MissingNext (@missing = next) lets later routes answer instead.
	Missing     int
	MissingNext bool
	// ETag is set by @etag = auto: responses carry a hash ETag and answer
	// a matching If-None-Match with 304.
	ETag bool
	// Fallback is set by @fallback = true: the route is tried after every
	// route without it, whatever the load order.
	Fallback bool
	// Chance is the @match.chance share of otherwise matching requests the
	// route accepts, between 0 (exclusive) and 1. 0 means no @match.chance,
	// so the route accepts every matching request.
	Chance float64
}

// AnyMethod is the request line method that accepts every request method.
const AnyMethod = "*"

// Accepts reports whether the route answers requests with requestMethod.
func (m Method) Accepts(requestMethod string) bool {
	if m.Method == AnyMethod {
		return true
	}
	if !strings.Contains(m.Method, ",") {
		return m.Method == requestMethod
	}
	return slices.Contains(strings.Split(m.Method, ","), requestMethod)
}

// Methods lists the request methods the route answers, or nil when it
// answers any method ("*").
func (m Method) Methods() []string {
	if m.Method == AnyMethod {
		return nil
	}
	return strings.Split(m.Method, ",")
}

// SampleMethod is the method to send when trying the route: its first
// method, or GET for a route that accepts any method.
func (m Method) SampleMethod() string {
	if m.Method == AnyMethod {
		return http.MethodGet
	}
	first, _, _ := strings.Cut(m.Method, ",")
	return first
}

// parseRequestMethods parses the method token of a request line: one
// method, a comma-separated list such as "GET,HEAD", or "*". Methods are
// upper-cased and duplicates dropped.
func parseRequestMethods(token string) (string, error) {
	if token == AnyMethod {
		return AnyMethod, nil
	}
	var methods []string
	for _, part := range strings.Split(token, ",") {
		method := strings.ToUpper(part)
		if !isHTTPMethod(method) {
			if part == "" || part == AnyMethod {
				return "", fmt.Errorf("invalid method list %q (separate methods with commas and no spaces, example: \"GET,HEAD /health\", or use \"*\" alone for any method)", token)
			}
			return "", fmt.Errorf("unrecognized HTTP method %q (supported: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS, CONNECT, TRACE, a comma-separated list such as GET,HEAD, or * for any method)", part)
		}
		if !slices.Contains(methods, method) {
			methods = append(methods, method)
		}
	}
	return strings.Join(methods, ","), nil
}

// isMethodToken reports whether a request line token reads as a method, a
// method list or "*".
func isMethodToken(token string) bool {
	if token == AnyMethod {
		return true
	}
	for _, part := range strings.Split(strings.ToUpper(token), ",") {
		if !isHTTPMethod(part) {
			return false
		}
	}
	return true
}

// Callback is an outbound request a route sends after it responds, set by
// @callback = METHOD URL (or @callback.N = METHOD URL) and the .delay,
// .header.Name, .body and .retries directives under it. URL, header values
// and Body are templates rendered against the triggering request.
type Callback struct {
	// Index orders a route's callbacks: 0 for @callback, N for @callback.N.
	Index   int
	Method  string
	URL     string
	Delay   time.Duration
	Headers http.Header
	Body    string
	// Retries is how many times a failed send (no response, 429 or 5xx) is
	// retried with backoff.
	Retries int
	// retriesSet records an explicit @callback.N.retries, which wins over
	// the section's @callback.retries.
	retriesSet bool
	// line is the first directive that mentions the callback, for errors.
	line int
}

// maxCallbackRetries caps @callback.retries.
const maxCallbackRetries = 10

// legacyVariablePattern matches the removed "# $name=value" comment variables.
var legacyVariablePattern = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(.*)$`)

var variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// jsonMatcherName reports whether name is a @json or @xml matcher whose path
// uses "*" segments or ["key"] segments, which the usual name rule rejects.
// The path must start with a plain field so the name still starts like a
// variable name.
func jsonMatcherName(name string) bool {
	path, ok := strings.CutPrefix(name, "json.")
	if !ok {
		path, ok = strings.CutPrefix(name, "xml.")
	}
	if !ok || path == "" || path[0] == '[' {
		return false
	}
	_, err := ParseJSONPath(path)
	return err == nil
}

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
	endings       []string
	directives    directives
	bodyStartLine int
	nameLine      int
	// unnamed sections ("###" alone, or a request before the first "###")
	// are named after their request line.
	unnamed bool
}

// unnamedSection is the name of an unnamed section until its request line
// is read.
const unnamedSection = "(unnamed)"

func newMethod(name string, status int, source string, line int) *Method {
	return &Method{
		Name:          name,
		Status:        status,
		Variables:     make(map[string]string),
		VariableLines: make(map[string]int),
		MatchHeaders:  make(http.Header),
		MatchQuery:    make(url.Values),
		MatchPath:     make(map[string]string),
		MatchJSON:     make(map[string]string),
		MatchXML:      make(map[string]string),
		MatchForm:     make(url.Values),
		MatchCookies:  make(url.Values),
		Headers:       make(http.Header),
		Source:        source,
		Line:          line,
	}
}

// isComment reports whether a trimmed line is a "#" or "//" comment.
func isComment(line string) bool {
	return strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//")
}

// looksLikeRequestLine reports whether a trimmed line reads as a request
// line: "METHOD target", or a bare target, which REST Client sends as GET.
func looksLikeRequestLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	if len(fields) >= 2 && isMethodToken(fields[0]) {
		return true
	}
	return len(fields) <= 2 && isBareTarget(fields[0])
}

// isBareTarget reports whether a request line token is a target without a
// method: a path or an http(s) URL.
func isBareTarget(token string) bool {
	return strings.HasPrefix(token, "/") || strings.HasPrefix(token, "http://") ||
		strings.HasPrefix(token, "https://") || strings.HasPrefix(token, "{{")
}

// httpVersionPattern matches an HTTP version token at the end of a request
// line, which REST Client allows and mock ignores.
var httpVersionPattern = regexp.MustCompile(`^HTTP/\d(?:\.\d)?$`)

// Options adjust how request files are parsed. The zero value parses files
// as written.
type Options struct {
	// Vars override @name definitions, file- or section-scoped, in every file,
	// and supply values for {{name}} references that no @name defines. Names
	// must pass CheckVariableOverride.
	Vars map[string]string
	// Defaults supply values for {{name}} references that no @name defines,
	// such as a REST Client environment; @name definitions and Vars win.
	// Names that IsDefaultVariable rejects are ignored.
	Defaults map[string]string
	// Headers are response headers for every route that sets no header of
	// that name, either after its request line or with @default.header.Name
	// (which wins). @proxy routes get none.
	Headers http.Header
}

// IsDefaultVariable reports whether name can be supplied by Options.Defaults:
// a valid variable name that is neither a request matcher nor a control
// variable such as @port or @delay, which a default must not set by accident.
func IsDefaultVariable(name string) bool {
	_, control := controlVariables[name]
	return CheckVariableOverride(name) == nil && !control && !strings.HasPrefix(name, "callback.") && !strings.HasPrefix(name, defaultHeaderPrefix)
}

// CheckVariableOverride reports whether name can be set with Options.Vars:
// a variable name that is not a request matcher (@header.*, @query.*,
// @path.*, @body, @json.*, @xml.*, @form.*, @cookie.*).
func CheckVariableOverride(name string) error {
	if !variableNamePattern.MatchString(name) {
		return fmt.Errorf("invalid variable name %q (use letters, digits, _, . and -, starting with a letter or _)", name)
	}
	if isMatcherName(name) {
		return fmt.Errorf("@%s is a request matcher, not a variable, and cannot be set from the command line", name)
	}
	return nil
}

// Load parses the request files in order.
func Load(paths []string) ([]Method, error) {
	return Options{}.Load(paths)
}

// Parse parses one request file read from r; source names it in errors.
func Parse(source string, r io.Reader) ([]Method, error) {
	return Options{}.Parse(source, r)
}

// Load parses the request files in order with these options.
func (o Options) Load(paths []string) ([]Method, error) {
	var methods []Method
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		parsed, err := o.Parse(path, file)
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

// Parse parses one request file with these options.
func (o Options) Parse(source string, r io.Reader) ([]Method, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	// ending is the terminator of the line just scanned ("\n", "\r\n" or ""
	// at the end of the input), so "@trim = off" bodies can be kept exactly.
	var ending string
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		advance, token, err := bufio.ScanLines(data, atEOF)
		if token != nil {
			ending = string(data[len(token):advance])
		}
		return advance, token, err
	})

	var sections []pendingSection
	var fileDefs []definition
	var current *Method
	var currentUnnamed bool
	// leading holds the comment lines directly above a request before the
	// first ###, which become part of its unnamed section.
	var leading, leadingEndings []string
	var section []string
	var sectionEndings []string
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
		endings := append([]string(nil), sectionEndings...)
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
			endings:       endings,
			directives:    found,
			bodyStartLine: bodyStartLine,
			nameLine:      sectionNameLine,
			unnamed:       currentUnnamed,
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
			name, statusTemplate := strings.TrimSpace(after), ""
			if match := sectionStatusTemplatePattern.FindStringSubmatch(name); match != nil {
				name, statusTemplate = name[len(match[0]):], match[1]
			}
			name, status, err := parseSectionName(name)
			if err != nil {
				return nil, parseErrorf(source, lineNumber, "%v", err)
			}
			currentUnnamed = name == ""
			if currentUnnamed {
				name = unnamedSection
			}
			current = newMethod(name, status, source, lineNumber)
			current.StatusTemplate = statusTemplate
			section = section[:0]
			sectionEndings = sectionEndings[:0]
			sectionNameLine = lineNumber
			bodyStartLine = lineNumber + 1
			continue
		}
		if current == nil {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				leading, leadingEndings = leading[:0], leadingEndings[:0]
				continue
			}
			if strings.HasPrefix(trimmed, "@") {
				leading, leadingEndings = leading[:0], leadingEndings[:0]
				def, err := parseDefinition(trimmed, source, lineNumber)
				if err != nil {
					return nil, err
				}
				fileDefs = append(fileDefs, def)
				continue
			}
			if isComment(trimmed) {
				leading = append(leading, line)
				leadingEndings = append(leadingEndings, ending)
				continue
			}
			if looksLikeRequestLine(trimmed) {
				// A request before the first ### starts an unnamed section,
				// as in REST Client files, with the comments directly above it.
				current, currentUnnamed = newMethod(unnamedSection, 0, source, lineNumber), true
				section = append(append(section[:0], leading...), line)
				sectionEndings = append(append(sectionEndings[:0], leadingEndings...), ending)
				sectionNameLine, bodyStartLine = lineNumber-len(leading), lineNumber-len(leading)
				continue
			}
			return nil, parseErrorf(source, lineNumber,
				"content before first ### section: %s (start each mock with ### Name, or define a file variable with @name = value)", quoteSnippet(trimmed))
		}
		if len(section) == 0 {
			bodyStartLine = lineNumber
		}
		section = append(section, line)
		sectionEndings = append(sectionEndings, ending)
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
		method, err := parseSection(pending, fileDefs, o, source)
		if err != nil {
			return nil, err
		}
		methods = append(methods, method)
	}
	return methods, nil
}

// sectionStatusPattern matches a leading "[code]" in a ### section name.
var sectionStatusPattern = regexp.MustCompile(`^\[\s*(\d+)\s*\]\s*`)

// sectionStatusTemplatePattern matches a leading "[{{placeholder}}]" in a
// ### section name, a status rendered for each response.
var sectionStatusTemplatePattern = regexp.MustCompile(`^\[\s*(\{\{.*?\}\})\s*\]\s*`)

// parseSectionName splits an optional leading "[status]" from a ### section
// name, as in "### [204] Delete a user". Status is 0 when none is given.
// Brackets that do not hold only digits, like "[beta]", stay part of the name.
func parseSectionName(text string) (name string, status int, err error) {
	match := sectionStatusPattern.FindStringSubmatch(text)
	if match == nil {
		return text, 0, nil
	}
	status, convErr := strconv.Atoi(match[1])
	if convErr != nil || status < 200 || status > 999 {
		return "", 0, fmt.Errorf(`invalid response status [%s] in section name (expected a final HTTP status between 200 and 999, example: "### [204] Delete a user")`, match[1])
	}
	return text[len(match[0]):], status, nil
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
		case strings.HasPrefix(line, "//"):
			found.comments = append(found.comments, strings.TrimSpace(line[2:]))
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
	name, value, ok := cutDefinition(line[1:])
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
	case !variableNamePattern.MatchString(name) && !jsonMatcherName(name):
		return definition{}, parseErrorf(source, lineNumber,
			"invalid variable name %q (use letters, digits, '_', '.', '-', starting with a letter or '_')", name)
	}
	return definition{name: name, value: unescapeValue(strings.TrimSpace(value)), line: lineNumber}, nil
}

// cutDefinition splits "name = value" at the first "=" outside brackets, so
// a @json.items[sku=A1].qty matcher keeps its selector.
func cutDefinition(text string) (name, value string, ok bool) {
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth = max(depth-1, 0)
		case '=':
			if depth == 0 {
				return text[:i], text[i+1:], true
			}
		}
	}
	return text, "", false
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
// scopeDefinitions merges file and section definitions (later ones win) and
// then applies command-line overrides. An override keeps the line of the
// definition it replaces; one with no definition gets line 0.
func scopeDefinitions(fileDefs, methodDefs []definition, opts Options) []definition {
	overrides := opts.Vars
	var defaults []definition
	for _, name := range slices.Sorted(maps.Keys(opts.Defaults)) {
		if IsDefaultVariable(name) {
			defaults = append(defaults, definition{name: name, value: opts.Defaults[name]})
		}
	}
	var merged []definition
	index := make(map[string]int)
	for _, def := range slices.Concat(defaults, fileDefs, methodDefs) {
		key := def.name
		if headerName, ok := strings.CutPrefix(key, "header."); ok {
			key = "header." + http.CanonicalHeaderKey(headerName)
		} else if headerName, ok := strings.CutPrefix(key, defaultHeaderPrefix); ok {
			key = defaultHeaderPrefix + http.CanonicalHeaderKey(headerName)
		}
		if i, ok := index[key]; ok {
			merged[i] = def
			continue
		}
		index[key] = len(merged)
		merged = append(merged, def)
	}
	for _, name := range slices.Sorted(maps.Keys(overrides)) {
		if i, ok := index[name]; ok {
			merged[i].value = overrides[name]
			continue
		}
		merged = append(merged, definition{name: name, value: overrides[name]})
	}
	return merged
}

func parseSection(pending pendingSection, fileDefs []definition, opts Options, source string) (Method, error) {
	method := pending.method
	lines := pending.lines
	sectionNameLine := pending.nameLine
	nameStatus := method.Status != 0 || method.StatusTemplate != ""
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

	defs := scopeDefinitions(fileDefs, pending.directives.defs, opts)
	for _, def := range defs {
		if isMatcherName(def.name) || strings.HasPrefix(def.name, defaultHeaderPrefix) {
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
	if err := applyStatusDirective(&method, defs, pending.directives.defs, nameStatus, source); err != nil {
		return method, err
	}
	for _, callback := range method.Callbacks {
		if callback.Method == "" {
			directive := "@callback"
			if callback.Index > 0 {
				directive += "." + strconv.Itoa(callback.Index)
			}
			return method, parseErrorf(source, callback.line,
				`section %q: %s.* directives need "%s = METHOD URL" (example: "%s = POST {{$json.callbackUrl}}")`, method.Name, directive, directive, directive)
		}
		if !callback.retriesSet {
			callback.Retries = method.CallbackRetries
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
	if len(requestLine) == 3 && httpVersionPattern.MatchString(requestLine[2]) {
		requestLine = requestLine[:2]
	}
	if len(requestLine) == 1 && isBareTarget(requestLine[0]) {
		requestLine = []string{http.MethodGet, requestLine[0]}
	} else if len(requestLine) == 2 && isBareTarget(requestLine[0]) && httpVersionPattern.MatchString(requestLine[1]) {
		requestLine = []string{http.MethodGet, requestLine[0]}
	}
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
			`section %q has an invalid HTTP request line %s (expected "METHOD /path", optionally followed by an HTTP version such as HTTP/1.1)`,
			method.Name, quoteSnippet(rawRequest))
	}

	var err error
	method.Method, err = parseRequestMethods(requestLine[0])
	if err != nil {
		return method, parseErrorf(source, lineAt(i), "section %q has an %v", method.Name, err)
	}

	method.Target = requestLine[1]
	if pending.unnamed {
		method.Name = method.Method + " " + method.Target
		// REST Client names a request with a "# @name login" comment.
		for _, comment := range method.Comments {
			if name, ok := strings.CutPrefix(comment, "@name "); ok && strings.TrimSpace(name) != "" {
				method.Name = strings.TrimSpace(name)
				break
			}
		}
	}
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
	if fullURL && target.Port() != "" {
		port, err := parsePort(target.Port())
		if err != nil {
			return method, parseErrorf(source, lineAt(i),
				"section %q has an invalid port in request target %s: %v", method.Name, quoteSnippet(rawTarget), err)
		}
		if method.Port != 0 && method.Port != port {
			return method, parseErrorf(source, lineAt(i),
				"section %q: request target port %d conflicts with @port = %d (line %d); remove one or make them match",
				method.Name, port, method.Port, method.VariableLines["port"])
		}
		method.Port = port
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
	if err := checkWildcard(method.EscapedPath); err != nil {
		return method, parseErrorf(source, lineAt(i), "section %q: %v", method.Name, err)
	}
	if err := checkPathMatchers(&method, pending.directives.defs, source); err != nil {
		return method, err
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
	bodyStart := i
	bodyLines := trimTrailingBlankLines(lines[i:])
	if err := applyDefaultHeaders(&method, defs, opts.Headers, source); err != nil {
		return method, err
	}
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
	if raw, ok := method.Variable("trim"); ok && strings.TrimSpace(raw) == "off" {
		var body strings.Builder
		for j := bodyStart; j < len(lines); j++ {
			body.WriteString(lines[j])
			body.WriteString(pending.endings[j])
		}
		method.Body = body.String()
		return method, nil
	}
	method.Body = strings.Join(bodyLines, "\n")
	return method, nil
}

// defaultHeaderPrefix starts @default.header.Name, a response header for
// every route in scope that does not set it.
const defaultHeaderPrefix = "default.header."

// applyDefaultHeaders adds the @default.header.Name values, then the
// Options.Headers, for each header name the route does not set itself.
// Values may hold placeholders, rendered per response like any response
// header. A @proxy route answers with the upstream's headers, so it gets
// none. A default Content-Type goes to DefaultContentType instead of
// Headers: the server uses it only when it cannot tell the type from a
// "< path" file's extension or by sniffing the body.
func applyDefaultHeaders(method *Method, defs []definition, extra http.Header, source string) error {
	defaults := make(http.Header)
	for _, def := range defs {
		name, ok := strings.CutPrefix(def.name, defaultHeaderPrefix)
		if !ok {
			continue
		}
		if !ValidHeaderName(name) || !ValidHeaderValue(def.value) {
			return parseErrorf(source, def.line,
				`section %q: @%s needs a valid header name and value (example: "@default.header.Content-Type = application/json")`, method.Name, def.name)
		}
		defaults.Set(name, def.value)
	}
	if method.Proxy != "" {
		return nil
	}
	for _, source := range []http.Header{defaults, extra} {
		for name, values := range source {
			if http.CanonicalHeaderKey(name) == "Content-Type" {
				if method.DefaultContentType == "" && method.Headers.Get(name) == "" && len(values) > 0 {
					method.DefaultContentType = values[0]
				}
				continue
			}
			if len(method.Headers.Values(name)) == 0 && len(values) > 0 {
				method.Headers[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
			}
		}
	}
	return nil
}

const fileVariableRemoved = `$file and @file are no longer supported; start the response body with "< path" instead (example: "< ./users.json")`

// isMatcherName reports whether a variable name is a request matcher
// (@header.Name, @query.name, @path.name, @body, @json.path, @xml.path,
// @form.name or @cookie.name) rather than a {{name}} variable.
func isMatcherName(name string) bool {
	if name == "body" {
		return true
	}
	for _, prefix := range []string{"header.", "query.", "path.", "json.", "xml.", "form.", "cookie."} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// PathParams returns the parameter names in an escaped route path (as
// Method.EscapedPath): :name segments and a trailing *name wildcard. A bare *
// wildcard has no name. Escaped %3A and %2A segments are literal text.
func PathParams(path string) []string {
	var names []string
	for _, part := range strings.Split(path, "/") {
		if name, ok := strings.CutPrefix(part, ":"); ok {
			names = append(names, name)
		} else if name, ok := strings.CutPrefix(part, "*"); ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

var wildcardNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// checkWildcard requires a * or *name wildcard to be the last path segment.
func checkWildcard(path string) error {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		name, ok := strings.CutPrefix(part, "*")
		if !ok {
			continue
		}
		if i != len(parts)-1 {
			return fmt.Errorf("wildcard %q must be the last path segment (example: \"/files/*path\")", part)
		}
		if name != "" && !wildcardNamePattern.MatchString(name) {
			return fmt.Errorf("invalid wildcard name %q (use letters, digits and '_', starting with a letter or '_')", name)
		}
	}
	return nil
}

// checkPathMatchers drops file-scoped @path.name matchers for sections whose
// path has no :name parameter, and rejects section-scoped ones.
func checkPathMatchers(method *Method, sectionDefs []definition, source string) error {
	params := make(map[string]bool)
	for _, name := range PathParams(method.EscapedPath) {
		params[name] = true
	}
	for name := range method.MatchPath {
		if params[name] {
			continue
		}
		delete(method.MatchPath, name)
		for _, def := range sectionDefs {
			if def.name == "path."+name {
				return parseErrorf(source, def.line,
					"section %q: @path.%s has no matching :%s parameter in the request path %q", method.Name, name, name, method.Path)
			}
		}
	}
	return nil
}

// parseMatcherValue validates a matcher value and wraps errors with the line.
func parseMatcherValue(method *Method, def definition, value, source string) error {
	if _, err := ParseMatcher(value); err != nil {
		return parseErrorf(source, def.line, "section %q: @%s: %v", method.Name, def.name, err)
	}
	return nil
}

// applyControlVariable interprets the variables the mock server itself
// consumes: @delay, @port and the @header.Name, @query.name and @path.name
// matchers. Other variables are only referenced as {{name}} placeholders.
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
		if err := parseMatcherValue(method, def, value, source); err != nil {
			return err
		}
		method.MatchHeaders.Add(headerName, value)
		return nil
	}
	if name, ok := strings.CutPrefix(def.name, "query."); ok {
		if name == "" {
			return parseErrorf(source, def.line,
				`section %q: @query. requires a parameter name (example: "@query.page = ~[0-9]+")`, method.Name)
		}
		if err := parseMatcherValue(method, def, value, source); err != nil {
			return err
		}
		method.MatchQuery.Set(name, value)
		return nil
	}
	if def.name == "body" {
		if value == "" {
			return parseErrorf(source, def.line,
				`section %q: @body needs a value (use "!" to require an empty body, "*" for any body, or "~regex")`, method.Name)
		}
		if _, err := ParseBodyMatcher(value); err != nil {
			return parseErrorf(source, def.line, "section %q: @body: %v", method.Name, err)
		}
		method.MatchBody = value
		return nil
	}
	if path, ok := strings.CutPrefix(def.name, "json."); ok {
		if _, err := ParseJSONPath(path); err != nil || path == "" {
			return parseErrorf(source, def.line,
				`section %q: @%s needs a dot-separated JSON field path (example: "@json.user.name = alice", or @json.meta["user.id"] for a key with dots)`, method.Name, def.name)
		}
		if err := parseMatcherValue(method, def, value, source); err != nil {
			return err
		}
		method.MatchJSON[path] = value
		return nil
	}
	if path, ok := strings.CutPrefix(def.name, "xml."); ok {
		segments, err := ParseJSONPath(path)
		if err == nil {
			for i, segment := range segments {
				if segment.Quoted && strings.HasPrefix(segment.Name, "@") && (i != len(segments)-1 || len(segment.Name) == 1) {
					err = fmt.Errorf("attribute must be the last segment")
				}
			}
		}
		if err != nil || path == "" {
			return parseErrorf(source, def.line,
				`section %q: @%s needs a dot-separated XML element path starting at the root element (example: "@xml.order.customer.id = 42", or @xml.order["@id"] for an attribute)`, method.Name, def.name)
		}
		if err := parseMatcherValue(method, def, value, source); err != nil {
			return err
		}
		method.MatchXML[path] = value
		return nil
	}
	if name, ok := strings.CutPrefix(def.name, "form."); ok {
		if name == "" {
			return parseErrorf(source, def.line,
				`section %q: @form. requires a field name (example: "@form.grant_type = client_credentials")`, method.Name)
		}
		if err := parseMatcherValue(method, def, value, source); err != nil {
			return err
		}
		method.MatchForm.Set(name, value)
		return nil
	}
	if name, ok := strings.CutPrefix(def.name, "cookie."); ok {
		if name == "" {
			return parseErrorf(source, def.line,
				`section %q: @cookie. requires a cookie name (example: "@cookie.session = *")`, method.Name)
		}
		if err := parseMatcherValue(method, def, value, source); err != nil {
			return err
		}
		method.MatchCookies.Set(name, value)
		return nil
	}
	if key, ok := strings.CutPrefix(def.name, "callback."); ok {
		return applyCallbackDirective(method, def, key, value, source)
	}
	if name, ok := strings.CutPrefix(def.name, "path."); ok {
		if name == "" {
			return parseErrorf(source, def.line,
				`section %q: @path. requires a path parameter name (example: "@path.id = ~[0-9]+")`, method.Name)
		}
		if value == "*" || value == "!" {
			return parseErrorf(source, def.line,
				`section %q: @path.%s = %s is not supported; path parameters are always present (use "~pattern" or an exact value)`, method.Name, name, value)
		}
		if err := parseMatcherValue(method, def, value, source); err != nil {
			return err
		}
		method.MatchPath[name] = value
		return nil
	}
	switch def.name {
	case "status":
		// applyStatusDirective handles it, once the section name's status is known.
	case "require":
		ref, err := ParseRequire(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @require %v", method.Name, err)
		}
		method.Require = ref
	case "missing":
		switch text := strings.ToLower(strings.TrimSpace(value)); text {
		case "next":
			method.Missing, method.MissingNext = 0, true
		default:
			status, err := strconv.Atoi(text)
			if err != nil || status < 200 || status > 999 {
				return parseErrorf(source, def.line, `section %q: @missing must be next or a status between 200 and 999, such as "@missing = 404"`, method.Name)
			}
			method.Missing, method.MissingNext = status, false
		}
	case "etag":
		switch mode := strings.ToLower(strings.TrimSpace(value)); mode {
		case "auto":
			method.ETag = true
		case "off":
			method.ETag = false
		default:
			return parseErrorf(source, def.line, `section %q: @etag must be auto or off`, method.Name)
		}
	case "jwt.secret":
		// Read with Variable when a {{$jwt}} token is signed.
	case "file":
		return parseErrorf(source, def.line, "section %q: %s", method.Name, fileVariableRemoved)
	case "delay":
		delay, err := ParseDelay(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @delay %v", method.Name, err)
		}
		method.Delay, method.DelayMax, method.DelaySigma = delay.Min, delay.Max, delay.Sigma
	case "throttle":
		rate, err := ParseThrottle(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @throttle %v", method.Name, err)
		}
		method.Throttle = rate
	case "rate-limit.key":
		if strings.TrimSpace(def.value) == "" {
			return parseErrorf(source, def.line, `section %q: @rate-limit.key needs a value, such as "@rate-limit.key = {{$header.Authorization}}"`, method.Name)
		}
		method.RateLimitKey = strings.TrimSpace(def.value)
	case "rate-limit":
		limit, window, err := ParseRateLimit(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @rate-limit %v", method.Name, err)
		}
		method.RateLimit, method.RateWindow = limit, window
	case "stream":
		pause, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || pause < 0 {
			return parseErrorf(source, def.line, `section %q: @stream must be a non-negative duration, such as "50ms"`, method.Name)
		}
		method.Stream = pause
	case "fault":
		fault, rate, err := ParseFault(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @fault %v", method.Name, err)
		}
		method.Fault, method.FaultRate = fault, rate
	case "template":
		switch mode := strings.ToLower(strings.TrimSpace(value)); mode {
		case "auto":
			method.Template = ""
		case "on", "off":
			method.Template = mode
		default:
			return parseErrorf(source, def.line, `section %q: @template must be auto, on or off`, method.Name)
		}
	case "trim":
		if mode := strings.TrimSpace(value); mode != "on" && mode != "off" {
			return parseErrorf(source, def.line, `section %q: @trim must be on or off`, method.Name)
		}
	case "seed":
		if strings.TrimSpace(value) == "" {
			return parseErrorf(source, def.line, `section %q: @seed needs a value, such as "@seed = 42"`, method.Name)
		}
	case "compress":
		switch mode := strings.ToLower(strings.TrimSpace(value)); mode {
		case "off":
			method.Compress = ""
		case "gzip", "deflate", "auto":
			method.Compress = mode
		default:
			return parseErrorf(source, def.line, `section %q: @compress must be gzip, deflate, auto or off`, method.Name)
		}
	case "fixture":
		fixture := strings.TrimSpace(value)
		if fixture == "" || strings.Contains(fixture, "{{") {
			return parseErrorf(source, def.line, `section %q: @fixture needs a JSON, CSV or NDJSON file path without placeholders, such as "@fixture = data/users.json"`, method.Name)
		}
		method.Fixture = fixture
	case "callback":
		callbackMethod, target, err := ParseCallback(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @callback %v", method.Name, err)
		}
		callback := method.callback(0, def.line)
		callback.Method, callback.URL = callbackMethod, target
	case "proxy":
		proxy := strings.TrimSpace(value)
		if err := ValidateProxy(proxy); err != nil {
			return parseErrorf(source, def.line, "section %q: @proxy %v", method.Name, err)
		}
		method.Proxy = proxy
	case "port":
		port, err := parsePort(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @port %v", method.Name, err)
		}
		method.Port = port
	case "fallback":
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "true":
			method.Fallback = true
		case "false":
			method.Fallback = false
		default:
			return parseErrorf(source, def.line, `section %q: @fallback must be true or false`, method.Name)
		}
	case "match.chance":
		chance, err := ParseChance(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @match.chance %v", method.Name, err)
		}
		method.Chance = chance
	default:
		if strings.HasPrefix(def.name, "match.") {
			return parseErrorf(source, def.line, `section %q: unknown directive @%s (did you mean @match.chance?)`, method.Name, def.name)
		}
	}
	return nil
}

// applyStatusDirective applies @status, a response status or a template
// such as "{{$query.status | default 200}}". A status in the section name
// wins over a file-level @status; a section cannot set both.
func applyStatusDirective(method *Method, defs, sectionDefs []definition, nameStatus bool, source string) error {
	i := slices.IndexFunc(defs, func(def definition) bool { return def.name == "status" })
	if i < 0 {
		return nil
	}
	def := defs[i]
	if nameStatus {
		if slices.ContainsFunc(sectionDefs, func(d definition) bool { return d.name == "status" }) {
			return parseErrorf(source, def.line, `section %q sets a status in its name and with @status; keep one`, method.Name)
		}
		return nil
	}
	value := strings.TrimSpace(def.value)
	if PlaceholderPattern.MatchString(value) {
		method.StatusTemplate = value
		return nil
	}
	status, err := strconv.Atoi(Expand(value, method.Variables, nil))
	if err != nil || status < 200 || status > 999 {
		return parseErrorf(source, def.line, `section %q: @status must be a status between 200 and 999 or a placeholder, such as "@status = {{$query.status | default 200}}"`, method.Name)
	}
	method.Status = status
	return nil
}

// requirePattern matches a @require reference: $name, optionally in {{ }}.
var requirePattern = regexp.MustCompile(`^(?:\{\{\s*)?\$(` + PlaceholderName + `)(?:\s*\}\})?$`)

// ParseRequire parses a @require value, a request or fixture reference such
// as "$fixture.users[id=$path.id]" or "{{$json.user.id}}", into the name
// after the "$".
func ParseRequire(raw string) (string, error) {
	match := requirePattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return "", errors.New(`must be one $reference, such as "$fixture.users[id=$path.id]" or "$json.user.id"`)
	}
	return match[1], nil
}

// ParseChance parses a @match.chance value, a percentage above 0% and at
// most 100% such as "20%", into a share between 0 and 1. 100% is returned
// as 0, which means the route accepts every matching request.
func ParseChance(raw string) (float64, error) {
	text := strings.TrimSpace(raw)
	percent, err := strconv.ParseFloat(strings.TrimSuffix(text, "%"), 64)
	if err != nil || !strings.HasSuffix(text, "%") || percent <= 0 || percent > 100 {
		return 0, fmt.Errorf(`must be a percentage above 0%% and at most 100%%, such as "20%%"`)
	}
	if percent == 100 {
		return 0, nil
	}
	return percent / 100, nil
}

// callback returns the route's callback with this index, creating it (in
// index order) for the first directive that mentions it.
func (m *Method) callback(index, line int) *Callback {
	i, found := slices.BinarySearchFunc(m.Callbacks, index, func(c *Callback, index int) int { return c.Index - index })
	if !found {
		m.Callbacks = slices.Insert(m.Callbacks, i, &Callback{Index: index, Headers: make(http.Header), line: line})
	}
	return m.Callbacks[i]
}

// applyCallbackDirective handles the directives under @callback: retries
// for the whole section, and for one callback (@callback.N for numbered
// ones) N itself, delay, header.Name, body and retries.
func applyCallbackDirective(method *Method, def definition, key, value, source string) error {
	if key == "retries" {
		retries, err := parseRetries(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: @callback.retries %v", method.Name, err)
		}
		method.CallbackRetries = retries
		return nil
	}
	index, prefix := 0, "@callback."
	if first, rest, _ := strings.Cut(key, "."); first != "" && first[0] >= '0' && first[0] <= '9' {
		n, err := strconv.Atoi(first)
		if err != nil || n < 1 || n > 99 {
			return parseErrorf(source, def.line, `section %q: @callback.%s: callbacks are numbered 1 to 99, such as "@callback.2 = POST {{$json.callbackUrl}}"`, method.Name, first)
		}
		index, key, prefix = n, rest, "@callback."+first+"."
		if key == "" {
			callbackMethod, target, err := ParseCallback(value)
			if err != nil {
				return parseErrorf(source, def.line, "section %q: @callback.%d %v", method.Name, n, err)
			}
			callback := method.callback(n, def.line)
			callback.Method, callback.URL = callbackMethod, target
			return nil
		}
	}
	callback := method.callback(index, def.line)
	switch {
	case key == "delay":
		delay, err := time.ParseDuration(strings.TrimSpace(value))
		if err != nil || delay < 0 {
			return parseErrorf(source, def.line, `section %q: %sdelay must be a non-negative duration, such as "500ms"`, method.Name, prefix)
		}
		callback.Delay = delay
	case key == "body":
		callback.Body = value
	case key == "retries" && index > 0:
		retries, err := parseRetries(value)
		if err != nil {
			return parseErrorf(source, def.line, "section %q: %sretries %v", method.Name, prefix, err)
		}
		callback.Retries, callback.retriesSet = retries, true
	case strings.HasPrefix(key, "header."):
		name := strings.TrimPrefix(key, "header.")
		if !ValidHeaderName(name) {
			return parseErrorf(source, def.line, `section %q: %sheader. needs a valid header name (example: "%sheader.Content-Type = application/json")`, method.Name, prefix, prefix)
		}
		callback.Headers.Set(name, value)
	default:
		return parseErrorf(source, def.line, `section %q: unknown directive @%s (use %sdelay, %sheader.Name, %sbody or @callback.retries)`, method.Name, def.name, prefix, prefix, prefix)
	}
	return nil
}

// parseRetries parses a callback retry count from 0 to maxCallbackRetries.
func parseRetries(value string) (int, error) {
	retries, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || retries < 0 || retries > maxCallbackRetries {
		return 0, fmt.Errorf("must be a whole number from 0 to %d", maxCallbackRetries)
	}
	return retries, nil
}

// ParseCallback parses an @callback value, "METHOD URL", such as
// "POST {{$json.callbackUrl}}". The URL may be entirely a placeholder, so
// only a literal URL is checked here: it must be an absolute http or https
// URL. A placeholder URL is checked when the callback is sent.
func ParseCallback(raw string) (method, target string, err error) {
	const usage = `must be "METHOD URL", such as "POST https://example.com/webhook" or "POST {{$json.callbackUrl}}"`
	method, target, ok := strings.Cut(strings.TrimSpace(raw), " ")
	target = strings.TrimSpace(target)
	if !ok || target == "" || !isHTTPMethod(method) || method == http.MethodConnect {
		return "", "", errors.New(usage)
	}
	if !PlaceholderPattern.MatchString(target) {
		if err := ValidateCallbackURL(target); err != nil {
			return "", "", err
		}
	}
	return method, target, nil
}

// ValidateCallbackURL checks a callback target: an absolute http or https URL
// without credentials.
func ValidateCallbackURL(raw string) error {
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return fmt.Errorf("URL %q must be an absolute http or https URL", raw)
	}
	if target.User != nil {
		return fmt.Errorf("URL %q must not include user credentials", raw)
	}
	return nil
}

// ValidateProxy checks an @proxy value: an absolute http or https URL whose
// scheme and host are literal. Request placeholders such as {{$path.rest}}
// may appear in the path and query.
func ValidateProxy(raw string) error {
	const usage = `must be an http or https URL, such as "https://api.example.com" or "https://api.example.com/v2/{{$path.rest}}"`
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || (!strings.EqualFold(scheme, "http") && !strings.EqualFold(scheme, "https")) {
		return errors.New(usage)
	}
	hostEnd := strings.IndexAny(rest, "/?#")
	if hostEnd < 0 {
		hostEnd = len(rest)
	}
	if strings.Contains(raw[:len(scheme)+3+hostEnd], "{{") {
		return errors.New("placeholders may appear only in the path and query, not in the scheme or host")
	}
	sample := PlaceholderPattern.ReplaceAllString(raw, "x")
	target, err := url.Parse(sample)
	if err != nil || target.Host == "" || target.Hostname() == "" {
		return errors.New(usage)
	}
	if target.Fragment != "" || strings.Contains(sample, "#") {
		return errors.New("must not have a #fragment")
	}
	if target.User != nil {
		return errors.New("must not include user credentials")
	}
	return nil
}

// Delay is a parsed @delay: a fixed Min, a uniform range from Min to Max, or,
// when Sigma is set, a lognormal distribution with median Min and shape
// Sigma (the standard deviation of the delay's logarithm).
type Delay struct {
	Min, Max time.Duration
	Sigma    float64
}

// z99 is the standard normal quantile of the 99th percentile, which turns a
// p50/p99 pair into a lognormal shape.
const z99 = 2.3263478740408408

// maxDelaySigma bounds a lognormal shape; beyond it the tail is so long that
// the p99 is over 1000 times the median.
const maxDelaySigma = 3

// ParseDelay parses an @delay value: a duration such as "250ms"; a range
// "100ms..800ms" from which each response picks a uniformly random delay;
// "lognormal 90ms 0.4" (a median and a shape); or "p50 80ms p99 900ms" (a
// median and a 99th percentile, which give the same lognormal curve).
func ParseDelay(raw string) (Delay, error) {
	text := strings.TrimSpace(raw)
	fields := strings.Fields(text)
	switch {
	case len(fields) == 3 && strings.EqualFold(fields[0], "lognormal"):
		median, err := time.ParseDuration(fields[1])
		sigma, sigmaErr := strconv.ParseFloat(fields[2], 64)
		if err != nil || median <= 0 || sigmaErr != nil || sigma <= 0 || sigma > maxDelaySigma {
			return Delay{}, fmt.Errorf(`"lognormal median shape" needs a positive median and a shape above 0 and at most %d, such as "lognormal 90ms 0.4"`, maxDelaySigma)
		}
		return Delay{Min: median, Sigma: sigma}, nil
	case len(fields) == 4 && strings.EqualFold(fields[0], "p50") && strings.EqualFold(fields[2], "p99"):
		p50, err50 := time.ParseDuration(fields[1])
		p99, err99 := time.ParseDuration(fields[3])
		if err50 != nil || err99 != nil || p50 <= 0 || p99 <= p50 {
			return Delay{}, fmt.Errorf(`"p50 median p99 tail" needs a positive median below the 99th percentile, such as "p50 80ms p99 900ms"`)
		}
		sigma := math.Log(float64(p99)/float64(p50)) / z99
		if sigma > maxDelaySigma {
			return Delay{}, fmt.Errorf("p99 %s is too far above p50 %s (keep it under about 1000 times)", p99, p50)
		}
		return Delay{Min: p50, Sigma: sigma}, nil
	}
	first, second, isRange := strings.Cut(text, "..")
	low, err := time.ParseDuration(strings.TrimSpace(first))
	if err != nil || low < 0 {
		return Delay{}, fmt.Errorf(`must be a non-negative duration such as "250ms", a range such as "100ms..800ms", "lognormal 90ms 0.4" or "p50 80ms p99 900ms"`)
	}
	if !isRange {
		return Delay{Min: low}, nil
	}
	high, err := time.ParseDuration(strings.TrimSpace(second))
	if err != nil || high < low {
		return Delay{}, fmt.Errorf(`range must be "min..max" with max at least min, such as "100ms..800ms"`)
	}
	return Delay{Min: low, Max: high}, nil
}

// P99 is the 99th percentile of a lognormal delay, or 0 for other delays.
func (d Delay) P99() time.Duration {
	if d.Sigma <= 0 {
		return 0
	}
	return time.Duration(float64(d.Min) * math.Exp(z99*d.Sigma))
}

// ParseThrottle parses a @throttle value, a transfer rate such as "32kb/s",
// "1.5mb/s" or "512b/s", into bytes per second. kb is 1024 bytes and mb is
// 1024 kb; the unit is case-insensitive.
func ParseThrottle(raw string) (int64, error) {
	const usage = `must be a rate such as "32kb/s", "1mb/s" or "512b/s"`
	text := strings.ToLower(strings.TrimSpace(raw))
	amount, ok := strings.CutSuffix(text, "/s")
	if !ok {
		return 0, errors.New(usage)
	}
	scale := 0.0
	for _, unit := range []struct {
		suffix string
		scale  float64
	}{{"kb", 1024}, {"mb", 1024 * 1024}, {"b", 1}} {
		if number, found := strings.CutSuffix(amount, unit.suffix); found {
			amount, scale = number, unit.scale
			break
		}
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(amount), 64)
	if err != nil || scale == 0 || value <= 0 || value > 1e12 || int64(value*scale) < 1 {
		return 0, errors.New(usage)
	}
	return int64(value * scale), nil
}

// faultKinds are the @fault values.
var faultKinds = []string{"reset", "truncate", "hang", "empty", "garbage"}

// ParseRateLimit parses a @rate-limit value: a request count, a slash and a
// window, such as "5/1s", "100/1m" or "5/s" (a bare unit means one of it).
func ParseRateLimit(raw string) (limit int, window time.Duration, err error) {
	count, per, ok := strings.Cut(strings.TrimSpace(raw), "/")
	limit, countErr := strconv.Atoi(strings.TrimSpace(count))
	per = strings.TrimSpace(per)
	if per == "s" || per == "m" || per == "h" {
		per = "1" + per
	}
	window, windowErr := time.ParseDuration(per)
	if !ok || countErr != nil || windowErr != nil || limit < 1 || window <= 0 {
		return 0, 0, fmt.Errorf(`must be a request count and a window, such as "5/1s", "100/1m" or "10/s"`)
	}
	return limit, window, nil
}

// ParseFault parses an @fault value: reset, truncate, hang, empty or
// garbage, optionally followed by the share of responses it applies to, such
// as "reset 25%". rate is between 0 and 1.
func ParseFault(raw string) (fault string, rate float64, err error) {
	fields := strings.Fields(strings.ToLower(raw))
	if len(fields) == 0 || len(fields) > 2 || !slices.Contains(faultKinds, fields[0]) {
		return "", 0, fmt.Errorf(`must be reset, truncate, hang, empty or garbage, optionally followed by a percentage such as "reset 25%%"`)
	}
	if len(fields) == 1 {
		return fields[0], 1, nil
	}
	percent, err := strconv.ParseFloat(strings.TrimSuffix(fields[1], "%"), 64)
	if err != nil || !strings.HasSuffix(fields[1], "%") || percent < 0 || percent > 100 {
		return "", 0, fmt.Errorf("percentage %q must be between 0%% and 100%%, such as 25%%", fields[1])
	}
	return fields[0], percent / 100, nil
}

// FileIsTemplate reports whether an external body path holds placeholders
// and so names a different file per request.
func FileIsTemplate(raw string) bool {
	return PlaceholderPattern.MatchString(raw)
}

// parsePort parses a listener port between 1 and 65535.
func parsePort(raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("must be a port number between 1 and 65535, got %q", raw)
	}
	return port, nil
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
	"delay":          {},
	"port":           {},
	"stream":         {},
	"fault":          {},
	"seed":           {},
	"template":       {},
	"trim":           {},
	"proxy":          {},
	"fixture":        {},
	"compress":       {},
	"callback":       {},
	"rate-limit":     {},
	"fallback":       {},
	"match.chance":   {},
	"status":         {},
	"require":        {},
	"missing":        {},
	"etag":           {},
	"jwt.secret":     {},
	"throttle":       {},
	"rate-limit.key": {},
}

// PlaceholderPattern matches {{name}}, {{%name}} and {{$name args}} placeholders
// in bodies and headers. Submatch 1 is the optional "%" (percent-encode) or "$"
// (request parameter or generated value) prefix; submatch 2 is the name;
// submatch 3 is the space-separated arguments, which only {{$name}} accepts.
var PlaceholderPattern = regexp.MustCompile(`\{\{([%$]?)(` + PlaceholderName + `)(?:[ \t]+([^{}]*?))?[ \t]*}}`)

// PlaceholderName is the regexp for a placeholder name such as user.name,
// meta["user.id"] or users[id=$path.id].name: dot-separated fields,
// ["quoted"] keys and [key=value] array selectors.
const PlaceholderName = `[A-Za-z_](?:[A-Za-z0-9_.-]|\["[^"\s{}]+"\]|\[[A-Za-z0-9_-]+=[^\]\s{}"]+\])*`

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
		if raw == "" || FileIsTemplate(raw) {
			// A templated path names a different file per request; it is read
			// on every request, so there is nothing fixed to watch.
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
// (@delay, @port, @stream, @fault, @seed, @template, @trim) and are never referenced as {{name}} in the request target, body, or
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
			if _, ok := controlVariables[name]; ok || line == 0 || strings.HasPrefix(name, "callback.") {
				// Line 0 is a command-line variable with no definition in
				// the file; it need not be used by every file.
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
	collectPlaceholders(method.File, used)
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
