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
	defaultDelay time.Duration
	delay        time.Duration
}

type parseState int

const (
	stateNone parseState = iota
	stateBody
)

// RoutesFile parses an API file.
func RoutesFile(fn string, delay time.Duration) (routes []*Route, err error) {
	f, err := os.Open(fn)
	if err != nil {
		return
	}

	defer f.Close()
	dir := path.Dir(fn)
	return parseReader(f, dir, delay)
}

func RoutesReader(r io.Reader, delay time.Duration) (routes []*Route, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	return parseReader(r, dir, delay)
}

// parseReader parses the incoming routes file
func parseReader(r io.Reader, dir string, delay time.Duration) ([]*Route, error) {
	var routes []*route
	sp := &routeParser{baseDir: dir, defaultDelay: delay, delay: delay}

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
				return nil, err
			}

			if route != nil {
				routes = append(routes, route)
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
						if len(routes) > 0 {
							routes[len(routes)-1].Body = body
							body = []byte{}
						}
						state = stateNone
					}
				} else {
					line = line + "\r\n"
					body = append(body, line...)
				}
			} else {
				if len(body) > 0 && len(routes) > 0 {
					routes[len(routes)-1].Body = body
					body = []byte{}
				}
				state = stateNone
			}

		default:
			continue
		}
	}

	if len(body) > 0 && len(routes) > 0 {
		routes[len(routes)-1].Body = body
	}

	return mergeRoutes(routes), nil
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
		return nil, fmt.Errorf("parsing line %d: %s", lineNum, line)
	}

	if !sp.isHTTPMethod(tokens[0]) {
		return nil, fmt.Errorf("invalid http method, line %d: %s", lineNum, line)
	}

	if !sp.isHTTPStatusCode(tokens[1]) {
		return nil, fmt.Errorf("invalid http status code, line %d: %s", lineNum, line)
	}

	if !sp.isHTTPPath(tokens[2]) {
		return nil, fmt.Errorf("invalid path, line %d: %s", lineNum, line)
	}

	contentType := "application/json"
	body := []byte{}

	if tlen > 3 {
		rest := strings.Join(tokens[3:tlen], "")
		rlen := len(rest)
		if rlen < 2 {
			return nil, fmt.Errorf("invalid optional, line %d: %s", lineNum, line)
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
				return nil, fmt.Errorf("could not read file, line %d: %s", lineNum, line)
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
	uri := []string{}
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
