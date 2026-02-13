package data

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Endpoint represents a mocked route and can have one or more responses.
// It supports path variables, query parameters, and dynamic responses.
type Endpoint struct {
	Method     string
	Path       string
	index      int
	responses  []mockResponse
	localVars  map[string]mockResponse
	globalVars map[string]string
	sync.RWMutex
}

// mockResponse holds the details of a single response for an endpoint.
type mockResponse struct {
	status int
	header map[string]string
	delay  time.Duration
	body   []byte
}

// Handler is an interface for handling HTTP requests.
// It allows for pluggable response strategies (e.g., random, sequential).
type Handler interface {
	Handle(w http.ResponseWriter, r *http.Request)
}

// Endpoint implements the Handler interface.
// Handle processes the request and writes the appropriate response.
func (e *Endpoint) Handle(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()

	// First, check for GET parameter matches
	for key, values := range queryParams {
		value := values[rand.IntN(len(values))] // Assuming rand is imported
		getVar := getVarKey(key, value)

		// Use RLock for safe read access to localVars
		e.RLock()
		m, ok := e.localVars[getVar]
		e.RUnlock()

		if ok {
			e.writeHTTPResponse(w, r, e.Path, m, getVar)
			return
		}
	}

	// Otherwise, cycle through responses
	e.writeHTTPResponse(w, r, e.Path, e.getNextResponse(), "")
}

// getNextResponse returns the next response in a round-robin fashion.
// It uses a mutex for thread safety.
func (e *Endpoint) getNextResponse() mockResponse {
	e.Lock()
	defer e.Unlock()

	index := e.index % len(e.responses)
	response := e.responses[index]
	e.index++
	return response
}

// writeHTTPResponse writes the response to the HTTP writer.
// It handles delays, substitutions, and headers.
func (e *Endpoint) writeHTTPResponse(w http.ResponseWriter, r *http.Request, path string, resp mockResponse, getVar string) {
	if resp.delay > 0 {
		time.Sleep(resp.delay)
	}

	// Prepare substitution variables
	subVars := make(url.Values)

	// Use RLock to safely read globalVars
	e.RLock()
	for name, val := range e.globalVars {
		subVars[name] = []string{val}
	}
	e.RUnlock()

	// Add path variables
	items := strings.SplitSeq(path, "/")
	for item := range items {
		if strings.HasPrefix(item, "{") && strings.HasSuffix(item, "}") {
			key := item[1 : len(item)-1]
			value := chi.URLParam(r, key) // Assuming chi is imported
			subVars[key] = []string{value}
		}
	}

	// Add delay and query params
	subVars["delay"] = []string{resp.delay.String()}
	for k, v := range r.URL.Query() {
		arr := strings.SplitN(getVar, "=", 2)
		if len(arr) == 2 && arr[0] == k {
			subVars[k] = []string{arr[1]}
		} else {
			subVars[k] = v
		}
	}

	// Substitute headers
	for hdrName, hdrValue := range resp.header {
		w.Header().Add(hdrName, string(substitute(subVars, []byte(hdrValue))))
	}

	w.WriteHeader(resp.status)
	_, err := w.Write(substitute(subVars, resp.body))
	if err != nil {
		log.Println(err.Error()) // Assuming log is imported
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// merge combines duplicate routes into endpoints.
// It groups by method and path, handling multiple responses.
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
			// Deep copy globalVars for each endpoint to prevent shared state
			globalVarsCopy := make(map[string]string, len(globalVars))
			maps.Copy(globalVarsCopy, globalVars)

			m[key] = &Endpoint{
				Method:     t.method,
				Path:       t.path,
				responses:  make([]mockResponse, 0),
				localVars:  make(map[string]mockResponse),
				globalVars: globalVarsCopy,
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

// getVarKey creates a key for variable-based responses.
func getVarKey(key, value string) string {
	var buf bytes.Buffer
	buf.WriteString(key)
	buf.WriteString("=")
	buf.WriteString(value)
	return buf.String()
}

// GetEndpointsFromReader parses endpoints from an io.Reader.
// It gets the current directory for file references.
func GetEndpointsFromReader(r io.Reader) (routes []*Endpoint, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return getEndpoints(r, dir, "")
}

// GetEndpointsFromFile parses endpoints from a file.
// It uses the file's directory for relative paths.
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

// getEndpoints is a helper that parses and merges routes.
func getEndpoints(r io.Reader, dir, fn string) (routes []*Endpoint, err error) {
	p := NewParser(dir, fn) // Updated to use new interface
	if err := p.Parse(r); err != nil {
		return nil, err
	}
	return p.GetRoutes(), nil
}
