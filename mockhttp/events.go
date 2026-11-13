package mockhttp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultLogSize is how many request events the log keeps unless SetLimits
// changes it.
const DefaultLogSize = 1000

// Limits are the request body and request log sizes (see SetLimits).
type Limits struct {
	// MaxBody is how many bytes of a request body are read for body
	// matchers, templates and the log.
	MaxBody int64
	// LogSize is how many request events the log keeps.
	LogSize int
}

// SetLimits changes the request body and request log sizes. A zero field
// keeps the default (DefaultMaxBody, DefaultLogSize). A smaller log drops
// the oldest events.
func (s *Server) SetLimits(limits Limits) {
	s.maxBody.Store(limits.MaxBody)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logSize = limits.LogSize
	if size := s.logSizeLocked(); len(s.events) > size {
		s.events = slices.Clone(s.events[len(s.events)-size:])
	}
}

// Limits returns the request body and request log sizes in effect.
func (s *Server) Limits() Limits {
	limits := Limits{MaxBody: s.maxBody.Load(), LogSize: DefaultLogSize}
	if limits.MaxBody <= 0 {
		limits.MaxBody = DefaultMaxBody
	}
	s.mu.Lock()
	limits.LogSize = s.logSizeLocked()
	s.mu.Unlock()
	return limits
}

func (s *Server) logSizeLocked() int {
	if s.logSize > 0 {
		return s.logSize
	}
	return DefaultLogSize
}

type RequestEvent struct {
	Session  string        `json:"session"`
	Kind     string        `json:"kind,omitempty"`
	Reason   string        `json:"reason,omitempty"`
	ID       uint64        `json:"id"`
	Request  EventRequest  `json:"request"`
	Response EventResponse `json:"response"`
	Match    MatchInfo     `json:"match"`
	// Callback is set on an outbound @callback request; Match.Route is the
	// route that sent it.
	Callback *CallbackInfo `json:"callback,omitempty"`
	// ResendOf is set on a request replayed through POST /resend: the ID of
	// the event it was copied from, or 0 when it was not copied from one.
	ResendOf *uint64 `json:"resendOf,omitempty"`
}

