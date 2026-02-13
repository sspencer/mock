package data

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultContentType      = "text/html; charset=utf-8"
	recordStartIndicator    = "###"
	globalVariableIndicator = "@"
)

// parse @variable = value
var (
	variableRegex = regexp.MustCompile(`@\s*([a-zA-Z]\w*)\s*=\s*(.+)`)
)

// Parser defines an interface for parsing mock files.
// This abstraction allows for different parsing strategies or extensions.
type Parser interface {
	Parse(r io.Reader) error
	GetRoutes() []*Endpoint
	GetGlobalVars() map[string]string
}

// parser implements the Parser interface.
// It parses HTTP mock files into endpoints and global variables.
type parser struct {
	baseDir      string
	fileName     string
	routes       []*route
	defaultDelay time.Duration
	route        *route
	globalVars   map[string]string
}

// NewParser creates a new parser instance.
// It initializes the parser with the base directory and file name for error reporting.
func NewParser(baseDir, fileName string) Parser {
	return &parser{
		baseDir:    baseDir,
		fileName:   fileName,
		globalVars: make(map[string]string),
	}
}

// Parse processes the input reader and builds routes.
// It uses a state machine to handle different sections of the file, reducing complexity.
func (p *parser) Parse(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	state := stateNone
	var err error
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		switch state {
		case stateNone:
			state, err = p.handleStateNone(line, lineNum)
		case stateVariable:
			state, err = p.handleStateVariable(line, lineNum)
		case stateResponse:
			state, err = p.handleStateRequest(line, lineNum)
		case stateHeader:
			state, err = p.handleStateHeader(line, lineNum)
		case stateBody:
			state, err = p.handleStateBody(line, lineNum)
		}

		if err != nil {
			return err
		}
	}

	if p.route != nil {
		p.finalizeRoute()
	}
	return nil
}

// GetRoutes returns the parsed routes as endpoints.
// It merges routes and applies global variables.
func (p *parser) GetRoutes() []*Endpoint {
	return merge(p.routes, p.globalVars)
}

// GetGlobalVars returns the global variables map.
func (p *parser) GetGlobalVars() map[string]string {
	return p.globalVars
}

// finalizeRoute appends the current route to the list after trimming whitespace.
func (p *parser) finalizeRoute() {
	if len(p.route.path) > 0 {
		p.route.body = bytes.TrimSpace(p.route.body)
		p.routes = append(p.routes, p.route)
	}
}

// handleStateNone looks for the start of a new HTTP request.
// It initializes a new route when "###" is encountered.
func (p *parser) handleStateNone(line string, lineNum int) (parseState, error) {
	if strings.HasPrefix(line, recordStartIndicator) {
		p.route = &route{
			name:   p.getName(line),
			status: http.StatusOK,
			delay:  p.defaultDelay,
			header: map[string]string{"content-type": defaultContentType},
		}
		return stateVariable, nil
	} else if strings.HasPrefix(line, globalVariableIndicator) {
		return stateNone, p.handleGlobalVariable(line, lineNum)
	}
	return stateNone, nil
}

// handleGlobalVariable processes global variable definitions (e.g., @delay=value).
func (p *parser) handleGlobalVariable(line string, lineNum int) error {
	tokens := variableRegex.FindStringSubmatch(line)
	if len(tokens) == 3 {
		name := tokens[1]
		value := strings.TrimSpace(tokens[2])

		if name == "delay" {
			if delay, err := time.ParseDuration(value); err == nil {
				p.defaultDelay = delay
			} else {
				return p.lineError("invalid duration, line %d: %s", lineNum, line)
			}
		}
		p.globalVars[name] = value
	}
	return nil
}

// handleStateVariable processes route-specific variables.
// It handles directives like @delay, @status, and @file.
func (p *parser) handleStateVariable(line string, lineNum int) (parseState, error) {
	if strings.TrimSpace(line) == "" {
		return stateVariable, nil
	}

	if strings.HasPrefix(line, recordStartIndicator) {
		p.route = nil
		return stateNone, nil
	}

	if !strings.HasPrefix(line, "#") {
		return p.handleStateRequest(line, lineNum)
	}

	tokens := variableRegex.FindStringSubmatch(line[1:])
	if len(tokens) == 3 {
		name := tokens[1]
		val := strings.TrimSpace(strings.Trim(tokens[2], "\""))
		switch name {
		case "delay":
			delay, err := time.ParseDuration(val)
			if err != nil {
				return stateNone, fmt.Errorf("invalid duration, line %d: %s", lineNum, line)
			}
			p.route.delay = delay
		case "status":
			status, err := strconv.Atoi(val)
			if err != nil || http.StatusText(status) == "" {
				return stateNone, p.lineError("invalid status, line %d: %s", lineNum, line)
			}
			p.route.status = status
		case "file":
			fn := path.Join(p.baseDir, path.Clean(val))
			p.route.header["content-type"] = mime.TypeByExtension(path.Ext(fn))
			body, err := os.ReadFile(fn)
			if err != nil {
				return stateNone, p.lineError(fmt.Sprintf("could not read file %q: %v", fn, err), lineNum, line)
			}
			p.route.body = body
		default:
			msg := fmt.Sprintf("unrecognized variable %q. Did you mean one of: delay, status, or file?", name)
			return stateNone, p.lineError(msg+", line %d: %s", lineNum, line)
		}
	}
	return stateVariable, nil
}

// handleStateRequest parses the HTTP method and path.
// It expects a single line like "GET /users".
func (p *parser) handleStateRequest(line string, lineNum int) (parseState, error) {
	req, err := p.parseRequest(line, lineNum)
	if err != nil {
		return stateNone, err
	}

	p.route.method = req.method
	p.route.path = req.uri
	p.route.uriKey = req.key
	p.route.uriVal = req.val

	return stateHeader, nil
}

