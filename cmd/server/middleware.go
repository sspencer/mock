package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
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

func (s *mockServer) requestLogger(logger loggerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// short circuit upon seeing '/mock', the configurable
			// mount path for displaying web requests
			if strings.HasPrefix(r.URL.Path, s.logPath) {
				next.ServeHTTP(w, r)
				return
			}

			cw := &ResponseCapturingWriter{
				ResponseWriter: w,
				StatusCode:     http.StatusOK,
				Body:           &bytes.Buffer{},
			}

			// Call the next handler with the capturing writer
			next.ServeHTTP(cw, r)

			now := time.Now()
			body, err := httputil.DumpRequest(r, true)
			if err != nil {
				body = []byte("")
			}

			reqBody := ""
			reqDetails := string(bytes.TrimSpace(body))
			parts := strings.Split(string(body), "\r\n\r\n")
			if len(parts) > 1 {
				reqBody = parts[1]
			}

			respBody := cw.Body.String()
			respDetails := fmt.Sprintf("%s %d %s\nContent-Type: %s\nDate: %s\nContent-Length: %d\n\n%s\n",
				r.Proto,
				cw.StatusCode,
				http.StatusText(cw.StatusCode),
				cw.Header().Get("Content-Type"),
				now.UTC().Format("Mon, 02 Jan 2006 15:04:05 MST"),
				len(cw.Body.String()),
				respBody)

			data := httpLog{
				Request: httpRequestLog{
					Header:  r.Header,
					Method:  r.Method,
					URL:     r.URL.String(),
					Details: reqDetails,
					Body:    reqBody,
				},
				Response: httpResponseLog{
					Header:     cw.Header(),
					Status:     cw.StatusCode,
					StatusText: http.StatusText(cw.StatusCode),
					Time:       now.Format("15:04:05"),
					Details:    respDetails,
					Body:       respBody,
				},
			}

			jsonBody, _ := json.Marshal(data)

			s.broadcast(string(jsonBody))

			logger(data)
		})
	}
}
