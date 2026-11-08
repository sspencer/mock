package mockhttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	mathrand "math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sspencer/mock/restclient"
)

type Server struct {
	methods []restclient.Method
	// hits counts the requests each route in methods answered.
	hits           []*atomic.Uint64
	unmatched      unmatchedLog
	logger         *slog.Logger
	events         []RequestEvent
	subscribers    map[chan RequestEvent]struct{}
	nextEventID    atomic.Uint64
	session        string
	clearedThrough uint64
	mu             sync.Mutex
	config         ConfigState
	upstream       atomic.Pointer[upstreamProxy]
	// defaults holds the --delay and --fault for routes that set none.
	defaults atomic.Pointer[RouteDefaults]
	// limiters holds the @rate-limit state of each route, by routeKey.
	limiters map[string]*rateLimiter
	limitMu  sync.Mutex
	// reloader re-reads the request files for POST /reload.
	reloader func()
	// callbacks tracks @callback requests in flight; callbackCtx is cancelled
	// by Close.
	callbacks       sync.WaitGroup
	callbackCtx     context.Context
	cancelCallbacks context.CancelFunc
	callbackMu      sync.Mutex
	closed          bool
}

func New(methods []restclient.Method, logger *slog.Logger) *Server {
	s := &Server{
		methods:     cloneMethods(methods),
		hits:        carryHits(methods, nil, nil),
		session:     rand.Text(),
		logger:      logger,
		subscribers: make(map[chan RequestEvent]struct{}),
	}
	s.callbackCtx, s.cancelCallbacks = context.WithCancel(context.Background())
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
	s.hits = carryHits(compiled, s.methods, s.hits)
	s.pruneLimiters(compiled)
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
		up := s.upstream.Load()
		s.recordUnmatched(r, up != nil)
		if up != nil {
			capture.match.Upstream = up.target.String()
			s.serveUpstream(up, capture, r)
		} else {
			http.NotFound(capture, r)
		}
		status = capture.statusCode()
		s.logRequest(r, requestBody, capture, status, "", arrivedAt, time.Since(arrivedAt))
		return
	}
	method = s.withDefaults(method)
	if s.overRateLimit(capture, method) {
		s.logRequest(r, requestBody, capture, capture.statusCode(), method.Name, arrivedAt, time.Since(arrivedAt))
		return
	}
	if !s.delay(r.Context(), method) {
		return
	}
	_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))

	status = method.Status
	render := newRenderer(method, values)
	if method.Proxy != "" {
		target, exact, err := proxyTarget(method, render)
		if err != nil {
			s.logResponseRenderError(err)
			http.Error(capture, "mock: "+err.Error(), http.StatusBadGateway)
		} else {
			capture.match.Upstream = target.String()
			s.serveRouteProxy(capture, r, target, exact)
		}
		s.logRequest(r, requestBody, capture, capture.statusCode(), method.Name, arrivedAt, time.Since(arrivedAt))
		return
	}
	file, hasFile, err := render.file()
	var body []byte
	var headers http.Header
	if err == nil {
		body, err = render.body(file, hasFile)
	}
	if err == nil {
		headers, err = render.headers(file)
	}
	if err != nil {
		switch {
		case errors.Is(err, errNoResponseFile):
			http.Error(capture, "mock: no response file for this request", http.StatusNotFound)
		case errors.Is(err, errRenderedTooLarge):
			s.logResponseRenderError(err)
			http.Error(capture, "mock: rendered response is too large", http.StatusInternalServerError)
		default:
			s.logResponseRenderError(err)
			http.Error(capture, "mock: failed to read response file", http.StatusInternalServerError)
		}
		s.logRequest(r, requestBody, capture, capture.statusCode(), method.Name, arrivedAt, time.Since(arrivedAt))
		return
	}

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
	if method.Compress == "auto" && method.Stream == 0 {
		capture.Header().Add("Vary", "Accept-Encoding")
	}
	if encoding := responseEncoding(method, r); encoding != "" && method.Stream == 0 && len(body) > 0 && statusAllowsBody(status) && capture.Header().Get("Content-Encoding") == "" {
		// Type the body before it is compressed, or sniffing sees gzip bytes.
		if capture.Header().Get("Content-Type") == "" {
			capture.Header().Set("Content-Type", http.DetectContentType(body))
		}
		body = compressBody(encoding, body)
		capture.Header().Set("Content-Encoding", encoding)
		render.verbatim = false // Ranges would apply to the compressed bytes.
	}
	if render.verbatim && status == http.StatusOK && method.Stream == 0 && method.Fault == "" &&
		(r.Method == http.MethodGet || r.Method == http.MethodHead) {
		s.serveFileContent(capture, r, file.path, body)
		id := s.logRequest(r, requestBody, capture, capture.statusCode(), method.Name, arrivedAt, time.Since(arrivedAt))
		s.scheduleCallback(r, method, render, id, capture.match)
		return
	}
	sendsBody := r.Method != http.MethodHead && len(body) > 0 && statusAllowsBody(status)
	fault := chooseFault(method, sendsBody)
	if fault == "reset" {
		// Log first: the handler does not return normally after a reset.
		capture.fault = fault
		capture.headers = http.Header{}
		id := s.logRequest(r, requestBody, capture, 0, method.Name, arrivedAt, time.Since(arrivedAt))
		s.scheduleCallback(r, method, render, id, capture.match)
		resetConnection(capture)
		return
	}
	streaming := method.Stream > 0 && sendsBody
	if statusAllowsBody(status) {
		if capture.Header().Get("Content-Type") == "" && len(body) > 0 {
			capture.Header().Set("Content-Type", http.DetectContentType(body))
		}
		capture.Header().Del("Transfer-Encoding")
		if streaming {
			capture.Header().Del("Content-Length")
		} else {
			capture.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
	} else {
		capture.Header().Del("Content-Length")
		capture.Header().Del("Transfer-Encoding")
	}
	if fault == "truncate" {
		// Send half the body, then drop the connection; with Content-Length
		// set to the full size the client sees a short body.
		capture.fault = fault
		body = body[:len(body)/2]
	}
	capture.WriteHeader(status)
	switch {
	case streaming:
		s.stream(r.Context(), controller, capture, body, method)
	case sendsBody:
		_, _ = capture.Write(body)
	}

	id := s.logRequest(r, requestBody, capture, status, method.Name, arrivedAt, time.Since(arrivedAt))
	s.scheduleCallback(r, method, render, id, capture.match)
	if fault == "truncate" {
		_ = controller.Flush()
		panic(http.ErrAbortHandler)
	}
}

