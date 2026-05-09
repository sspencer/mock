package mockhttp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
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

func New(methods []restclient.Method, logger *slog.Logger) *Server {
	return &Server{
		methods:     methods,
		logger:      logger,
		counters:    make(map[string]int),
		subscribers: make(map[chan RequestEvent]struct{}),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	arrivedAt := time.Now()
	requestBody := readRequestBody(r)
	capture := newResponseCapture(w)

	method, values, ok := s.findMethod(r)
	status := http.StatusNotFound
	if !ok {
		http.NotFound(capture, r)
		status = capture.statusCode()
		s.logRequest(r, requestBody, capture, status, "", arrivedAt, time.Since(arrivedAt))
		return
	}
	if !s.delay(r.Context(), method) {
		return
	}
	filePath, hasFile := resolveFilePath(method)

	status = statusFromVariables(method.Variables)
	body, err := renderBody(*method, values, filePath, hasFile)
	if err != nil {
		s.logResponseRenderError(err)
		http.Error(capture, "mock: failed to read response file", http.StatusInternalServerError)
		s.logRequest(r, requestBody, capture, capture.statusCode(), method.Name, arrivedAt, time.Since(arrivedAt))
		return
	}

	headers := responseHeaders(*method, filePath)
	for name, headerValues := range headers {
		for _, value := range headerValues {
			capture.Header().Add(name, value)
		}
	}
	capture.WriteHeader(status)
	if body != "" && statusAllowsBody(status) {
		_, _ = io.WriteString(capture, body)
	}

	s.logRequest(r, requestBody, capture, status, method.Name, arrivedAt, time.Since(arrivedAt))
}

func (s *Server) delay(ctx context.Context, method *restclient.Method) bool {
	raw, ok := method.Variables["delay"]
	if !ok {
		return true
	}
	delay, err := time.ParseDuration(raw)
	if err != nil || delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
