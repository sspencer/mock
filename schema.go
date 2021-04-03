package mock

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
)

// Schema represents the mocked endpoint.
type Schema struct {
	Method    string
	Path      string
	Index     int
	Responses []Response
}

type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

func (s *Schema) String() string {
	var b strings.Builder
	nr := len(s.Responses)
	for i, resp := range s.Responses {
		fmt.Fprintf(&b, " %3d | %-6s %-28s | %-24s | %4d bytes", resp.Status, s.Method, s.Path, resp.ContentType, len(resp.Body))
		if nr > 1 && i < nr-1 {
			fmt.Fprintln(&b)
		}
	}

	return b.String()
}

// Handler returns a HTTP handler method for the given schema.
func (s *Schema) Handler(logger responseLogger) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		if s.Index >= len(s.Responses) {
			s.Index = 0
		}

		resp := s.Responses[s.Index]

		logger(resp.Status, r)
		w.Header().Add("Content-Type", resp.ContentType)
		w.WriteHeader(resp.Status)

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
