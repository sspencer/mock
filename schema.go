package mock

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/julienschmidt/httprouter"
)

// Schema represents the mocked endpoint.
type Schema struct {
	Method      string
	Status      int
	Path        string
	ContentType string
	Response    []byte
}

func (s *Schema) String() string {
	return fmt.Sprintf(" %3d | %-4s %-28s | %-24s | %4d bytes", s.Status, s.Method, s.Path, s.ContentType, len(s.Response))
}

// Handler returns a HTTP handler method for the given schema.
func (s *Schema) Handler(logger responseLogger) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
		logger(s.Status, r)
		w.Header().Add("Content-Type", s.ContentType)
		w.WriteHeader(s.Status)

		if len(ps) > 0 {
			r := s.Response
			for i := range ps {
				key := fmt.Sprintf("{{%s}}", ps[i].Key)
				r = bytes.ReplaceAll(r, []byte(key), []byte(ps[i].Value))
			}

			w.Write(r)
		} else {
			w.Write(s.Response)
		}
	}
}
