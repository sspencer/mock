package mockhttp

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sspencer/mock/restclient"
)

type Server struct {
	methods        []restclient.Method
	logger         *slog.Logger
	events         []RequestEvent
	subscribers    map[chan RequestEvent]struct{}
	nextEventID    atomic.Uint64
	session        string
	clearedThrough uint64
	mu             sync.Mutex
	config         ConfigState
}

func New(methods []restclient.Method, logger *slog.Logger) *Server {
	s := &Server{
		methods:     cloneMethods(methods),
		session:     rand.Text(),
		logger:      logger,
		subscribers: make(map[chan RequestEvent]struct{}),
	}
	s.config = ConfigState{Revision: 1, RouteCount: len(methods), LastSuccess: time.Now().UTC().Format(time.RFC3339Nano)}
	warnMethodConfig(logger, methods)
	return s
}

// SetMethods replaces the mock routes served by this server.
func (s *Server) SetMethods(methods []restclient.Method) {
	compiled := cloneMethods(methods)
	warnMethodConfig(s.logger, compiled)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.methods = compiled
	s.config.Revision++
	s.config.RouteCount = len(compiled)
	s.config.LastSuccess = time.Now().UTC().Format(time.RFC3339Nano)
	s.config.Loading = false
	s.config.Error = ""
}

// Methods returns a snapshot of the currently configured mock routes.
func (s *Server) Methods() []restclient.Method {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMethods(s.methods)
}

// ClearEvents drops stored request-log events. Live SSE clients keep their
// connection but will not re-receive cleared history on a later reconnect snapshot.
func (s *Server) ClearEvents() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	arrivedAt := time.Now()
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Time{}) // Configured delays do not spend the write budget.
	requestBody, matchBody := readRequestBody(r)
	capture := newResponseCapture(w)

	method, values, match, ok := s.findMethod(r, matchBody)
	capture.match = match
	status := http.StatusNotFound
	if !ok {
		_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		http.NotFound(capture, r)
		status = capture.statusCode()
		s.logRequest(r, requestBody, capture, status, "", arrivedAt, time.Since(arrivedAt))
		return
	}
	if !s.delay(r.Context(), method) {
		return
	}
	_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
	filePath, hasFile := resolveFilePath(method)

	status = method.Status
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
			if !restclient.ValidHeaderName(name) || !restclient.ValidHeaderValue(value) {
				http.Error(capture, "mock: invalid rendered response header", http.StatusInternalServerError)
				s.logRequest(r, requestBody, capture, capture.statusCode(), method.Name, arrivedAt, time.Since(arrivedAt))
				return
			}
		}
		for _, value := range headerValues {
			capture.Header().Add(name, value)
		}
	}
	// Set authoritative metadata before committing headers; net/http may otherwise
	// infer headers which the request journal cannot observe.
	if capture.Header().Get("Date") == "" {
		capture.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	}
	if statusAllowsBody(status) {
		if capture.Header().Get("Content-Type") == "" && len(body) > 0 {
			capture.Header().Set("Content-Type", http.DetectContentType(body))
		}
		capture.Header().Del("Transfer-Encoding")
		capture.Header().Set("Content-Length", strconv.Itoa(len(body)))
	} else {
		capture.Header().Del("Content-Length")
		capture.Header().Del("Transfer-Encoding")
	}
	capture.WriteHeader(status)
	if r.Method != http.MethodHead && len(body) > 0 && statusAllowsBody(status) {
		_, _ = capture.Write(body)
	}

	s.logRequest(r, requestBody, capture, status, method.Name, arrivedAt, time.Since(arrivedAt))
}