// chooseFault picks the @fault to inject for this response, if any. A
// truncate fault needs a body to cut short.
func chooseFault(method *restclient.Method, sendsBody bool) string {
	if method.Fault == "" || method.Fault == "truncate" && !sendsBody {
		return ""
	}
	if method.FaultRate < 1 && mathrand.Float64() >= method.FaultRate {
		return ""
	}
	return method.Fault
}

// resetConnection drops the client connection without a response. On TCP it
// sends a reset (RST) rather than a clean close.
func resetConnection(w http.ResponseWriter) {
	conn, _, err := http.NewResponseController(w).Hijack()
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = conn.Close()
}

// stream writes body one chunk at a time, pausing method.Stream between
// chunks. Each write gets a fresh write deadline.
func (s *Server) stream(ctx context.Context, controller *http.ResponseController, w http.ResponseWriter, body []byte, method *restclient.Method) {
	sse := responseContentType(*method, "") == "text/event-stream" || strings.HasPrefix(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream")
	for i, chunk := range streamChunks(body, sse) {
		if i > 0 && !sleep(ctx, method.Stream) {
			return
		}
		_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := w.Write(chunk); err != nil {
			return
		}
		if controller.Flush() != nil {
			return
		}
	}
}

// streamChunks splits a body into lines, or into events (ending at a blank
// line) for server-sent events. Line endings stay with their chunk.
func streamChunks(body []byte, sse bool) [][]byte {
	var chunks [][]byte
	for len(body) > 0 {
		end := len(body)
		if sse {
			if i := bytes.Index(body, []byte("\n\n")); i >= 0 {
				end = i + 2
			}
			if i := bytes.Index(body, []byte("\r\n\r\n")); i >= 0 && i+4 < end {
				end = i + 4
			}
		} else if i := bytes.IndexByte(body, '\n'); i >= 0 {
			end = i + 1
		}
		chunks = append(chunks, body[:end])
		body = body[end:]
	}
	return chunks
}

func (s *Server) delay(ctx context.Context, method *restclient.Method) bool {
	delay := method.Delay
	if method.DelayMax > delay {
		delay += time.Duration(mathrand.Int64N(int64(method.DelayMax-delay) + 1))
	}
	return sleep(ctx, delay)
}

// sleep waits for d, returning false if ctx ends first.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
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
			if _, _, err := restclient.ParseDelay(raw); err != nil {
				logger.Warn("invalid @delay will be ignored", "delay", raw, "method", method.Name, "source", method.Source, "error", err)
			}
		}
		if method.Compress != "" && method.Stream > 0 {
			logger.Warn("@compress is ignored for a section with @stream, which sends its body in readable pieces", "method", method.Name, "source", method.Source, "line", method.Line)
		}
		var fixture any
		if method.Fixture != "" {
			var err error
			if fixture, err = loadFixture(&method); err != nil {
				logger.Warn("{{$fixture.*}} placeholders will stay literal", "method", method.Name, "source", method.Source, "line", method.Line, "error", err)
			}
		}
		if method.Proxy != "" && method.Callback != nil {
			logger.Warn("@callback is ignored for a section with @proxy", "method", method.Name, "source", method.Source, "line", method.Line)
		}
		if method.Proxy != "" && (method.Body != "" || method.File != "" || len(method.Headers) > 0 || method.Stream > 0 || method.Fault != "") {
			logger.Warn("@proxy forwards the request, so this section's response body, file, headers, @stream and @fault are ignored", "method", method.Name, "source", method.Source, "line", method.Line)
		}
		checked := method
		if method.File != "" && method.Body == "" && !restclient.FileIsTemplate(method.File) {
			path := restclient.ResolveFile(method.Source, method.File)
			if body, err := readResponseFile(method.Source, method.File); err == nil && fileIsTemplated(method, path, body) {
				checked.Body = string(body)
			}
		}
		warnUnknownPlaceholders(logger, checked, fixture)
		checkedMethods = append(checkedMethods, checked)
		warnIncomingHeadersUsedAsResponse(logger, method)
		warnInvalidJSONBody(logger, method)
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
// they share method and port, a's path pattern covers b's (see
// pathCovers), and each of a's constraints is also one of b's. It errs
// towards false; routes it misses still work.
func shadows(a, b restclient.Method) bool {
	if a.Method != b.Method || (a.Port != 0 && a.Port != b.Port) {
		return false
	}
	segments, ok := pathCovers(routePattern(a), routePattern(b))
	if !ok {
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
	for _, pair := range [][2]url.Values{{a.MatchQuery, b.MatchQuery}, {a.MatchForm, b.MatchForm}, {a.MatchCookies, b.MatchCookies}} {
		for key, values := range pair[0] {
			for _, value := range values {
				if !slices.Contains(pair[1][key], value) {
					return false
				}
			}
		}
	}
	for name, raw := range a.MatchPath {
		segment, ok := segments[name]
		if !ok {
			return false
		}
		if param, isParam := strings.CutPrefix(segment, ":"); isParam {
			// b's parameter must carry the same constraint, under any name.
			if b.MatchPath[param] != raw {
				return false
			}
		} else if literal, err := url.PathUnescape(segment); err != nil || !restclient.MustMatcher(raw).Match([]string{literal}) {
			return false
		}
	}
	return subsetMap(a.MatchJSON, b.MatchJSON) && subsetMap(a.MatchXML, b.MatchXML)
}

// pathCovers reports whether every request path that pattern b matches is
// also matched by pattern a, comparing escaped patterns segment by segment:
// a literal covers only the same literal, a :param covers any one segment, and
// a trailing * covers the rest of the path, including nothing. It returns, for
// each of a's :params, the segment of b it lines up with (a literal, or b's
// own ":name"). A *name wildcard in a is not in the map.
func pathCovers(a, b string) (map[string]string, bool) {
	aParts, bParts := splitPath(a), splitPath(b)
	aWild := len(aParts) > 0 && strings.HasPrefix(aParts[len(aParts)-1], "*")
	bWild := len(bParts) > 0 && strings.HasPrefix(bParts[len(bParts)-1], "*")
	if aWild {
		aParts = aParts[:len(aParts)-1]
	}
	if bWild {
		bParts = bParts[:len(bParts)-1]
	}
	switch {
	case aWild && len(bParts) < len(aParts):
		return nil, false
	case !aWild && (bWild || len(aParts) != len(bParts)):
		return nil, false
	}
	// A pattern ending in "/" also answers ".../index.html", which a pattern
	// without the slash does not.
	if strings.HasSuffix(b, "/") && !strings.HasSuffix(a, "/") && !aWild {
		return nil, false
	}
	segments := make(map[string]string)
	for i, part := range aParts {
		if name, ok := strings.CutPrefix(part, ":"); ok {
			if name == "" {
				return nil, false
			}
			segments[name] = bParts[i]
			continue
		}
		if strings.HasPrefix(bParts[i], ":") {
			return nil, false
		}
		want, errA := url.PathUnescape(part)
		got, errB := url.PathUnescape(bParts[i])
		if errA != nil || errB != nil || want != got {
			return nil, false
		}
	}
	return segments, true
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
			out[i].Delay, out[i].DelayMax, _ = restclient.ParseDelay(raw)
		}
		out[i].Headers = methods[i].Headers.Clone()
		out[i].MatchHeaders = methods[i].MatchHeaders.Clone()
		out[i].MatchQuery = cloneValues(methods[i].MatchQuery)
		out[i].MatchPath = maps.Clone(methods[i].MatchPath)
		out[i].MatchJSON = maps.Clone(methods[i].MatchJSON)
		out[i].MatchXML = maps.Clone(methods[i].MatchXML)
		out[i].MatchForm = cloneValues(methods[i].MatchForm)
		out[i].MatchCookies = cloneValues(methods[i].MatchCookies)
		out[i].Comments = append([]string(nil), methods[i].Comments...)
		out[i].Variables = make(map[string]string, len(methods[i].Variables))
		for k, v := range methods[i].Variables {
			out[i].Variables[k] = v
		}
		out[i].VariableLines = maps.Clone(methods[i].VariableLines)
		if c := methods[i].Callback; c != nil {
			callback := *c
			callback.Headers = c.Headers.Clone()
			out[i].Callback = &callback
		}
		out[i].Query = make(map[string][]string, len(methods[i].Query))
		for k, v := range methods[i].Query {
			out[i].Query[k] = append([]string(nil), v...)
		}
	}
	return out
}

