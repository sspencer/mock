package mockhttp

import (
	"crypto/tls"
	"github.com/sspencer/mock/restclient"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCaptureStructuredBytesAndTLS(t *testing.T) {
	s := New([]restclient.Method{{Method: "POST", Path: "/x", Body: "é\n", Headers: make(http.Header)}}, nil)
	r := httptest.NewRequest("POST", "https://localhost/x", strings.NewReader("é\n"))
	r.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	event := s.events[0]
	if event.Request.Scheme != "https" || event.Request.Body.Size != 3 || event.Response.Body.Size != 3 || event.Response.Body.Text != "é\n" {
		t.Fatalf("bad capture: %+v", event)
	}
	if event.Response.Headers.Get("Content-Length") != "3" || event.Response.Headers.Get("Content-Type") == "" {
		t.Fatal("missing actual metadata")
	}
	binary := eventBody(loggedBody{text: string([]byte{0, 255})}, 2)
	if binary.Encoding != "base64" || binary.Text != "AP8=" {
		t.Fatalf("lost binary bytes: %+v", binary)
	}
}
func TestHeadDoesNotCaptureSuppressedBody(t *testing.T) {
	s := New([]restclient.Method{{Method: "HEAD", Path: "/x", Body: "body", Headers: make(http.Header)}}, nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("HEAD", "/x", nil))
	if s.events[0].Response.Body.Size != 0 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "4" {
		t.Fatal("HEAD capture included a body")
	}
}