func (s *Server) delay(ctx context.Context, method *restclient.Method) bool {
	delay := method.Delay
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
	checkedMethods := make([]restclient.Method, 0, len(methods))
	for _, method := range methods {
		if raw, ok := method.Variable("delay"); ok {
			if _, err := time.ParseDuration(raw); err != nil {
				logger.Warn("invalid @delay will be ignored", "delay", raw, "method", method.Name, "source", method.Source, "error", err)
			}
		}
		checked := method
		if path, ok := resolveFilePath(&method); ok && method.Body == "" {
			if body, err := readResponseFile(method); err == nil && fileTemplatesEnabled(method, path) && isMostlyText(body) {
				checked.Body = string(body)
			}
		}
		warnUnknownPlaceholders(logger, checked)
		checkedMethods = append(checkedMethods, checked)
		warnIncomingHeadersUsedAsResponse(logger, method)
	}
	warnShadowedRoutes(logger, methods)
	for _, unused := range restclient.UnusedVariables(checkedMethods) {
		logger.Warn("unused variable (not referenced as {{"+unused.Name+"}} in a body or response header)",
			"variable", "@"+unused.Name,
			"source", unused.Source,
			"line", unused.Line,
		)
	}
}

// warnShadowedRoutes warns about routes that can never be selected because an
// earlier route accepts every request they would. The first matching route
// wins, so such a route is dead configuration, often left over from files
// written for the removed response rotation.
func warnShadowedRoutes(logger *slog.Logger, methods []restclient.Method) {
	for j := range methods {
		for i := range j {
			if !shadows(methods[i], methods[j]) {
				continue
			}
			logger.Warn(fmt.Sprintf("route can never match: earlier route %q (%s:%d) matches every request it would; put more specific routes first", methods[i].Name, methods[i].Source, methods[i].Line),
				"method", methods[j].Name,
				"source", methods[j].Source,
				"line", methods[j].Line,
			)
			break
		}
	}
}

// shadows reports whether route a matches every request route b matches:
// they share method, port and path pattern, and each of a's constraints is
// also one of b's. It errs towards false; routes it misses still work.
func shadows(a, b restclient.Method) bool {
	if a.Method != b.Method || (a.Port != 0 && a.Port != b.Port) || routePattern(a) != routePattern(b) {
		return false
	}
	if a.MatchBody != "" && a.MatchBody != b.MatchBody {
		return false
	}
	for key, values := range a.Query {
		if len(b.Query[key]) < len(values) || !slices.Equal(values, b.Query[key][:len(values)]) {
			return false
		}
	}
	for key, values := range a.MatchHeaders {
		for _, value := range values {
			if !slices.Contains(b.MatchHeaders.Values(key), value) {
				return false
			}
		}
	}
	for key, values := range a.MatchQuery {
		for _, value := range values {
			if !slices.Contains(b.MatchQuery[key], value) {
				return false
			}
		}
	}
	return subsetMap(a.MatchPath, b.MatchPath) && subsetMap(a.MatchJSON, b.MatchJSON)
}

func subsetMap(a, b map[string]string) bool {
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}

