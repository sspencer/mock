package mock

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

type routeParser struct {
	baseDir      string
	fileName     string
	routes       []*route
	defaultDelay time.Duration
	delay        time.Duration
}

type parseState int

const (
	stateNone parseState = iota
	stateBody
)

func RoutesReader(r io.Reader, delay time.Duration) (routes []*Route, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	sp := &routeParser{baseDir: dir, defaultDelay: delay, delay: delay}
	if err = sp.parseReader(r); err != nil {
		return nil, err
	}

	return mergeRoutes(sp.routes), nil
}

// RoutesFiles parses API file(s).
func RoutesFiles(files []string, delay time.Duration) ([]*Route, error) {
	sp := &routeParser{defaultDelay: delay, delay: delay}

	for _, fn := range files {
		err := sp.parseFile(fn)
		if err != nil {
			return nil, err
		}
	}

	return mergeRoutes(sp.routes), nil
}

// parseFile parses the incoming routes from a file
func (sp *routeParser) parseFile(fn string) error {
	f, err := os.Open(fn)
	if err != nil {
		return err
	}

	defer f.Close()

	sp.fileName = fn
	sp.baseDir = path.Dir(fn)

	return sp.parseReader(f)
}

// parseReader parses the incoming routes from a reader
func (sp *routeParser) parseReader(r io.Reader) error {
	scanner := bufio.NewScanner(r)
	state := stateNone

	var body []byte
	lineNum := 0
	multiLine := false
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trim := strings.TrimSpace(line)

		switch state {
		case stateNone:
			multiLine = false
			if len(trim) == 0 || line[0:1] == "#" {
				continue
			}

			route, err := sp.parse(line, lineNum)
			if err != nil {
				return err
			}

			if route != nil {
				sp.routes = append(sp.routes, route)
				if len(route.Body) == 0 {
					state = stateBody
					body = []byte{}
				} else {
					// route has optional @file response, no Response expected
					state = stateNone
				}
			}

		case stateBody:
			if len(trim) > 0 || multiLine {
				if trim == `"""` {
					if !multiLine {
						multiLine = true
					} else {
						if len(sp.routes) > 0 {
							sp.routes[len(sp.routes)-1].Body = body
							body = []byte{}
						}
						state = stateNone
					}
				} else {
					line = line + "\r\n"
					body = append(body, line...)
				}
			} else {
				if len(body) > 0 && len(sp.routes) > 0 {
					sp.routes[len(sp.routes)-1].Body = body
					body = []byte{}
				}
				state = stateNone
			}

		default:
			continue
		}
	}

	if len(body) > 0 && len(sp.routes) > 0 {
		sp.routes[len(sp.routes)-1].Body = body
	}

	return nil
}

func (sp *routeParser) lineError(msg string, lineNum int, line string) error {
	if sp.fileName == "" {
		return fmt.Errorf(msg, lineNum, line)
	}

	return fmt.Errorf("file %q, "+msg, sp.fileName, lineNum, line)
}

func (sp *routeParser) parse(line string, lineNum int) (*route, error) {
	ok, err := sp.parseVariable(line)
	if err != nil {
		return nil, fmt.Errorf("invalid duration line %d: %s", lineNum, line)
	} else if ok {
		return nil, nil
	}

	tokens := strings.Split(line, " ")
	tlen := len(tokens)
	if tlen < 3 {
		return nil, sp.lineError("parsing line %d: %s", lineNum, line)
	}

	if !sp.isHTTPMethod(tokens[0]) {
		return nil, sp.lineError("invalid http method, line %d: %s", lineNum, line)
	}

	if !sp.isHTTPStatusCode(tokens[1]) {
		return nil, sp.lineError("invalid http status code, line %d: %s", lineNum, line)
	}

	if !sp.isHTTPPath(tokens[2]) {
		return nil, sp.lineError("invalid path, line %d: %s", lineNum, line)
	}

	contentType := "application/json"
	var body []byte

	if tlen > 3 {
		rest := strings.Join(tokens[3:tlen], "")
		rlen := len(rest)
		if rlen < 2 {
			return nil, sp.lineError("invalid optional, line %d: %s", lineNum, line)
		}

		first := rest[0:1]
		last := rest[rlen-1 : rlen]

		if first == "\"" && last == "\"" {
			contentType = rest[1 : rlen-1]
		}

		if first == "@" {
			var err error
			fn := path.Join(sp.baseDir, path.Clean(rest[1:rlen]))
			contentType = mime.TypeByExtension(path.Ext(fn))
			if body, err = ioutil.ReadFile(fn); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				return nil, sp.lineError("could not read file, line %d: %s", lineNum, line)
			}
		}
	}

	r := &route{}
	r.Method = strings.ToUpper(tokens[0])
	r.Status, _ = strconv.Atoi(tokens[1])
	r.Path = sp.cleanPath(tokens[2])
	r.ContentType = contentType
	r.Body = body
	r.Delay = sp.delay

	sp.delay = sp.defaultDelay

	return r, nil
}

func (sp *routeParser) isHTTPMethod(m string) bool {
	switch strings.ToUpper(m) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func (sp *routeParser) isHTTPStatusCode(code string) bool {
	var n int
	var err error

	if n, err = strconv.Atoi(code); err != nil {
		return false
	}

	return n >= http.StatusContinue && n <= http.StatusNetworkAuthenticationRequired
}

func (sp *routeParser) isHTTPPath(p string) bool {
	// just check for leading "/", everything else is encoded later
	return p[0:1] == "/"
}

func (sp *routeParser) cleanPath(p string) string {
	items := strings.Split(p, "/")
	var uri []string
	for _, i := range items {
		uri = append(uri, url.PathEscape(i))
	}

	return strings.Join(uri, "/")
}

func (sp *routeParser) parseVariable(line string) (bool, error) {
	if strings.HasPrefix(line, "delay:") {
		delay, err := time.ParseDuration(strings.TrimSpace(line[6:]))
		if err != nil {
			return false, err
		}

		sp.delay = delay
		return true, nil
	}

	return false, nil
}
