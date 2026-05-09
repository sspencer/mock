package mockhttp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	mathrand "math/rand"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"mock/restclient"
)

type Server struct {
	methods     []restclient.Method
	logger      *slog.Logger
	counters    map[string]int
	events      []RequestEvent
	subscribers map[chan RequestEvent]struct{}
	mu          sync.Mutex
}

var placeholderPattern = regexp.MustCompile(`\{\{\$([A-Za-z_][A-Za-z0-9_]*)\}\}`)

const maxRequestEvents = 200

type RequestEvent struct {
	Request  EventRequest  `json:"request"`
	Response EventResponse `json:"response"`
}

type EventRequest struct {
	Method  string `json:"method"`
	URL     string `json:"url"`
	Details string `json:"details"`
}

type EventResponse struct {
	Status     int    `json:"status"`
	StatusText string `json:"statusText"`
	Time       string `json:"time"`
	Details    string `json:"details"`
}

func New(methods []restclient.Method, logger *slog.Logger) *Server {
	return &Server{
		methods:     methods,
		logger:      logger,
		counters:    make(map[string]int),
		subscribers: make(map[chan RequestEvent]struct{}),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestBody := readRequestBody(r)
	capture := newResponseCapture(w)

	method, values, ok := s.findMethod(r)
	status := http.StatusNotFound
	if !ok {
		http.NotFound(capture, r)
		status = capture.statusCode()
		s.logRequest(r, requestBody, capture, status, "", time.Since(start))
		return
	}
	s.delay(method)
	filePath, _ := resolveFilePath(method)

	headers := responseHeaders(*method, filePath)
	for name, headerValues := range headers {
		for _, value := range headerValues {
			capture.Header().Add(name, value)
		}
	}
	status = statusFromVariables(method.Variables)
	body := renderBody(*method, values, filePath)
	capture.WriteHeader(status)
	if body != "" && statusAllowsBody(status) {
		_, _ = io.WriteString(capture, body)
	}

	s.logRequest(r, requestBody, capture, status, method.Name, time.Since(start))
}

func (s *Server) ServeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	events, subscriber := s.subscribe()
	defer s.unsubscribe(subscriber)

	for _, event := range events {
		if !writeEvent(w, event) {
			return
		}
	}
	flusher.Flush()

	for {
		select {
		case event := <-subscriber:
			if !writeEvent(w, event) {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) findMethod(r *http.Request) (*restclient.Method, map[string]string, bool) {
	type match struct {
		method *restclient.Method
		values map[string]string
	}

	var matches []match
	for i := range s.methods {
		method := &s.methods[i]
		if method.Method != r.Method {
			continue
		}
		values, ok := matchPath(method.Path, r.URL.Path)
		if !ok || !queryMatches(method.Query, r.URL.Query()) {
			continue
		}
		for name, queryValues := range r.URL.Query() {
			if len(queryValues) > 0 {
				values[name] = queryValues[0]
			}
		}
		matches = append(matches, match{method: method, values: values})
	}
	if len(matches) == 0 {
		return nil, nil, false
	}

	selected := s.nextMatch(r, len(matches))
	return matches[selected].method, matches[selected].values, true
}

func (s *Server) nextMatch(r *http.Request, count int) int {
	if count == 1 {
		return 0
	}
	key := r.Method + " " + r.URL.RequestURI()
	s.mu.Lock()
	defer s.mu.Unlock()

	selected := s.counters[key] % count
	s.counters[key]++
	return selected
}

func matchPath(pattern string, requestPath string) (map[string]string, bool) {
	if strings.HasSuffix(pattern, "/") && strings.TrimSuffix(requestPath, "index.html") == pattern {
		requestPath = pattern
	}
	patternParts := splitPath(pattern)
	requestParts := splitPath(requestPath)
	if len(patternParts) != len(requestParts) {
		return nil, false
	}

	values := make(map[string]string)
	for i := range patternParts {
		if key, ok := strings.CutPrefix(patternParts[i], ":"); ok {
			if key == "" {
				return nil, false
			}
			value, err := url.PathUnescape(requestParts[i])
			if err != nil {
				return nil, false
			}
			values[key] = value
			continue
		}
		if patternParts[i] != requestParts[i] {
			return nil, false
		}
	}
	return values, true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func queryMatches(expected url.Values, actual url.Values) bool {
	for key, expectedValues := range expected {
		actualValues, ok := actual[key]
		if !ok || len(actualValues) < len(expectedValues) {
			return false
		}
		for i, expectedValue := range expectedValues {
			if actualValues[i] != expectedValue {
				return false
			}
		}
	}
	return true
}

func statusFromVariables(variables map[string]string) int {
	raw, ok := variables["status"]
	if !ok {
		return http.StatusOK
	}
	status, err := strconv.Atoi(raw)
	if err != nil || status < 100 || status > 999 {
		return http.StatusOK
	}
	return status
}

func statusAllowsBody(status int) bool {
	return status != http.StatusNoContent && status != http.StatusNotModified && (status < 100 || status >= 200)
}

func (s *Server) delay(method *restclient.Method) {
	raw, ok := method.Variables["delay"]
	if !ok {
		return
	}
	delay, err := time.ParseDuration(raw)
	if err != nil || delay <= 0 {
		return
	}
	time.Sleep(delay)
}

func responseHeaders(method restclient.Method, filePath string) http.Header {
	headers := method.Headers.Clone()
	if headers.Get("Content-Type") != "" || method.Body != "" || filePath == "" {
		return headers
	}
	if contentType := mime.TypeByExtension(filepath.Ext(filePath)); contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	return headers
}

func renderBody(method restclient.Method, values map[string]string, filePath string) string {
	if method.Body == "" {
		if filePath != "" {
			body, err := os.ReadFile(filePath)
			if err == nil {
				return string(body)
			}
		}
		return ""
	}

	return placeholderPattern.ReplaceAllStringFunc(method.Body, func(match string) string {
		parts := placeholderPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		key := parts[1]
		if value, ok := values[key]; ok {
			return value
		}
		if value, ok := method.Variables[key]; ok {
			return value
		}
		return generatedValue(key)
	})
}

func resolveFilePath(method *restclient.Method) (string, bool) {
	raw, ok := method.Variables["file"]
	if !ok {
		return "", false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || filepath.IsAbs(raw) {
		return "", false
	}
	cleaned := filepath.Clean(raw)
	sep := string(filepath.Separator)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+sep) {
		return "", false
	}
	return filepath.Join(filepath.Dir(method.Source), cleaned), true
}

func generatedValue(key string) string {
	switch key {
	case "integer":
		return strconv.Itoa(mathrand.Intn(10000))
	case "float":
		return strconv.FormatFloat(math.Round(mathrand.Float64()*10000)/100, 'f', 2, 64)
	case "bool":
		if mathrand.Intn(2) == 0 {
			return "false"
		}
		return "true"
	case "uuid":
		return uuid()
	case "timestamp":
		return strconv.FormatInt(time.Now().Unix(), 10)
	case "isoTimestamp":
		return time.Now().UTC().Format(time.RFC3339)
	case "name":
		return "Alex Morgan"
	case "firstName":
		return "Alex"
	case "lastName":
		return "Morgan"
	case "phone":
		return "555-0100"
	case "user":
		return "amorgan"
	case "email":
		return "alex.morgan@example.com"
	case "url":
		return "https://example.com"
	case "server":
		return "mock"
	case "hash":
		return randomHex(16)
	case "sentence":
		return "This response was generated by mock."
	default:
		return ""
	}
}

func uuid() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

func randomHex(size int) string {
	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		return strings.Repeat("0", size*2)
	}
	return hex.EncodeToString(bytes)
}

type responseCapture struct {
	http.ResponseWriter
	status int
	body   strings.Builder
}

func newResponseCapture(w http.ResponseWriter) *responseCapture {
	return &responseCapture{ResponseWriter: w}
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
		w.body.Write(body[:n])
	}
	return n, err
}

func (w *responseCapture) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *responseCapture) bodyString() string {
	return w.body.String()
}

func readRequestBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		r.Body = io.NopCloser(strings.NewReader(""))
		return ""
	}
	s := string(body)
	r.Body = io.NopCloser(strings.NewReader(s))
	return s
}

