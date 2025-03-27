package data

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
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
	variableRegex = regexp.MustCompile(`@\s*([a-zA-Z][\w]*)\s*=\s*(.+)`)
)

// route representation during parse, with just a single response.
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

type parser struct {
	baseDir      string
	fileName     string
	routes       []*route
	defaultDelay time.Duration
	route        *route
	globalVars   map[string]string
}

type parseState int

const (
	stateNone parseState = iota
	stateVariable
	stateResponse
	stateHeader
	stateBody
)

func newParser(baseDir, fileName string) *parser {
	return &parser{
		baseDir:    baseDir,
		fileName:   fileName,
		globalVars: make(map[string]string),
	}
}

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

// String returns an internal representation of a single route, used in
// *.golden files when testing
func (r *route) String() string {
	hdr := ""
	for n, v := range r.header {
		hdr += n + v
	}
	return fmt.Sprintf("%s: %s %s delay=%s status=%d header=%d body=%d",
		r.name,
		r.method,
		r.path,
		r.delay,
		r.status,
		len(hdr),
		len(r.body))
}

func (p *parser) String() string {
	sb := strings.Builder{}
	for _, r := range p.routes {
		sb.WriteString(r.String())
		sb.WriteString("\n")
	}

	return sb.String()
}

// A route looks like this:
//     ### RequestDetails Name          # parseNone
//     # @status 200             # parseVariable
//     POST /users               # parseRequest
//     Content-Type: plain/text  # parseHeader
//                               # "blank line"
//     response body             # parseBody

// parse the incoming routes from a reader
func (p *parser) parse(r io.Reader) error {
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
		p.appendRoute()
	}
	return nil
}

// parseName assumes line starts with "###" otherwise returns UUID
func (p *parser) getName(line string) string {
	name := strings.TrimSpace(line[3:])
	if name == "" {
		name = uuid.New().String()
	}

	return name
}

// parseNone looks for start of a new http request, which is signified by "###"
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
		tokens := variableRegex.FindStringSubmatch(line)
		if len(tokens) == 3 {
			name := tokens[1]
			value := strings.TrimSpace(tokens[2])

			if name == "delay" {
				p.defaultDelay, _ = time.ParseDuration(value)
			}

			p.globalVars[name] = value
		}
	}

	return stateNone, nil
}

// parseVariable looks for "# @var value" definitions immediately after new request definition
func (p *parser) handleStateVariable(line string, lineNum int) (parseState, error) {
	// skip blank line, continue looking for variables
	if strings.TrimSpace(line) == "" {
		return stateVariable, nil
	}

	// route ended early, start over
	if strings.HasPrefix(line, recordStartIndicator) {
		p.route = nil
		return stateNone, nil
	}

	// if line doesn't start with "#", move to next state
	if line[:1] != "#" {
		return p.handleStateRequest(line, lineNum)
	}

	// variable specification:
	// # @name=value
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
			return stateVariable, nil

		case "status":
			status, err := strconv.Atoi(val)
			statusText := http.StatusText(status)
			if err != nil || statusText == "" {
				return stateNone, p.lineError("invalid status, line %d: %s", lineNum, line)
			}

			p.route.status = status

		case "file":
			// TBD verify file is readable
			var err error
			fn := path.Join(p.baseDir, path.Clean(val))
			p.route.header["content-type"] = mime.TypeByExtension(path.Ext(fn))
			if p.route.body, err = os.ReadFile(fn); err != nil {
				log.Println(err.Error())
				return stateNone, p.lineError("could not read file, line %d: %s", lineNum, line)
			}

		default:
			return stateNone, p.lineError("unrecognized variable, line %d: %s", lineNum, line)
		}
	}

	return stateVariable, nil
}

// parseRequest expects one http method and url, like "GET /users"
func (p *parser) handleStateRequest(line string, lineNum int) (parseState, error) {
	req, err := p.getRequest(line, lineNum)
	if err != nil {
		return stateNone, err
	}

	p.route.method = req.method
	p.route.path = req.uri
	p.route.uriKey = req.key
	p.route.uriVal = req.val

	return stateHeader, nil
}

// parseHeader expects 0 or more lines with "Content-Type: application/json"
func (p *parser) handleStateHeader(line string, lineNum int) (parseState, error) {
	if strings.TrimSpace(line) == "" {
		return stateBody, nil
	}

	tokens := strings.SplitN(line, ":", 2)
	p.route.header[strings.ToLower(tokens[0])] = tokens[1]

	return stateHeader, nil
}

// parseLine expects an additional line to make up the http request
func (p *parser) handleStateBody(line string, lineNum int) (parseState, error) {
	if strings.HasPrefix(line, recordStartIndicator) {
		p.appendRoute()
		return p.handleStateNone(line, lineNum)
	}

	p.route.body = append(p.route.body, line+"\r\n"...)
	return stateBody, nil
}

func (p *parser) appendRoute() {
	if len(p.route.path) > 0 {
		p.route.body = bytes.TrimSpace(p.route.body)
		p.routes = append(p.routes, p.route)
	}
}

type requestInfo struct {
	method string
	uri    string
	key    string
	val    string
}

// getRequest returns "[<http method>] <url>" from line
func (p *parser) getRequest(line string, lineNum int) (req *requestInfo, err error) {
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

func getVarParams(uri string) (string, string) {
	u, err := url.Parse(uri)
	if err == nil {
		values := u.Query()
		for key, value := range values {
			return key, value[0]
		}
	}
	return "", ""
}

func (p *parser) lineError(msg string, lineNum int, line string) error {
	if p.fileName == "" {
		return fmt.Errorf(msg, lineNum, line)
	}

	return fmt.Errorf("file %q, "+msg, p.fileName, lineNum, line)
}

func (p *parser) isHTTPMethod(m string) bool {
	switch strings.ToUpper(m) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// isHTTPPath verifies string looks like an url (has leading "/")
func (p *parser) isHTTPPath(u string) bool {
	x, err := url.Parse(u)
	if err != nil {
		return false
	}

	urlPath := x.Path

	// just check for leading "/", everything else is encoded later
	return len(urlPath) != 0 && urlPath[0:1] == "/"
}

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
