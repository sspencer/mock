package data

import (
	"fmt"
	"github.com/sspencer/mock/internal/colorlog"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
)

var (
	// replace {{params}} in body
	replacerRegex = regexp.MustCompile(`\{\{[^}]+}}`)
)

// Endpoint represents the mocked route and can have one or more responses.
type Endpoint struct {
	Method    string
	Path      string
	Index     int
	Responses []Response
}

type Response struct {
	Name   string
	Status int
	Header map[string]string
	Delay  time.Duration
	Body   []byte
}

func (e *Endpoint) String() string {
	var b strings.Builder
	nr := len(e.Responses)
	for i, resp := range e.Responses {
		contentType, _ := resp.Header["content-type"]
		fmt.Fprintf(&b, " %3d | %-6s %-28s | %-24s | %4d bytes | %s", resp.Status, e.Method, e.Path, contentType, len(resp.Body), resp.Delay)
		if nr > 1 && i < nr-1 {
			fmt.Fprintln(&b)
		}
	}

	return b.String()
}

// Handle returns a HTTP handler method for the given endpoint.
func (e *Endpoint) Handle(logger colorlog.ResponseLoggerFunc) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		if e.Index >= len(e.Responses) {
			e.Index = 0
		}

		resp := e.Responses[e.Index]

		logger(resp.Status, r)
		for n, v := range resp.Header {
			w.Header().Add(n, v)
		}
		w.WriteHeader(resp.Status)

		if resp.Delay > 0 {
			time.Sleep(resp.Delay)
		}

		// replace {{params}} and {{variables}} in body
		out := substituteVars(replacerRegex.ReplaceAllFunc(resp.Body, substituteParams(ps)))
		w.Write(out)

		e.Index++
	}
}

// Combine duplicate routes (Method/Path) into an Endpoint with one or more responses
func mergeRoutes(apis []*route) []*Endpoint {
	var routes []*Endpoint
	m := make(map[string]*Endpoint)
	for _, t := range apis {
		key := fmt.Sprintf("%s:%s", t.Method, t.Path)
		resp := Response{
			Status: t.Status,
			Body:   t.Body,
			Header: t.Header,
			Delay:  t.Delay,
		}

		if route, ok := m[key]; ok {
			route.Responses = append(route.Responses, resp)
		} else {
			m[key] = &Endpoint{
				Method:    t.Method,
				Path:      t.Path,
				Responses: []Response{resp},
			}
		}
	}

	for _, s := range m {
		routes = append(routes, s)
	}

	return routes
}

// GetEndpointsFromReader parses a reader from <stdin> or similar
func GetEndpointsFromReader(r io.Reader, delay time.Duration) (routes []*Endpoint, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	sp := &parser{baseDir: dir, defaultDelay: delay}
	if err = sp.parse(r); err != nil {
		return nil, err
	}

	return mergeRoutes(sp.routes), nil
}

// GetEndpointsFromFiles parses the *.http file(s).
func GetEndpointsFromFiles(files []string, delay time.Duration) ([]*Endpoint, error) {
	sp := &parser{defaultDelay: delay}

	for _, fn := range files {
		err := sp.readFile(fn)
		if err != nil {
			return nil, err
		}
	}

	return mergeRoutes(sp.routes), nil
}
