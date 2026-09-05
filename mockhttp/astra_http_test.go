package mockhttp

import (
	"github.com/sspencer/mock/restclient"
	"net/http/httptest"
	"testing"
	"time"
)

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineRecorder) SetWriteDeadline(d time.Time) error {
	w.deadlines = append(w.deadlines, d)
	return nil
}
func TestWriteBudgetStartsAfterConfiguredDelay(t *testing.T) {
	s := New([]restclient.Method{{Method: "GET", Path: "/", Body: "ok", Variables: map[string]string{"delay": "10ms"}}}, nil)
	w := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	s.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if len(w.deadlines) != 2 || !w.deadlines[0].IsZero() || time.Until(w.deadlines[1]) < 29*time.Second {
		t.Fatalf("bad write budget: %v", w.deadlines)
	}
}
func TestClearRejectsCrossOriginEvenWithCustomHeader(t *testing.T) {
	r := httptest.NewRequest("POST", "http://localhost/clear", nil)
	r.Header.Set("X-Requested-With", "xhr")
	r.Header.Set("Origin", "https://other.test")
	if clearRequestAllowed(r) {
		t.Fatal("cross-origin clear accepted")
	}
	r.Header.Set("Origin", "https://localhost")
	if clearRequestAllowed(r) {
		t.Fatal("different scheme accepted")
	}
	r.Header.Set("Origin", "http://localhost")
	if !clearRequestAllowed(r) {
		t.Fatal("same-origin clear rejected")
	}
}
func TestClearWhileSubscriberReads(t *testing.T) {
	s := New(nil, nil)
	_, ch := s.subscribe()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ch:
			case <-stop:
				return
			}
		}
	}()
	for range 1000 {
		s.ClearEvents()
	}
	close(stop)
	<-done
	s.unsubscribe(ch)
}
