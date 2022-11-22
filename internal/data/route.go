package data

import (
	"crypto/sha256"
	"crypto/subtle"
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
	index     int
	responses []response
}

type response struct {
	auth   auth
	status int
	header map[string]string
	delay  time.Duration
	body   []byte
}

func (e *Endpoint) String() string {
	var b strings.Builder
	nr := len(e.responses)
	for i, resp := range e.responses {
		contentType, _ := resp.header["content-type"]
		fmt.Fprintf(&b, " %3d | %-6s %-28s | %-24s | %4d bytes | %s", resp.status, e.Method, e.Path, contentType, len(resp.body), resp.delay)
		if nr > 1 && i < nr-1 {
			fmt.Fprintln(&b)
		}
	}

	return b.String()
}

// Handle returns a HTTP handler method for the given endpoint.
func (e *Endpoint) Handle(logger colorlog.ResponseLoggerFunc) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		if e.index >= len(e.responses) {
			e.index = 0
		}

		resp := e.responses[e.index]

		logger(resp.status, r)

		if !isAuthorized(w, r, resp.auth) {
			return
		}

		for n, v := range resp.header {
			w.Header().Add(n, v)
		}
		w.WriteHeader(resp.status)

		if resp.delay > 0 {
			time.Sleep(resp.delay)
		}

		// replace {{params}} and {{variables}} in body
		out := substituteVars(replacerRegex.ReplaceAllFunc(resp.body, substituteParams(ps)))
		w.Write(out)

		e.index++
	}
}

func isAuthorized(w http.ResponseWriter, r *http.Request, auth auth) bool {
	if auth.authType != authTypeBasic {
		return true
	}

	username, password, ok := r.BasicAuth()

	if ok {
		usernameHash := sha256.Sum256([]byte(username))
		passwordHash := sha256.Sum256([]byte(password))

		expectedUsernameHash := sha256.Sum256([]byte(auth.username))
		expectedPasswordHash := sha256.Sum256([]byte(auth.password))

		usernameMatch := subtle.ConstantTimeCompare(usernameHash[:], expectedUsernameHash[:]) == 1
		passwordMatch := subtle.ConstantTimeCompare(passwordHash[:], expectedPasswordHash[:]) == 1

		if usernameMatch && passwordMatch {
			return true
		}
	}

	w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
	return false
}

// Combine duplicate routes (method/path) into an Endpoint with one or more responses
func mergeRoutes(apis []*route) []*Endpoint {
	var routes []*Endpoint
	m := make(map[string]*Endpoint)
	for _, t := range apis {
		key := fmt.Sprintf("%s:%s", t.method, t.path)
		resp := response{
			auth:   t.auth,
			status: t.status,
			body:   t.body,
			header: t.header,
			delay:  t.delay,
		}

		if route, ok := m[key]; ok {
			route.responses = append(route.responses, resp)
		} else {
			m[key] = &Endpoint{
				Method:    t.method,
				Path:      t.path,
				responses: []response{resp},
			}
		}
	}

	for _, s := range m {
		routes = append(routes, s)
	}

	return routes
}

// GetEndpointsFromReader parses a reader from <stdin> or similar
func GetEndpointsFromReader(r io.Reader) (routes []*Endpoint, err error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	sp := &parser{baseDir: dir}
	if err = sp.parse(r); err != nil {
		return nil, err
	}

	return mergeRoutes(sp.routes), nil
}

// GetEndpointsFromFiles parses the *.http file(s).
func GetEndpointsFromFiles(files []string) ([]*Endpoint, error) {
	sp := &parser{}

	for _, fn := range files {
		err := sp.readFile(fn)
		if err != nil {
			return nil, err
		}
	}

	return mergeRoutes(sp.routes), nil
}
