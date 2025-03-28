package data

import (
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"sync"
	"time"
)

var (
	// replace {{params}} in body
	replacerRegex = regexp.MustCompile(`\{\{[^}]+}}`)
)

// Endpoint represents the mocked route and can have one or more responses.
type Endpoint struct {
	Method     string
	Path       string
	index      int
	responses  []mockResponse
	localVars  map[string]mockResponse
	globalVars map[string]string
	sync.RWMutex
}

type mockResponse struct {
	status int
	header map[string]string
	delay  time.Duration
	body   []byte
}

// Combine duplicate routes (method/path) into an Endpoint with one or more responses
func merge(apis []*route, globalVars map[string]string) []*Endpoint {
	m := make(map[string]*Endpoint)

	for _, t := range apis {
		key := fmt.Sprintf("%s:%s", t.method, t.path)
		resp := mockResponse{
			status: t.status,
			body:   t.body,
			header: t.header,
			delay:  t.delay,
		}

		if _, ok := m[key]; !ok {
			m[key] = &Endpoint{
				Method:     t.method,
				Path:       t.path,
				responses:  make([]mockResponse, 0),
				localVars:  make(map[string]mockResponse),
				globalVars: globalVars,
			}
		}

		m[key].responses = append(m[key].responses, resp)
		varKey := getVarKey(t.uriKey, t.uriVal)
		if varKey != "" {
			m[key].localVars[varKey] = resp
		}
	}

	var routes []*Endpoint
	for _, endpoint := range m {
		routes = append(routes, endpoint)
	}

	return routes
}

func getVarKey(k, v string) string {
	if k == "" {
		return ""
	} else if v == "" {
		return k
	}

	return fmt.Sprintf("%s=%s", k, v)
}

// GetEndpointsFromReader parses a reader from <stdin> or similar
func GetEndpointsFromReader(r io.Reader) (routes []*Endpoint, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	return getEndpoints(r, dir, "")
}

// GetEndpointsFromFile parses the *.http file(s).
func GetEndpointsFromFile(fn string) ([]*Endpoint, error) {
	r, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := r.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	routes, err := getEndpoints(r, path.Dir(fn), fn)
	return routes, err
}

func getEndpoints(r io.Reader, dir, fn string) (routes []*Endpoint, err error) {
	p := newParser(dir, fn)
	if err := p.parse(r); err != nil {
		return nil, err
	}

	return merge(p.routes, p.globalVars), nil
}
