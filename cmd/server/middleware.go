package main

import (
	"bytes"
	"net/http"
	"net/http/httputil"

	"github.com/sspencer/mock/internal/colorlog"
)

// ResponseCapturingWriter is a custom ResponseWriter that captures the response data.
type ResponseCapturingWriter struct {
	http.ResponseWriter
	StatusCode int
	Body       *bytes.Buffer
}

func (w *ResponseCapturingWriter) WriteHeader(statusCode int) {
	w.StatusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *ResponseCapturingWriter) Write(data []byte) (int, error) {
	if w.Body != nil {
		n, err := w.Body.Write(data)
		if err != nil {
			return n, err
		}
	}
	return w.ResponseWriter.Write(data)
}

func (s *MockServer) ColorLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cw := &ResponseCapturingWriter{
				ResponseWriter: w,
				StatusCode:     http.StatusOK,
				Body:           &bytes.Buffer{},
			}

			// Call the next handler with the capturing writer
			next.ServeHTTP(cw, r)

			body, err := httputil.DumpRequest(r, true)
			if err != nil {
				body = []byte("")
			}

			s.logger(colorlog.HTTPLog{
				Status:   cw.StatusCode,
				Method:   r.Method,
				Uri:      r.URL.String(),
				Body:     string(bytes.TrimSpace(body)),
				Response: cw.Body.String(),
				Header:   cw.Header(),
			})
		})
	}
}
