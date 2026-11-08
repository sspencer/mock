package mockhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type receivedCallback struct {
	method, path, contentType, via, body string
}

func callbackReceiver(t *testing.T, status int) (*httptest.Server, <-chan receivedCallback) {
	t.Helper()
	got := make(chan receivedCallback, 4)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- receivedCallback{r.Method, r.URL.RequestURI(), r.Header.Get("Content-Type"), r.Header.Get("Via"), string(body)}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, "thanks")
	}))
	t.Cleanup(receiver.Close)
	return receiver, got
}

func waitCallbackEvent(t *testing.T, s *Server) RequestEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if events := s.Requests(RequestFilter{Callbacks: true}); len(events) > 0 {
			return events[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no callback event recorded")
	return RequestEvent{}
}

func TestCallbackSendsRenderedRequest(t *testing.T) {
	receiver, got := callbackReceiver(t, http.StatusAccepted)
	s := newTestServer(t, `### [202] Pay
@callback = POST {{$json.callbackUrl}}
@callback.delay = 20ms
@callback.header.X-Signature = sig-{{$json.id}}
@callback.body = {"id": "{{$json.id}}", "status": "paid"}
POST /payments
Content-Type: application/json

{"id": "{{$json.id}}", "status": "pending"}
`)
	t.Cleanup(s.Close)
	start := time.Now()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/payments", strings.NewReader(`{"id":"p1","callbackUrl":"`+receiver.URL+`/hooks?x=1"}`))
	req.Header.Set("Content-Type", "application/json")
	s.ServeHTTP(rec, req)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"pending"`) {
		t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
	}
	var cb receivedCallback
	select {
	case cb = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("callback not received")
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Error("callback sent before its delay")
	}
	if cb.method != "POST" || cb.path != "/hooks?x=1" || cb.contentType != "application/json" || cb.body != `{"id": "p1", "status": "paid"}` || cb.via == "" {
		t.Errorf("callback = %+v", cb)
	}
	event := waitCallbackEvent(t, s)
	trigger := s.Requests(RequestFilter{})
	if len(trigger) != 1 || event.Callback.Trigger != trigger[0].ID || event.Match.Route == nil || event.Match.Route.Name != "Pay" {
		t.Errorf("event = %+v, requests = %d", event, len(trigger))
	}
	if event.Response.Status != 202 || event.Response.Body.Text != "thanks" || event.Request.Headers.Get("X-Signature") != "sig-p1" || event.Request.URL != "/hooks?x=1" {
		t.Errorf("event request/response = %+v / %+v", event.Request, event.Response)
	}
	if got := describeRoute(s.methods[0], 0, 0).Callback; got != "POST {{$json.callbackUrl}} after 20ms" {
		t.Errorf("route callback = %q", got)
	}
}

func TestCallbackFailuresAreLogged(t *testing.T) {
	old := callbackTimeout
	callbackTimeout = 50 * time.Millisecond
	t.Cleanup(func() { callbackTimeout = old })
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })

	for name, tc := range map[string]struct{ target, body, want string }{
		"timeout":    {slow.URL, `{}`, "timed out"},
		"unresolved": {"{{$json.missing}}", `{}`, "no value"},
		"bad scheme": {"{{$json.u}}", `{"u":"ftp://example.com"}`, "http or https"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t, "### Hook\n@callback = POST "+tc.target+"\nPOST /h\n\nok\n")
			t.Cleanup(s.Close)
			status, _ := serveBody(t, s, "/h", tc.body)
			if status != 200 {
				t.Fatalf("status = %d", status)
			}
			event := waitCallbackEvent(t, s)
			if event.Response.Status != 0 || !strings.Contains(event.Callback.Error, tc.want) {
				t.Errorf("event = %+v", event.Callback)
			}
		})
	}
}

func TestCallbackLoopIsNotSent(t *testing.T) {
	s := newTestServer(t, "### Hook\n@callback = POST http://127.0.0.1:1/x\nPOST /h\n\nok\n")
	t.Cleanup(s.Close)
	req := httptest.NewRequest("POST", "/h", nil)
	req.Header.Set("Via", "1.1 "+s.via())
	s.ServeHTTP(httptest.NewRecorder(), req)
	if event := waitCallbackEvent(t, s); !strings.Contains(event.Callback.Error, "loop") {
		t.Errorf("error = %q", event.Callback.Error)
	}
}

func TestCloseCancelsPendingCallbacks(t *testing.T) {
	receiver, got := callbackReceiver(t, 200)
	s := newTestServer(t, "### Hook\n@callback = POST "+receiver.URL+"\n@callback.delay = 1h\nPOST /h\n\nok\n")
	serveBody(t, s, "/h", "")
	done := make(chan struct{})
	var once sync.Once
	go func() { s.Close(); once.Do(func() { close(done) }) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the pending callback")
	}
	serveBody(t, s, "/h", "")
	select {
	case cb := <-got:
		t.Errorf("callback sent after Close: %+v", cb)
	case <-time.After(20 * time.Millisecond):
	}
	if events := s.Requests(RequestFilter{Callbacks: true}); len(events) != 0 {
		t.Errorf("callback events = %d", len(events))
	}
}
