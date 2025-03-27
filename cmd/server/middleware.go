package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

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

func (s *MockServer) ColorLogger(eventServer *EventServer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			respDetails := fmt.Sprintf("%s %d %s\nContent-Type: %s\nDate: %s\nContent-Length: %d\n\n%s\n",
				r.Proto,
				cw.StatusCode,
				http.StatusText(cw.StatusCode),
				cw.Header().Get("Content-Type"),
				now.UTC().Format("Mon, 02 Jan 2006 15:04:05 MST"),
				len(cw.Body.String()),
				cw.Body.String())

			data := colorlog.HTTPLog{
				Status:          cw.StatusCode,
				StatusText:      http.StatusText(cw.StatusCode),
				Time:            now.Format("15:04:05"),
				Method:          r.Method,
				Uri:             r.URL.String(),
				RequestDetails:  reqDetails,
				RequestBody:     reqBody,
				ResponseBody:    cw.Body.String(),
				ResponseDetails: respDetails,
				ResponseHeader:  cw.Header(),
			}

			jsonBody, _ := json.Marshal(data)

			eventServer.Broadcast(string(jsonBody))
			s.logger(data)
		})
	}
}
