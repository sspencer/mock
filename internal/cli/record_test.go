package cli

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

func recordingServer(t *testing.T, file string, routes string) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users":
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				gz := gzip.NewWriter(w)
				_, _ = io.WriteString(gz, `[{"id":1}]`)
				_ = gz.Close()
				return
			}
			_, _ = io.WriteString(w, `[{"id":1}]`)
		case "/login":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "ok")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	methods, err := restclient.Parse("routes.http", strings.NewReader(routes))
	if err != nil {
		t.Fatal(err)
	}
	server := mockhttp.New(methods, nil)
	t.Cleanup(server.Close)
	target, _ := url.Parse(upstream.URL)
	server.SetUpstream(target)
	rec, err := newRecorder(file, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	server.SetRequestHook(rec.record)
	front := httptest.NewServer(server)
	t.Cleanup(front.Close)
	return front
}

func send(t *testing.T, method, target, body string, header http.Header) {
	t.Helper()
	req, _ := http.NewRequest(method, target, strings.NewReader(body))
	for name, values := range header {
		req.Header[name] = values
	}
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
}

func TestRecordForwardedRequests(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recorded.http")
	front := recordingServer(t, file, "### Mocked\nGET /mocked\n\nhi\n")
	send(t, "GET", front.URL+"/users?page=1", "", http.Header{"Accept-Encoding": {"gzip"}})
	send(t, "GET", front.URL+"/users?page=2", "", nil)
	send(t, "GET", front.URL+"/mocked", "", nil)
	send(t, "POST", front.URL+"/login", "user=a&pass=b", http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	send(t, "GET", front.URL+"/missing", "", nil)

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	want := "### [200] GET /users\n@query.page = *\nGET /users\nContent-Type: application/json\n\n[{\"id\":1}]\n" +
		"\n### [201] POST /login\n@form.user = *\n@form.pass = *\nPOST /login\nContent-Type: text/plain; charset=utf-8\n\nok\n" +
		"\n### [404] GET /missing\nGET /missing\nContent-Type: text/plain; charset=utf-8\n\n404 page not found\n"
	if string(data) != want {
		t.Fatalf("got\n%s\nwant\n%s", data, want)
	}
	if _, err := restclient.Parse(file, bytes.NewReader(data)); err != nil {
		t.Fatalf("recording does not load: %v", err)
	}

	// A restart keeps the recorded sections and appends only new ones.
	front = recordingServer(t, file, "### Mocked\nGET /mocked\n\nhi\n")
	send(t, "GET", front.URL+"/users", "", nil)
	if again, _ := os.ReadFile(file); string(again) != want {
		t.Fatalf("restart appended a duplicate:\n%s", again)
	}
}

func TestRecordAppendsAfterExistingContent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "recorded.http")
	if err := os.WriteFile(file, []byte("### Old\nGET /old"), 0o644); err != nil {
		t.Fatal(err)
	}
	front := recordingServer(t, file, "")
	send(t, "POST", front.URL+"/login", "", nil)
	data, _ := os.ReadFile(file)
	if !strings.HasPrefix(string(data), "### Old\nGET /old\n\n### [201] POST /login\n") {
		t.Fatalf("got\n%s", data)
	}
	if err := os.WriteFile(file, []byte("not a request file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newRecorder(file, 0, nil); err == nil {
		t.Fatal("recorded to a file that does not parse")
	}
}

func TestRecordNeedsUpstream(t *testing.T) {
	if _, err := parseConfig([]string{"-r", "x.http", "a.http"}); ExitCode(err) != 2 {
		t.Fatalf("got %v", err)
	}
	cfg, err := parseConfig([]string{"--record", "x.http", "-u", "http://example.com", "a.http"})
	if err != nil || cfg.Record != "x.http" {
		t.Fatalf("got %+v, %v", cfg, err)
	}
}