// fixture is the route's decoded @fixture file, or nil when it has none or
// it failed to load.
func warnUnknownPlaceholders(logger *slog.Logger, method restclient.Method, fixture any) {
	if method.Template == "off" {
		return
	}
	known := make(map[string]bool)
	for k := range method.Query {
		known[k] = true
	}
	for k, values := range method.MatchQuery {
		if len(values) > 0 && values[0] != "!" {
			known[k] = true
		}
	}
	pathParams := make(map[string]bool)
	for _, name := range restclient.PathParams(method.EscapedPath) {
		known[name] = true
		pathParams[name] = true
	}
	for _, raw := range matcherValues(method) {
		for _, name := range restclient.CaptureNames(raw) {
			known[name] = true
		}
	}
	texts := []string{method.Body, method.File}
	for _, values := range method.Headers {
		texts = append(texts, values...)
	}
	seen := make(map[string]bool)
	warn := func(message, placeholder string, args ...any) {
		if seen[message+placeholder] {
			return
		}
		seen[message+placeholder] = true
		logger.Warn(message, append([]any{"placeholder", placeholder, "source", method.Source, "line", method.Line, "method", method.Name}, args...)...)
	}
	var check func(nodes []templateNode, inRepeat bool)
	check = func(nodes []templateNode, inRepeat bool) {
		for _, node := range nodes {
			switch node.kind {
			case repeatNode:
				check(node.children, true)
				continue
			case eachNode:
				check(node.children, true)
				continue
			case textNode:
				continue
			}
			if node.hasFilter("default") {
				continue
			}
			key := node.name
			if node.prefix != "$" {
				if _, ok := method.Variables[key]; !ok {
					warn("unresolved placeholder preserved literally (no @"+key+" variable is defined)", key)
				}
				continue
			}
			if isFunction(key) {
				if _, err := callFunction(key, node.args, time.Now(), nil); err != nil {
					warn("invalid placeholder arguments preserved literally", node.text, "error", err)
				}
				continue
			}
			if name, ok := strings.CutPrefix(key, "path."); ok {
				if !pathParams[name] {
					warn("unresolved placeholder preserved literally (the route path has no :"+name+" parameter)", key)
				}
				continue
			}
			if path, ok := fixturePath(key); ok {
				switch {
				case method.Fixture == "":
					warn("unresolved placeholder preserved literally (no @fixture file is set)", key)
				case fixture != nil && len(selectJSON(fixture, path)) == 0:
					warn("unresolved placeholder preserved literally (@fixture "+method.Fixture+" has no value at "+path+")", key)
				}
				continue
			}
			if name, ok := strings.CutPrefix(key, "env."); ok && name != "" {
				if _, set := os.LookupEnv(name); !set {
					warn("unresolved placeholder preserved literally (environment variable "+name+" is not set)", key)
				}
				continue
			}
			if key == "index" && inRepeat {
				continue
			}
			if _, ok := itemPath(key); ok && inRepeat {
				continue
			}
			if known[key] || isGeneratedKey(key) || isRequestPlaceholder(key) {
				continue
			}
			warn("unresolved placeholder preserved literally (may be supplied by a request query)", key)
		}
	}
	for _, text := range texts {
		nodes, problems := parseTemplate(text)
		for _, problem := range problems {
			warn("template problem preserved literally: "+problem.message, problem.text)
		}
		check(nodes, false)
	}
}

