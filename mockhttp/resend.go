package mockhttp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxResendBytes caps the JSON body of POST /resend.
const maxResendBytes = 2 << 20

// ResendRequest is the body of POST /resend: the request to replay, usually
// copied from a logged event and optionally edited.
type ResendRequest struct {
	// From is the ID of the event being replayed, shown on the new event.
	From   uint64 `json:"from,omitempty"`
	Method string `json:"method"`
	// URL is the request target: a path with an optional query.
	URL string `json:"url"`
	// Host is the Host header. Its port picks the mock port the request is
	// answered on; without one, routes on every port can match.
	Host    string      `json:"host,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Body    string      `json:"body,omitempty"`
	// Encoding is "base64" when Body is base64-encoded binary data.
	Encoding string `json:"encoding,omitempty"`
}

// resendKey carries a *resendState through a replayed request's context.
type resendKey struct{}

type resendState struct {
	from uint64
	// id is the replayed request's event ID, set by logRequest.
	id uint64
}

// ServeResend handles POST /resend: it answers the request in the body as
// if a client had sent it to the mock port (matching, delays, faults,
// --upstream, callbacks and the request log all apply) and responds with
// {"id": N}, the new event's ID. The admin UI cannot call the mock port
// itself because mock sends no CORS headers. It applies the same cross-site
// checks as /clear.
func (s *Server) ServeResend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var resend ResendRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxResendBytes))
	if err := decoder.Decode(&resend); err != nil {
		http.Error(w, "invalid resend request: "+err.Error(), http.StatusBadRequest)
		return
	}
	replay, state, err := resend.request(r, s.servesPort)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.serveReplay(replay)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if state.id == 0 {
		// The admin request was cancelled during a route's delay.
		http.Error(w, "the request was cancelled before a response was logged", http.StatusServiceUnavailable)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]uint64{"id": state.id})
}

// request builds the replayed request. It carries the admin request's
// context, so closing the admin connection cancels a route's delay. servesPort
// reports whether a route listens on a port.
func (resend ResendRequest) request(admin *http.Request, servesPort func(int) bool) (*http.Request, *resendState, error) {
	method := strings.ToUpper(strings.TrimSpace(resend.Method))
	if method == "" || strings.ContainsAny(method, " \t\r\n") {
		return nil, nil, errors.New("method is required and must be one word")
	}
	target := strings.TrimSpace(resend.URL)
	if !strings.HasPrefix(target, "/") {
		return nil, nil, errors.New("url must be a path such as /users?page=2")
	}
	parsed, err := url.ParseRequestURI(target)
	if err != nil {
		return nil, nil, errors.New("invalid url: " + err.Error())
	}
	body := []byte(resend.Body)
	switch resend.Encoding {
	case "":
	case "base64":
		if body, err = base64.StdEncoding.DecodeString(resend.Body); err != nil {
			return nil, nil, errors.New("invalid base64 body: " + err.Error())
		}
	default:
		return nil, nil, errors.New(`encoding must be empty or "base64"`)
	}
	host := strings.TrimSpace(resend.Host)
	if host == "" {
		host = "localhost"
	}

	state := &resendState{from: resend.From}
	ctx := context.WithValue(admin.Context(), resendKey{}, state)
	port := 0
	if _, rawPort, err := net.SplitHostPort(host); err == nil {
		port, _ = strconv.Atoi(rawPort)
	}
	if port > 0 && servesPort(port) {
		// The port the request is answered on decides which routes match.
		ctx = context.WithValue(ctx, http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	} else {
		// A Host with no port, or one no route listens on (the client went
		// through a proxy, or the port was edited), is answered as if it
		// arrived on every port, as an unbound in-process request would be.
		ctx = context.WithValue(ctx, http.LocalAddrContextKey, nil)
	}
	replay, err := http.NewRequestWithContext(ctx, method, "http://"+host+parsed.RequestURI(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("invalid request: " + err.Error())
	}
	// A server-side request's URL holds only the path and query.
	replay.URL = parsed
	replay.RemoteAddr = admin.RemoteAddr
	replay.Proto, replay.ProtoMajor, replay.ProtoMinor = "HTTP/1.1", 1, 1
	for name, values := range resend.Headers {
		switch http.CanonicalHeaderKey(name) {
		case "Host", "Content-Length", "Transfer-Encoding", "Connection":
			continue
		}
		for _, value := range values {
			replay.Header.Add(name, value)
		}
	}
	if len(body) == 0 {
		replay.Body = http.NoBody
	}
	return replay, state, nil
}

// servesPort reports whether a current route is served on port.
func (s *Server) servesPort(port int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.methods {
		if s.methods[i].Port == port {
			return true
		}
	}
	return false
}

// serveReplay answers a replayed request into a discarded response. Faults
// that drop the connection end the handler with a panic, which is expected
// here.
func (s *Server) serveReplay(r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil && recovered != http.ErrAbortHandler {
			panic(recovered)
		}
	}()
	s.ServeHTTP(&discardWriter{header: make(http.Header)}, r)
}

// discardWriter is the ResponseWriter of a replayed request. The response is
// read from the request log, not from here.
type discardWriter struct {
	header http.Header
}

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *discardWriter) WriteHeader(int)             {}
func (w *discardWriter) Flush()                      {}

// resendStateOf returns the replay state of r, or nil for a client request.
func resendStateOf(r *http.Request) *resendState {
	state, _ := r.Context().Value(resendKey{}).(*resendState)
	return state
}
