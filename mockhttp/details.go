package mockhttp

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxLoggedBodyBytes = 64 * 1024

var truncatedBodyMarker = fmt.Sprintf("[body truncated after %d bytes]", maxLoggedBodyBytes)

type responseCapture struct {
	http.ResponseWriter
	status        int
	body          strings.Builder
	bodyBytes     int
	bodyTruncated bool
}

func newResponseCapture(w http.ResponseWriter) *responseCapture {
	return &responseCapture{ResponseWriter: w}
}

// Unwrap exposes the underlying ResponseWriter for http.ResponseController and middleware.
func (w *responseCapture) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *responseCapture) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseCapture) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(body)
	if n > 0 {
		w.bodyBytes += n
		w.captureBody(body[:n])
	}
	return n, err
}

func (w *responseCapture) captureBody(body []byte) {
	remaining := maxLoggedBodyBytes - w.body.Len()
	if remaining <= 0 {
		w.bodyTruncated = true
		return
	}
	if len(body) > remaining {
		w.body.Write(body[:remaining])
		w.bodyTruncated = true
		return
	}
	w.body.Write(body)
}

func (w *responseCapture) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responseCapture) loggedBody() loggedBody {
	return loggedBody{text: w.body.String(), truncated: w.bodyTruncated}
}

func (w *responseCapture) bodyLength() int {
	return w.bodyBytes
}

type loggedBody struct {
	text      string
	truncated bool
}

func (b loggedBody) empty() bool {
	return b.text == "" && !b.truncated
}

func (b loggedBody) detailsText() string {
	if !b.truncated {
		return b.text
	}
	if b.text == "" {
		return truncatedBodyMarker
	}
	return b.text + "\n\n" + truncatedBodyMarker
}

type replayBody struct {
	io.Reader
	io.Closer
}

func readRequestBody(r *http.Request) loggedBody {
	if r.Body == nil {
		return loggedBody{}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxLoggedBodyBytes+1))
	if err != nil {
		r.Body = io.NopCloser(strings.NewReader(""))
		return loggedBody{}
	}
	r.Body = replayBody{
		Reader: io.MultiReader(bytes.NewReader(body), r.Body),
		Closer: r.Body,
	}
	if len(body) <= maxLoggedBodyBytes {
		return loggedBody{text: string(body)}
	}
	return loggedBody{text: string(body[:maxLoggedBodyBytes]), truncated: true}
}

func (s *Server) logRequest(r *http.Request, requestBody loggedBody, response *responseCapture, status int, methodName string, arrivedAt time.Time, elapsed time.Duration) {
	s.publishRequest(newRequestEvent(r, requestBody, response, status, arrivedAt, elapsed))

	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info(
		"http request",
		"method", r.Method,
		"path", r.URL.RequestURI(),
		"status", status,
		"mock", methodName,
		"duration", elapsed.String(),
	)
}

func (s *Server) logResponseRenderError(err error) {
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("failed to render mock response", "error", err)
}

func requestDetails(r *http.Request, body loggedBody) string {
	var details strings.Builder
	fmt.Fprintf(&details, "%s %s %s\n", r.Method, r.URL.RequestURI(), r.Proto)
	if r.Host != "" {
		fmt.Fprintf(&details, "Host: %s\n", r.Host)
	}

	headers := r.Header.Clone()
	if r.ContentLength > 0 && headers.Get("Content-Length") == "" {
		headers.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
	}
	if len(r.TransferEncoding) > 0 && headers.Get("Transfer-Encoding") == "" {
		headers.Set("Transfer-Encoding", strings.Join(r.TransferEncoding, ", "))
	}
	writeSortedHeaders(&details, headers)

	if !body.empty() {
		fmt.Fprintf(&details, "\n%s", body.detailsText())
	}
	return strings.TrimRight(details.String(), "\n")
}

func responseDetails(r *http.Request, response *responseCapture, status int) string {
	var details strings.Builder
	fmt.Fprintf(&details, "%s %d %s\n", r.Proto, status, statusText(status))

	body := response.loggedBody()
	headers := response.Header().Clone()
	if headers.Get("Date") == "" {
		headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}
	if response.bodyLength() > 0 && headers.Get("Content-Length") == "" {
		headers.Set("Content-Length", strconv.Itoa(response.bodyLength()))
	}
	writeSortedHeaders(&details, headers)

	if !body.empty() {
		fmt.Fprintf(&details, "\n%s", body.detailsText())
	}
	return strings.TrimRight(details.String(), "\n")
}

func writeSortedHeaders(details *strings.Builder, headers http.Header) {
	for _, name := range slices.Sorted(maps.Keys(headers)) {
		for _, value := range headers.Values(name) {
			fmt.Fprintf(details, "%s: %s\n", name, value)
		}
	}
}

func statusText(status int) string {
	if text := http.StatusText(status); text != "" {
		return text
	}
	return "Status"
}
