package mockhttp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxRequestEvents = 1000

type RequestEvent struct {
	Session  string        `json:"session"`
	Kind     string        `json:"kind,omitempty"`
	Reason   string        `json:"reason,omitempty"`
	ID       uint64        `json:"id"`
	Request  EventRequest  `json:"request"`
	Response EventResponse `json:"response"`
	Match    MatchInfo     `json:"match"`
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

// ServeRoutes handles GET of the currently configured mock routes.
func (s *Server) ServeRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	routes := make([]RouteInfo, 0, len(s.methods))
	for i, method := range s.methods {
		routes = append(routes, describeRoute(method, i, s.config.Revision))
	}
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(routes)
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

func (s *Server) publishRequest(event RequestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event.ID = s.nextEventID.Add(1)
	event.Session = s.session
	if len(s.events) == maxRequestEvents {
		copy(s.events, s.events[1:])
		s.events[len(s.events)-1] = event
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
			StatusText: statusText(status),
			Time:       elapsed.Round(time.Microsecond).String(),
			ElapsedMs:  elapsed.Milliseconds(),
			Details:    responseDetails(r, response, status),
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
