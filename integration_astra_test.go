package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exercise the embedded module UI, real TLS/net/http behavior, and SSE protocol
// together; recorder-only tests cannot verify actual transport metadata.
func TestTLSCaptureAndEmbeddedDashboardIntegration(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader("### response\n# $status=201\nPOST /x/:id\nContent-Type: application/json\nSet-Cookie: one=1\nSet-Cookie: two=2\n\n{\"id\":\"{{$id}}\"}"))
	if err != nil {
		t.Fatal(err)
	}
	mock := mockhttp.New(methods, nil)
	assets, err := staticFileSystem()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(withCORS(newHandler(mock, "mock", assets), "*", "mock"))
	defer server.Close()
	client := server.Client()
	req, err := http.NewRequest("POST", server.URL+"/x/a%252Fb", bytes.NewReader([]byte{0, 255}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 201 || string(body) != `{"id":"a%2Fb"}` {
		t.Fatalf("unexpected response: %d %s", res.StatusCode, body)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("mock CORS missing")
	}
	stream, err := client.Get(server.URL + "/mock/events")
	if err != nil {
		t.Fatal(err)
	}
	if stream.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("admin SSE exposed through CORS")
	}
	var event mockhttp.RequestEvent
	scanner := bufio.NewScanner(stream.Body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	stream.Body.Close()
	if event.Request.Scheme != "https" || event.Request.Body.Encoding != "base64" || event.Request.Body.Text != "AP8=" {
		t.Fatalf("incorrect request capture: %+v", event.Request)
	}
	if event.Response.Headers.Get("Date") != res.Header.Get("Date") || event.Response.Headers.Get("Content-Length") != res.Header.Get("Content-Length") || len(event.Response.Headers.Values("Set-Cookie")) != 2 {
		t.Fatalf("capture differs from wire headers: %v vs %v", event.Response.Headers, res.Header)
	}
	for _, path := range []string{"/mock/", "/mock/app.mjs", "/mock/har.mjs", "/mock/traffic.mjs"} {
		page, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(page.Body)
		page.Body.Close()
		if page.StatusCode != 200 || len(data) == 0 {
			t.Fatalf("embedded asset %s unavailable", path)
		}
		if strings.HasSuffix(path, ".mjs") && !strings.Contains(page.Header.Get("Content-Type"), "javascript") {
			t.Fatalf("module %s has wrong MIME %s", path, page.Header.Get("Content-Type"))
		}
	}
	clear, _ := http.NewRequest("POST", server.URL+"/mock/clear", nil)
	clear.Header.Set("X-Requested-With", "xhr")
	clear.Header.Set("Origin", "https://other.test")
	denied, err := client.Do(clear)
	if err != nil {
		t.Fatal(err)
	}
	denied.Body.Close()
	if denied.StatusCode != 403 {
		t.Fatal("cross-origin clear was allowed")
	}
	clear.Header.Set("Origin", server.URL)
	accepted, err := client.Do(clear)
	if err != nil {
		t.Fatal(err)
	}
	accepted.Body.Close()
	if accepted.StatusCode != 204 || accepted.Header.Get("X-Mock-Session") != event.Session || accepted.Header.Get("X-Mock-Cursor") == "" {
		t.Fatal("missing clear boundary metadata")
	}
}
