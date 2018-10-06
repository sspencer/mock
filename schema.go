package mock

import (
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
	//return fmt.Sprintf("%6s %3d %-28s %-26q [%4d bytes]", s.Method, s.Status, s.Path, s.ContentType, len(s.Response))
	return fmt.Sprintf(" %3d | %-4s %-28s | %-24s | %4d bytes", s.Status, s.Method, s.Path, s.ContentType, len(s.Response))
}

// Handler returns a HTTP handler method for the given schema.
func (s *Schema) Handler(logger responseLogger) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
		logger(s.Status, r)
		w.Header().Add("Content-Type", s.ContentType)
		w.WriteHeader(s.Status)
		w.Write(s.Response)
	}
}