// wellKnownIncomingHeaders are typical request headers. After the request line
// they are treated as response headers; warn unless a $header.* matcher is set.
var wellKnownIncomingHeaders = map[string]struct{}{
	"Accept":              {},
	"Accept-Charset":      {},
	"Accept-Encoding":     {},
	"Accept-Language":     {},
	"Authorization":       {},
	"Connection":          {},
	"Cookie":              {},
	"Expect":              {},
	"Forwarded":           {},
	"From":                {},
	"Host":                {},
	"If-Match":            {},
	"If-Modified-Since":   {},
	"If-None-Match":       {},
	"If-Range":            {},
	"If-Unmodified-Since": {},
	"Max-Forwards":        {},
	"Origin":              {},
	"Proxy-Authorization": {},
	"Range":               {},
	"Referer":             {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
	"User-Agent":          {},
	"Via":                 {},
	"X-Forwarded-For":     {},
	"X-Forwarded-Host":    {},
	"X-Forwarded-Proto":   {},
	"X-Real-Ip":           {},
	"X-Requested-With":    {},
}

func warnIncomingHeadersUsedAsResponse(logger *slog.Logger, method restclient.Method) {
	for name := range method.Headers {
		canonical := http.CanonicalHeaderKey(name)
		if _, ok := wellKnownIncomingHeaders[canonical]; !ok {
			continue
		}
		if method.MatchHeaders.Get(canonical) != "" {
			continue
		}
		logger.Warn("header after the request line is a response header; use @header."+canonical+" = value to match incoming requests",
			"header", canonical,
			"method", method.Name,
			"source", method.Source,
		)
	}
}

// cloneMethods gives the server ownership of all mutable route configuration.
func cloneMethods(methods []restclient.Method) []restclient.Method {
	out := append([]restclient.Method(nil), methods...)
	for i := range out {
		out[i].Status = http.StatusOK
		if methods[i].Status >= 200 && methods[i].Status <= 999 {
			out[i].Status = methods[i].Status
		}
		if raw, ok := methods[i].Variable("delay"); ok {
			out[i].Delay, _ = time.ParseDuration(raw)
		}
		out[i].Headers = methods[i].Headers.Clone()
		out[i].MatchHeaders = methods[i].MatchHeaders.Clone()
		out[i].MatchQuery = cloneValues(methods[i].MatchQuery)
		out[i].MatchPath = maps.Clone(methods[i].MatchPath)
		out[i].MatchJSON = maps.Clone(methods[i].MatchJSON)
		out[i].Comments = append([]string(nil), methods[i].Comments...)
		out[i].Variables = make(map[string]string, len(methods[i].Variables))
		for k, v := range methods[i].Variables {
			out[i].Variables[k] = v
		}
		out[i].VariableLines = maps.Clone(methods[i].VariableLines)
		out[i].Query = make(map[string][]string, len(methods[i].Query))
		for k, v := range methods[i].Query {
			out[i].Query[k] = append([]string(nil), v...)
		}
	}
	return out
}

func warnUnknownPlaceholders(logger *slog.Logger, method restclient.Method) {
	known := make(map[string]bool)
	for k := range method.Query {
		known[k] = true
	}
	for k, values := range method.MatchQuery {
		if len(values) > 0 && values[0] != "!" {
			known[k] = true
		}
	}
	for _, part := range strings.Split(method.Path, "/") {
		if strings.HasPrefix(part, ":") {
			known[part[1:]] = true
		}
	}
	texts := []string{method.Body}
	for _, values := range method.Headers {
		texts = append(texts, values...)
	}
	seen := make(map[string]bool)
	for _, text := range texts {
		for _, match := range restclient.PlaceholderPattern.FindAllStringSubmatch(text, -1) {
			prefix, key, args := match[1], match[2], strings.Fields(match[3])
			if seen[match[0]] {
				continue
			}
			if prefix == "$" && isFunction(key) {
				seen[match[0]] = true
				if _, err := callFunction(key, args, time.Now()); err != nil {
					logger.Warn("invalid placeholder arguments preserved literally", "placeholder", match[0], "error", err, "source", method.Source, "line", method.Line, "method", method.Name)
				}
				continue
			}
			if prefix == "$" {
				if known[key] || isGeneratedKey(key) {
					continue
				}
			} else if _, ok := method.Variables[key]; ok {
				continue
			}
			seen[match[0]] = true
			if prefix == "$" {
				logger.Warn("unresolved placeholder preserved literally (may be supplied by a request query)", "placeholder", key, "source", method.Source, "line", method.Line, "method", method.Name)
			} else {
				logger.Warn("unresolved placeholder preserved literally (no @"+key+" variable is defined)", "placeholder", key, "source", method.Source, "line", method.Line, "method", method.Name)
			}
		}
	}
}

func (s *Server) clearLocked() {
	s.events = nil
	s.clearedThrough = s.nextEventID.Add(1)
	event := RequestEvent{ID: s.clearedThrough, Session: s.session, Kind: "clear"}
	for subscriber := range s.subscribers {
		// Discard queued pre-clear traffic before publishing the clear boundary.
	drain:
		for {
			select {
			case <-subscriber:
			default:
				break drain
			}
		}
		subscriber <- event
	}
}
