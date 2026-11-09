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

// maxMatchBodyBytes is how much of a request body @body and @json matchers
// see. Larger bodies match no body matcher.
const maxMatchBodyBytes = 1024 * 1024

var truncatedBodyMarker = fmt.Sprintf("[body truncated after %d bytes]", maxLoggedBodyBytes)

type responseCapture struct {
	http.ResponseWriter
	status        int
	match         MatchInfo
	headers       http.Header
	body          strings.Builder
	bodyBytes     int
	bodyTruncated bool
	// fault is the @fault injected into this response, if any.
	fault string
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
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.status = status
	w.headers = w.Header().Clone()
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseCapture) Write(body []byte) (int, error) {
	if w.status == 0 {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(body))
		}
		w.WriteHeader(http.StatusOK)
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
	readError string
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

// readRequestBody reads up to maxMatchBodyBytes of the request body for
// matching and logging, and leaves r.Body readable from the start.
func readRequestBody(r *http.Request) (loggedBody, requestBody) {
	if r.Body == nil {
		return loggedBody{}, requestBody{}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMatchBodyBytes+1))
	readError := ""
	if err != nil {
		readError = err.Error()
	}
	r.Body = replayBody{
		Reader: io.MultiReader(bytes.NewReader(body), r.Body),
		Closer: r.Body,
	}
	match := requestBody{data: body, unavailable: err != nil || len(body) > maxMatchBodyBytes}
	if len(body) <= maxLoggedBodyBytes {
		return loggedBody{text: string(body), readError: readError}, match
	}
	return loggedBody{text: string(body[:maxLoggedBodyBytes]), truncated: true, readError: readError}, match
}

// logRequest records a handled request and returns its event ID.
func (s *Server) logRequest(r *http.Request, requestBody loggedBody, response *responseCapture, status int, methodName string, arrivedAt time.Time, elapsed time.Duration) uint64 {
	event := newRequestEvent(r, requestBody, response, status, arrivedAt, elapsed)
	resend := resendStateOf(r)
	if resend != nil {
		from := resend.from
		event.ResendOf = &from
	}
	id := s.publishRequest(s.redactEvent(event))
	if resend != nil {
		resend.id = id
	}
	// The hook gets the real request: -r turns it into routes, and a
	// "[redacted]" matcher or response would not replay what was recorded.
	if hook := s.requestHook.Load(); hook != nil {
		event.ID, event.Session = id, s.session
		(*hook)(event)
	}

	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	path := r.URL.RequestURI()
	if s.redact.Load() {
		path = redactURL(path)
	}
	attrs := []any{
		"method", r.Method,
		"path", path,
		"status", status,
		"route", methodName,
		"duration", elapsed,
	}
	if response.fault != "" {
		attrs = append(attrs, "fault", response.fault)
	}
	if upstream := response.match.Upstream; upstream != "" {
		attrs = append(attrs, "upstream", upstream)
	}
	logger.Info("http request", attrs...)
	return id
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
	if response.fault == "reset" {
		return "Connection reset by @fault = reset (no response was sent)"
	}
	fmt.Fprintf(&details, "%s %d %s\n", r.Proto, status, statusText(status))

	body := response.loggedBody()
	headers := response.sentHeaders()
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

// eventStatusText is the status text shown for an event, noting any @fault.
func eventStatusText(status int, fault string) string {
	switch fault {
	case "reset":
		return "Connection reset (@fault)"
	case "truncate":
		return statusText(status) + " (truncated by @fault)"
	}
	return statusText(status)
}

func statusText(status int) string {
	if text := http.StatusText(status); text != "" {
		return text
	}
	return "Status"
}

func (w *responseCapture) sentHeaders() http.Header {
	if w.headers != nil {
		return w.headers.Clone()
	}
	return w.Header().Clone()
}
