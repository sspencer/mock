package mock

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
)

// Route represents the mocked endpoint, with one or more responses.
type Route struct {
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

func (s *Route) String() string {
	var b strings.Builder
	nr := len(s.Responses)
	for i, resp := range s.Responses {
		contentType, _ := resp.Header["content-type"]
		fmt.Fprintf(&b, " %3d | %-6s %-28s | %-24s | %4d bytes | %s", resp.Status, s.Method, s.Path, contentType, len(resp.Body), resp.Delay)
		if nr > 1 && i < nr-1 {
			fmt.Fprintln(&b)
		}
	}

	return b.String()
}

// Handler returns a HTTP handler method for the given routes.
func (s *Route) Handler(logger responseLogger) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		if s.Index >= len(s.Responses) {
			s.Index = 0
		}

		resp := s.Responses[s.Index]

		logger(resp.Status, r)
		for n, v := range resp.Header {
			w.Header().Add(n, v)
		}
		w.WriteHeader(resp.Status)

		if resp.Delay > 0 {
			time.Sleep(resp.Delay)
		}

		// replace {{params}} in body
		if len(ps) > 0 {
			r := resp.Body
			for i := range ps {
				key := fmt.Sprintf("{{%s}}", ps[i].Key)
				r = bytes.ReplaceAll(r, []byte(key), []byte(ps[i].Value))
			}

			w.Write(r)
		} else {
			w.Write(resp.Body)
		}

		s.Index++
	}
}

// Combine duplicate routes (Method/Path) into a Route with one or more responses
func mergeRoutes(apis []*route) []*Route {
	var routes []*Route
	m := make(map[string]*Route)
	for _, t := range apis {
		key := fmt.Sprintf("%s:%s", t.Method, t.Path)
		resp := Response{
			Status: t.Status,
			Body:   t.Body,
			Header: t.Header,
		}

		if route, ok := m[key]; ok {
			route.Responses = append(route.Responses, resp)
		} else {
			m[key] = &Route{
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

func RoutesReader(r io.Reader, delay time.Duration) (routes []*Route, err error) {
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

// RoutesFiles parses the *.http file(s).
func RoutesFiles(files []string, delay time.Duration) ([]*Route, error) {
	sp := &parser{defaultDelay: delay}

	for _, fn := range files {
		err := sp.readFile(fn)
		if err != nil {
			return nil, err
		}
	}

	return mergeRoutes(sp.routes), nil
}