// handleStateHeader processes optional headers.
// It accumulates headers until an empty line is encountered.
func (p *parser) handleStateHeader(line string, lineNum int) (parseState, error) {
	if strings.TrimSpace(line) == "" {
		return stateBody, nil
	}

	tokens := strings.SplitN(line, ":", 2)
	if len(tokens) < 2 {
		return stateHeader, p.lineError("malformed header, line %d: %s", lineNum, line)
	}

	headerName := strings.ToLower(tokens[0])
	headerValue := strings.TrimSpace(tokens[1])
	if headerName == "content-type" && headerValue == "" {
		headerValue = defaultContentType
	}
	p.route.header[headerName] = headerValue

	return stateHeader, nil
}

// handleStateBody accumulates the response body.
// It continues until the next "###" or end of file.
func (p *parser) handleStateBody(line string, lineNum int) (parseState, error) {
	if strings.HasPrefix(line, recordStartIndicator) {
		p.finalizeRoute()
		return p.handleStateNone(line, lineNum)
	}

	p.route.body = append(p.route.body, line+"\r\n"...)
	return stateBody, nil
}

// parseRequest extracts method and URI from a line.
// It supports placeholders like :id.
func (p *parser) parseRequest(line string, lineNum int) (*requestInfo, error) {
	tokens := strings.Split(line, " ")
	switch len(tokens) {
	case 1:
		if p.isHTTPPath(tokens[0]) {
			uri, err := p.cleanPath(tokens[0])
			if err != nil {
				return nil, err
			}
			k, v := getVarParams(tokens[0])
			return &requestInfo{
				method: http.MethodGet,
				uri:    uri,
				key:    k,
				val:    v,
			}, nil
		}
	case 2:
		if p.isHTTPMethod(tokens[0]) && p.isHTTPPath(tokens[1]) {
			method := strings.ToUpper(tokens[0])
			uri, err := p.cleanPath(tokens[1])
			if err != nil {
				return nil, err
			}
			k, v := getVarParams(tokens[1])
			return &requestInfo{
				method: method,
				uri:    uri,
				key:    k,
				val:    v,
			}, nil
		}
	}
	return nil, p.lineError("unrecognized request, line %d: %s", lineNum, line)
}

// getName generates a name for the route from the "###" line.
// Defaults to a UUID if no name is provided.
func (p *parser) getName(line string) string {
	name := strings.TrimSpace(line[3:])
	if name == "" {
		name = uuid.New().String()
	}
	return name
}

// lineError creates a formatted error message with line number and content.
func (p *parser) lineError(msg string, lineNum int, line string) error {
	if p.fileName == "" {
		return fmt.Errorf(msg, lineNum, line)
	}
	return fmt.Errorf("file %q, "+msg, p.fileName, lineNum, line)
}

// isHTTPMethod checks if the string is a valid HTTP method.
func (p *parser) isHTTPMethod(m string) bool {
	switch strings.ToUpper(m) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// isHTTPPath checks if the string resembles a valid HTTP path (starts with "/").
func (p *parser) isHTTPPath(u string) bool {
	x, err := url.Parse(u)
	if err != nil {
		return false
	}
	urlPath := x.Path
	return len(urlPath) != 0 && urlPath[0:1] == "/"
}

// cleanPath normalizes and escapes the URI path.
// It converts :param to {param} for routing.
func (p *parser) cleanPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	uri = u.Path
	items := strings.Split(uri, "/")
	s := make([]string, len(items))
	for i, item := range items {
		if strings.HasPrefix(item, ":") && len(item) > 1 {
			s[i] = "{" + url.PathEscape(item[1:]) + "}"
		} else {
			s[i] = url.PathEscape(item)
		}
	}
	return strings.Join(s, "/"), nil
}

// getVarParams extracts the first query parameter key-value pair.
func getVarParams(uri string) (string, string) {
	u, err := url.Parse(uri)
	if err == nil {
		values := u.Query()
		for key, value := range values {
			if len(value) > 0 {
				return key, value[0]
			}
			return key, ""
		}
	}
	return "", ""
}

// requestInfo holds parsed request details.
type requestInfo struct {
	method string
	uri    string
	key    string
	val    string
}

// parseState represents the current parsing state.
type parseState int

const (
	stateNone parseState = iota
	stateVariable
	stateResponse
	stateHeader
	stateBody
)

// String returns a string representation of the parse state.
func (s parseState) String() string {
	switch s {
	case stateNone:
		return "NONE"
	case stateVariable:
		return "VARIABLE"
	case stateResponse:
		return "RESPONSE"
	case stateHeader:
		return "HEADER"
	default:
		return "BODY"
	}
}

// route represents a single route during parsing.
// It is converted to an Endpoint after merging.
type route struct {
	name   string
	method string
	path   string
	uriKey string
	uriVal string
	status int
	delay  time.Duration
	body   []byte
	header map[string]string
}

// String returns a debug representation of the route.
func (r *route) String() string {
	var hdr strings.Builder
	for n, v := range r.header {
		hdr.WriteString(n + v)
	}
	return fmt.Sprintf("%s: %s %s delay=%s status=%d header=%d body=%d",
		r.name, r.method, r.path, r.delay, r.status, len(hdr.String()), len(r.body))
}

// String returns a string representation of all routes.
func (p *parser) String() string {
	sb := strings.Builder{}
	for _, r := range p.routes {
		sb.WriteString(r.String())
		sb.WriteString("\n")
	}
	return sb.String()
}