func (n templateNode) hasFilter(name string) bool {
	for _, filter := range n.filters {
		if filter.name == name {
			return true
		}
	}
	return false
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

// isRequestPlaceholder reports whether key reads the request at render time
// ($query.name, $header.Name, $cookie.name, $json.field.path, $xml.path or $form.name). Its value
// depends on each request, so it is never reported as unknown.
func isRequestPlaceholder(key string) bool {
	if _, ok := requestEchoPath(key); ok {
		return true
	}
	for _, prefix := range []string{"query.", "header.", "cookie.", "json.", "xml.", "form."} {
		if name, ok := strings.CutPrefix(key, prefix); ok && name != "" {
			return true
		}
	}
	return false
}

// matcherValues returns every raw matcher value of a method, for scanning
// regexp capture names. @body values are adjusted as at match time.
func matcherValues(method restclient.Method) []string {
	var out []string
	for _, values := range method.MatchHeaders {
		out = append(out, values...)
	}
	for _, values := range method.MatchQuery {
		out = append(out, values...)
	}
	for _, values := range method.MatchForm {
		out = append(out, values...)
	}
	for _, values := range method.MatchCookies {
		out = append(out, values...)
	}
	for _, value := range method.MatchPath {
		out = append(out, value)
	}
	for _, value := range method.MatchJSON {
		out = append(out, value)
	}
	for _, value := range method.MatchXML {
		out = append(out, value)
	}
	if method.MatchBody != "" {
		out = append(out, method.MatchBody)
	}
	return out
}

// serveFileContent sends an unchanged response file with http.ServeContent,
// which answers Range, If-Range and If-Modified-Since requests (206, 416 and
// 304) and sets Accept-Ranges and Last-Modified from the file.
func (s *Server) serveFileContent(w http.ResponseWriter, r *http.Request, path string, body []byte) {
	var modTime time.Time
	if info, err := os.Stat(path); err == nil {
		modTime = info.ModTime()
	}
	w.Header().Del("Content-Length")
	w.Header().Del("Transfer-Encoding")
	if w.Header().Get("Content-Type") == "" && len(body) > 0 {
		w.Header().Set("Content-Type", http.DetectContentType(body))
	}
	http.ServeContent(w, r, "", modTime, bytes.NewReader(body))
}