func (s *Server) logRequest(r *http.Request, requestBody string, response *responseCapture, status int, methodName string, elapsed time.Duration) {
	s.publishRequest(newRequestEvent(r, requestBody, response, status, elapsed))

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

func (s *Server) subscribe() ([]RequestEvent, chan RequestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events := append([]RequestEvent(nil), s.events...)
	subscriber := make(chan RequestEvent, 16)
	s.subscribers[subscriber] = struct{}{}
	return events, subscriber
}

func (s *Server) unsubscribe(subscriber chan RequestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.subscribers, subscriber)
}

func (s *Server) publishRequest(event RequestEvent) {
	s.mu.Lock()
	if len(s.events) == maxRequestEvents {
		copy(s.events, s.events[1:])
		s.events[len(s.events)-1] = event
	} else {
		s.events = append(s.events, event)
	}

	subscribers := make([]chan RequestEvent, 0, len(s.subscribers))
	for subscriber := range s.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.Unlock()

	for _, subscriber := range subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func writeEvent(w io.Writer, event RequestEvent) bool {
	data, err := json.Marshal(event)
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err == nil
}

func newRequestEvent(r *http.Request, requestBody string, response *responseCapture, status int, elapsed time.Duration) RequestEvent {
	return RequestEvent{
		Request: EventRequest{
			Method:  r.Method,
			URL:     r.URL.RequestURI(),
			Details: requestDetails(r, requestBody),
		},
		Response: EventResponse{
			Status:     status,
			StatusText: statusText(status),
			Time:       elapsed.Round(time.Microsecond).String(),
			Details:    responseDetails(r, response, status),
		},
	}
}

func requestDetails(r *http.Request, body string) string {
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

	if body != "" {
		fmt.Fprintf(&details, "\n%s", body)
	}
	return strings.TrimRight(details.String(), "\n")
}

func responseDetails(r *http.Request, response *responseCapture, status int) string {
	var details strings.Builder
	fmt.Fprintf(&details, "%s %d %s\n", r.Proto, status, statusText(status))

	body := response.bodyString()
	headers := response.Header().Clone()
	if headers.Get("Date") == "" {
		headers.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}
	if body != "" && headers.Get("Content-Length") == "" {
		headers.Set("Content-Length", strconv.Itoa(len(body)))
	}
	writeSortedHeaders(&details, headers)

	if body != "" {
		fmt.Fprintf(&details, "\n%s", body)
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
