package mockhttp

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sspencer/mock/restclient"
)

type Server struct {
	methods     []restclient.Method
	logger      *slog.Logger
	counters    map[string]int
	events      []RequestEvent
	subscribers map[chan RequestEvent]struct{}
	nextEventID atomic.Uint64
	mu          sync.Mutex
}

func New(methods []restclient.Method, logger *slog.Logger) *Server {
	s := &Server{
		methods:     methods,
		logger:      logger,
		counters:    make(map[string]int),
		subscribers: make(map[chan RequestEvent]struct{}),
	}
	warnMethodConfig(logger, methods)
	return s
}

// SetMethods replaces the mock routes served by this server.
// Rotation counters are reset so duplicate routes start from the first match again.
func (s *Server) SetMethods(methods []restclient.Method) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods = methods
	s.counters = make(map[string]int)
	warnMethodConfig(s.logger, methods)
}

// Methods returns a snapshot of the currently configured mock routes.
func (s *Server) Methods() []restclient.Method {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]restclient.Method(nil), s.methods...)
}

// ClearEvents drops stored request-log events. Live SSE clients keep their
// connection but will not re-receive cleared history on a later reconnect snapshot.
func (s *Server) ClearEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
}

// ResetCounters resets duplicate-route rotation counters.
func (s *Server) ResetCounters() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counters = make(map[string]int)
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

	status = statusFromVariables(s.logger, method.Variables)
	body, err := renderBody(*method, values, filePath, hasFile)
	if err != nil {
		s.logResponseRenderError(err)
		http.Error(capture, "mock: failed to read response file", http.StatusInternalServerError)
		s.logRequest(r, requestBody, capture, capture.statusCode(), method.Name, arrivedAt, time.Since(arrivedAt))
		return
	}

	headers := responseHeaders(*method, values, filePath)
	for name, headerValues := range headers {
		for _, value := range headerValues {
			capture.Header().Add(name, value)
		}
	}
	capture.WriteHeader(status)
	if len(body) > 0 && statusAllowsBody(status) {
		_, _ = capture.Write(body)
	}

	s.logRequest(r, requestBody, capture, status, method.Name, arrivedAt, time.Since(arrivedAt))
}

func (s *Server) delay(ctx context.Context, method *restclient.Method) bool {
	raw, ok := method.Variables["delay"]
	if !ok {
		return true
	}
	delay, err := time.ParseDuration(raw)
	if err != nil {
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("ignoring invalid $delay", "delay", raw, "method", method.Name, "error", err)
		return true
	}
	if delay <= 0 {
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

func warnMethodConfig(logger *slog.Logger, methods []restclient.Method) {
	if logger == nil {
		logger = slog.Default()
	}
	for _, method := range methods {
		if raw, ok := method.Variables["status"]; ok {
			if _, err := parseStatusCode(raw); err != nil {
				logger.Warn("invalid $status will be treated as 200", "status", raw, "method", method.Name, "source", method.Source, "error", err)
			}
		}
		if raw, ok := method.Variables["delay"]; ok {
			if _, err := time.ParseDuration(raw); err != nil {
				logger.Warn("invalid $delay will be ignored", "delay", raw, "method", method.Name, "source", method.Source, "error", err)
			}
		}
		for _, name := range restclient.UnusedCustomVariables(method) {
			logger.Warn("unused custom variable (not referenced as {{$"+name+"}} in body or response headers)",
				"variable", "$"+name,
				"value", method.Variables[name],
				"method", method.Name,
				"source", method.Source,
			)
		}
	}
}
