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
)

const defaultContentType = "text/html; charset=utf-8"

// parse @variable = value
var (
	variableRegex = regexp.MustCompile("(@[a-z][-a-z0-9_]+)=?(.*)?")
)

type auth struct {
	authType authType
	username string
	password string
}

// route representation during parse, with just a single response.
type route struct {
	name   string
	method string
	path   string
	status int
	delay  time.Duration
	auth   auth
	body   []byte
	header map[string]string
}

type parser struct {
	baseDir      string
	fileName     string
	routes       []*route
	defaultDelay time.Duration
	route        *route
}

type authType int

const (
	authTypeNone authType = iota
	authTypeBasic
)

type parseState int

const (
	stateNone parseState = iota
	stateVariable
	stateResponse
	stateHeader
	stateBody
)

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

// readFile parses the incoming routes from a file
func (p *parser) readFile(fn string) error {
	f, err := os.Open(fn)
	if err != nil {
		return err
	}

	defer f.Close()

	p.fileName = fn
	p.baseDir = path.Dir(fn)

	return p.parse(f)
}

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
			state, err = p.parseNone(line)

		case stateVariable:
			state, err = p.parseVariable(line, lineNum)

		case stateResponse:
			state, err = p.parseRequest(line, lineNum)

		case stateHeader:
			state, err = p.parseHeader(line)

		case stateBody:
			state, err = p.parseBody(line)
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

func (p *parser) parseNone(line string) (parseState, error) {
	if len(line) >= 3 && line[:3] == "###" {
		// TBD save previous response
		name := strings.TrimSpace(line[3:])
		p.route = &route{
			name:   name,
			status: http.StatusOK,
			delay:  p.defaultDelay,
			header: map[string]string{"content-type": defaultContentType},
		}
		return stateVariable, nil
	}

	return stateNone, nil
}

func (p *parser) parseVariable(line string, lineNum int) (parseState, error) {
	// skip blank line
	if len(strings.TrimSpace(line)) == 0 {
		return stateVariable, nil
	}

	if len(line) >= 3 && line[:3] == "###" {
		p.route = nil
		return stateNone, nil
	}

	// if line doesn't start with "#", move to next state
	if line[:1] != "#" {
		// return stateResponse, nil
		return p.parseRequest(line, lineNum)
	}

	// variable specification:
	// # @name value
	// # @name=value
	tokens := variableRegex.FindStringSubmatch(line[1:])
	if tokens != nil && len(tokens) == 3 {
		name := tokens[1]
		val := strings.TrimSpace(strings.Trim(tokens[2], "\""))
		switch name {
		case "@delay":
			delay, err := time.ParseDuration(val)
			if err != nil {
				return stateNone, fmt.Errorf("invalid duration, line %d: %s", lineNum, line)
			}

			p.route.delay = delay
			return stateVariable, nil

		case "@status":
			status, err := strconv.Atoi(val)
			statusText := http.StatusText(status)
			if err != nil || statusText == "" {
				return stateNone, p.lineError("invalid status, line %d: %s", lineNum, line)
			}

			p.route.status = status

		case "@basicauth":
			vals := strings.Split(val, " ")
			if len(vals) != 2 {
				return stateNone, fmt.Errorf("invalid Auth, line %d: %s", lineNum, line)
			}

			p.route.auth = auth{
				authType: authTypeBasic,
				username: vals[0],
				password: vals[1],
			}

		case "@file":
			// TBD verify file is readable
			var err error
			fn := path.Join(p.baseDir, path.Clean(val))
			p.route.header["content-type"] = mime.TypeByExtension(path.Ext(fn))
			if p.route.body, err = os.ReadFile(fn); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				return stateNone, p.lineError("could not read file, line %d: %s", lineNum, line)
			}

		default:
			return stateNone, p.lineError("unrecognized variable, line %d: %s", lineNum, line)
		}
	}

	return stateVariable, nil
}

func (p *parser) parseRequest(line string, lineNum int) (parseState, error) {
	method, uri, err := p.getRequest(line, lineNum)

	if err != nil {
		return stateNone, err
	}

	p.route.method = method
	p.route.path = uri

	return stateHeader, nil
}

func (p *parser) parseHeader(line string) (parseState, error) {
	if len(strings.TrimSpace(line)) == 0 {
		return stateBody, nil
	}

	tokens := strings.SplitN(line, ":", 2)
	p.route.header[strings.ToLower(tokens[0])] = tokens[1]

	return stateHeader, nil
}

func (p *parser) parseBody(line string) (parseState, error) {
	if len(line) >= 3 && line[:3] == "###" {
		p.appendRoute()
		return p.parseNone(line)
	}

	line = line + "\r\n"
	p.route.body = append(p.route.body, line...)

	return stateBody, nil
}

func (p *parser) appendRoute() {
	if len(p.route.path) > 0 {
		p.route.body = bytes.TrimSpace(p.route.body)
		p.routes = append(p.routes, p.route)
	}
}

func (p *parser) getRequest(line string, lineNum int) (string, string, error) {

	tokens := strings.Split(line, " ")
	switch len(tokens) {
	case 1:
		if p.isHTTPPath(tokens[0]) {
			return http.MethodGet, tokens[0], nil
		}

	case 2:
		if p.isHTTPMethod(tokens[0]) && p.isHTTPPath(tokens[1]) {
			return strings.ToUpper(tokens[0]), p.cleanPath(tokens[1]), nil
		}
	}

	return "", "", p.lineError("unrecognized request, line %d: %s", lineNum, line)
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

func (p *parser) isHTTPPath(url string) bool {
	// just check for leading "/", everything else is encoded later
	return url[0:1] == "/"
}

func (p *parser) cleanPath(uri string) string {
	items := strings.Split(uri, "/")
	var s []string
	for _, i := range items {
		s = append(s, url.PathEscape(i))
	}

	return strings.Join(s, "/")
}