type EventRequest struct {
	Method      string      `json:"method"`
	Scheme      string      `json:"scheme"`
	Host        string      `json:"host"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     http.Header `json:"headers"`
	Body        EventBody   `json:"body"`
	URL         string      `json:"url"`
	Time        string      `json:"time"`
	StartedAt   string      `json:"startedAt"`
	Details     string      `json:"details"`
}

type EventResponse struct {
	Status      int         `json:"status"`
	HTTPVersion string      `json:"httpVersion"`
	Headers     http.Header `json:"headers"`
	Body        EventBody   `json:"body"`
	StatusText  string      `json:"statusText"`
	Time        string      `json:"time"`
	ElapsedMs   int64       `json:"elapsedMs"`
	Details     string      `json:"details"`
	// Fault is the @fault injected into the response: "reset" (no response
	// was sent; Status is 0) or "truncate" (the body was cut short).
	Fault string `json:"fault,omitempty"`
}

func (s *Server) ServeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastID := parseLastEventID(r)
	s.mu.Lock()
	events, subscriber := s.subscribeLocked()
	session, cleared, latest := s.session, s.clearedThrough, s.nextEventID.Load()
	s.mu.Unlock()
	defer s.unsubscribe(subscriber)
	controller := http.NewResponseController(w)
	send := func(event RequestEvent) bool {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return writeEvent(w, event) && controller.Flush() == nil
	}
	raw := r.Header.Get("Last-Event-ID")
	previousSession, _, hasSession := strings.Cut(raw, "/")
	gap := lastID > latest || lastID < cleared || len(events) > 0 && lastID > 0 && lastID+1 < events[0].ID
	if raw != "" && (hasSession && previousSession != session || gap) {
		reason := "history-gap"
		if hasSession && previousSession != session {
			reason = "server-restarted"
		} else if lastID < cleared {
			reason = "history-cleared"
		}
		if !send(RequestEvent{Session: session, Kind: "reset", Reason: reason}) {
			return
		}
		lastID = 0
	}
	for _, event := range events {
		if event.ID <= lastID {
			continue
		}
		if !send(event) {
			return
		}
		lastID = event.ID
	}
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if controller.Flush() != nil {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case event, open := <-subscriber:
			if !open {
				return
			} // Reconnect and replay retained history after overflow.
			if event.ID <= lastID {
				continue
			}
			if !send(event) {
				return
			}
			lastID = event.ID
		case <-heartbeat.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// ServeClear handles POST to clear only the in-memory request log.
// Bare form-style POSTs are rejected; the dashboard sends X-Requested-With or JSON.
func (s *Server) ServeClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.mu.Lock()
	s.clearLocked()
	w.Header().Set("X-Mock-Cursor", strconv.FormatUint(s.clearedThrough, 10))
	w.Header().Set("X-Mock-Session", s.session)
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func clearRequestAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) || u.Scheme != requestScheme(r) {
			return false
		}
	}
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if site == "cross-site" || site == "same-site" {
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-Requested-With")) != "" {
		return true
	}
	ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";")
	return strings.EqualFold(strings.TrimSpace(ct), "application/json")
}

// ServeRoutes handles GET of the currently configured mock routes, or with
// ?tag=name only the routes with that @tag.
func (s *Server) ServeRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.serveRoutesWrite(w, r)
		return
	}
	routes := s.routeInfos(r.URL.Query().Get("tag"))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(routes)
}

// Routes describes every route, in the order they are tried, as GET /routes
// does: with hit counts, run-time switches and @times counts left.
func (s *Server) Routes() []RouteInfo {
	return s.routeInfos("")
}

// routeInfos describes the routes, or only those with tag when it is set.
func (s *Server) routeInfos(tag string) []RouteInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes := make([]RouteInfo, 0, len(s.methods))
	for i, method := range s.methods {
		if tag != "" && !method.HasTag(tag) {
			continue
		}
		route := describeRoute(method, i, s.config.Revision)
		route.Hits = s.hits[i].Load()
		route.Override = s.describeOverride(method)
		route.Switch = s.ownSwitch(method)
		route.TimesLeft = timesLeft(&method, s.uses[i])
		routes = append(routes, route)
	}
	return routes
}

func (s *Server) subscribe() ([]RequestEvent, chan RequestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.subscribeLocked()
}

func (s *Server) subscribeLocked() ([]RequestEvent, chan RequestEvent) {
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

func (s *Server) publishRequest(event RequestEvent) uint64 {
	journal := s.journal.Load()
	if journal == nil {
		return s.storeEvent(event).ID
	}
	// Journal lines follow event IDs: number and write one event at a time.
	s.journalMu.Lock()
	defer s.journalMu.Unlock()
	event = s.storeEvent(event)
	(*journal)(event)
	return event.ID
}

// storeEvent numbers event, adds it to the log and sends it to subscribers.
func (s *Server) storeEvent(event RequestEvent) RequestEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = s.nextEventID.Add(1)
	event.Session = s.session
	if size := s.logSizeLocked(); len(s.events) >= size {
		// Dropping from the front lets append reuse or regrow the array,
		// so a large log costs no copy per event.
		s.events = append(s.events[len(s.events)-size+1:], event)
	} else {
		s.events = append(s.events, event)
	}
	for subscriber := range s.subscribers {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(s.subscribers, subscriber)
		}
	}
	return event
}

func writeEvent(w io.Writer, event RequestEvent) bool {
	data, err := json.Marshal(event)
	if err != nil {
		return false
	}
	kind := ""
	if event.Kind != "" {
		kind = "event: " + event.Kind + "\n"
	}
	_, err = fmt.Fprintf(w, "id: %s/%d\n%sdata: %s\n\n", event.Session, event.ID, kind, data)
	return err == nil
}

func newRequestEvent(r *http.Request, requestBody loggedBody, response *responseCapture, status int, arrivedAt time.Time, elapsed time.Duration) RequestEvent {
	return RequestEvent{
		Match: response.match,
		Request: EventRequest{
			Method: r.Method,
			Scheme: requestScheme(r), Host: r.Host, HTTPVersion: r.Proto,
			Headers: requestEventHeaders(r), Body: eventBody(requestBody, requestBodySize(r, requestBody)),
			URL:       r.URL.RequestURI(),
			Time:      formatRequestTime(arrivedAt),
			StartedAt: arrivedAt.UTC().Format(time.RFC3339Nano),
			Details:   requestDetails(r, requestBody),
		},
		Response: EventResponse{
			Status:      status,
			HTTPVersion: r.Proto, Headers: response.sentHeaders(), Body: eventBody(response.loggedBody(), int64(response.bodyLength())),
			StatusText: eventStatusText(status, response.fault),
			Time:       elapsed.Round(time.Microsecond).String(),
			ElapsedMs:  elapsed.Milliseconds(),
			Details:    responseDetails(r, response, status),
			Fault:      response.fault,
		},
	}
}

func formatRequestTime(t time.Time) string {
	return t.Local().Format("15:04:05")
}

func parseLastEventID(r *http.Request) uint64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return 0
	}
	if _, after, ok := strings.Cut(raw, "/"); ok {
		raw = after
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// EventBody retains byte counts and binary data separately from display markers.
type EventBody struct {
	Text         string `json:"text"`
	Encoding     string `json:"encoding,omitempty"`
	Size         int64  `json:"size"`
	CapturedSize int    `json:"capturedSize"`
	Truncated    bool   `json:"truncated"`
	Error        string `json:"error,omitempty"`
}

func eventBody(body loggedBody, size int64) EventBody {
	out := EventBody{Text: body.text, Size: size, CapturedSize: len(body.text), Truncated: body.truncated, Error: body.readError}
	if !utf8.ValidString(body.text) || strings.ContainsRune(body.text, 0) {
		out.Text = base64.StdEncoding.EncodeToString([]byte(body.text))
		out.Encoding = "base64"
	}
	return out
}
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
func requestBodySize(r *http.Request, b loggedBody) int64 {
	if r.ContentLength >= 0 && r.Body != nil {
		return r.ContentLength
	}
	if b.truncated || b.readError != "" {
		return -1
	}
	return int64(len(b.text))
}
func requestEventHeaders(r *http.Request) http.Header {
	headers := r.Header.Clone()
	if r.Host != "" {
		headers.Set("Host", r.Host)
	}
	if r.ContentLength > 0 {
		headers.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
	}
	if len(r.TransferEncoding) > 0 {
		headers.Set("Transfer-Encoding", strings.Join(r.TransferEncoding, ", "))
	}
	return headers
}

// SetJournal calls journal with every event the request log records,
// requests and @callback sends alike, as published: numbered, and masked
// when SetRedact is on, in event ID order. Calls are serialized, in the
// request's goroutine and outside the server's lock, so a slow journal
// slows requests. A nil journal removes it.
func (s *Server) SetJournal(journal func(RequestEvent)) {
	if journal == nil {
		s.journal.Store(nil)
		return
	}
	s.journal.Store(&journal)
}

// SetRequestHook calls hook with the event of every request the server
// handles (not @callback requests), after it is logged, in the request's
// goroutine, so hook must be safe for concurrent use. The event is the real
// request and response, not masked by SetRedact. A nil hook removes it.
func (s *Server) SetRequestHook(hook func(RequestEvent)) {
	if hook == nil {
		s.requestHook.Store(nil)
		return
	}
	s.requestHook.Store(&hook)
}
