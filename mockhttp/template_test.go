package mockhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

// serve parses routes, serves one request and returns the recorder.
func serve(t *testing.T, routes string, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	return serveFrom(t, "fixture.http", routes, r)
}

func serveFrom(t *testing.T, source, routes string, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	methods, err := restclient.Parse(source, strings.NewReader(routes))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	New(methods, slog.New(slog.DiscardHandler)).ServeHTTP(w, r)
	return w
}

func TestRepeatBlocks(t *testing.T) {
	tests := []struct {
		name, route, want string
	}{
		{"json commas", "### R\nGET /r\nContent-Type: application/json\n\n[{{#repeat 3}}{\"i\":{{$index}}}{{/repeat}}]", `[{"i":0},{"i":1},{"i":2}]`},
		{"zero copies", "### R\nGET /r\nContent-Type: application/json\n\n[{{#repeat 0}}{\"i\":{{$index}}}{{/repeat}}]", `[]`},
		{"standalone lines", "### R\nGET /r\nContent-Type: application/json\n\n[\n  {{#repeat 2}}\n  {\"i\": {{$index}}}\n  {{/repeat}}\n]", "[\n  {\"i\": 0},\n  {\"i\": 1}\n]"},
		{"nested", "### R\nGET /r\n\n{{#repeat 2}}{{#repeat 2}}{{$index}}{{/repeat}};{{/repeat}}", "01;01;"},
		{"plain text has no commas", "### R\nGET /r\n\n{{#repeat 3}}x{{/repeat}}", "xxx"},
		{"inside a JSON string", "### R\nGET /r\nContent-Type: application/json\n\n{\"s\":\"{{#repeat 3}}a{{/repeat}}\"}", `{"s":"aaa"}`},
		{"unclosed stays literal", "### R\nGET /r\n\n{{#repeat 2}}x", "{{#repeat 2}}x"},
		{"bad count stays literal", "### R\nGET /r\n\n{{#repeat lots}}x{{/repeat}}", "{{#repeat lots}}x{{/repeat}}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serve(t, tt.route, httptest.NewRequest("GET", "/r", nil))
			if got := w.Body.String(); got != tt.want {
				t.Fatalf("body = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRepeatRangeAndFreshValues(t *testing.T) {
	route := "### R\nGET /r\nContent-Type: application/json\n\n[{{#repeat 2 5}}\"{{$uuid}}\"{{/repeat}}]"
	w := serve(t, route, httptest.NewRequest("GET", "/r", nil))
	var ids []string
	if err := json.Unmarshal(w.Body.Bytes(), &ids); err != nil {
		t.Fatalf("invalid JSON %s: %v", w.Body, err)
	}
	if len(ids) < 2 || len(ids) > 5 {
		t.Fatalf("got %d copies, want 2 to 5", len(ids))
	}
	if ids[0] == ids[1] {
		t.Fatal("each copy should generate fresh values")
	}
}

func TestRepeatOutputIsCapped(t *testing.T) {
	route := "### R\nGET /r\n\n{{#repeat 1000}}{{#repeat 1000}}{{#repeat 1000}}xxxxxxxxxx{{/repeat}}{{/repeat}}{{/repeat}}"
	w := serve(t, route, httptest.NewRequest("GET", "/r", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}

func TestFilters(t *testing.T) {
	route := "### F\nGET /f/:name\n\n" +
		`{{$name | upper}} {{$name | lower}} {{$q | trim}} {{$q | urlencode}} {{$name | base64}} {{$q | json}} {{$n | add 5}} {{$missing | default "none here"}} {{$missing | default x | upper}} {{$name | default x}}`
	w := serve(t, route, httptest.NewRequest("GET", "/f/Ada?q=%20a%20b&n=37", nil))
	want := `ADA ada a b %20a%20b QWRh " a b" 42 none here X Ada`
	if got := w.Body.String(); got != want {
		t.Fatalf("body = %q\nwant   %q", got, want)
	}
}

func TestUnknownFilterStaysLiteral(t *testing.T) {
	w := serve(t, "### F\nGET /f\n\n{{$q | shout}}", httptest.NewRequest("GET", "/f?q=x", nil))
	if got := w.Body.String(); got != "{{$q | shout}}" {
		t.Fatalf("body = %q", got)
	}
}

func TestKeepReusesGeneratedValues(t *testing.T) {
	route := "### K\nGET /k\nX-Id: {{$uuid | keep}}\n\n{{$uuid | keep}} {{$uuid}} {{$uuid | keep other}} {{$uuid | keep other}}"
	w := serve(t, route, httptest.NewRequest("GET", "/k", nil))
	fields := strings.Fields(w.Body.String())
	if len(fields) != 4 {
		t.Fatalf("body = %q", w.Body)
	}
	if w.Header().Get("X-Id") != fields[0] {
		t.Fatalf("header %q and body %q should share a kept value", w.Header().Get("X-Id"), fields[0])
	}
	if fields[1] == fields[0] {
		t.Fatal("a placeholder without keep should generate a fresh value")
	}
	if fields[2] != fields[3] || fields[2] == fields[0] {
		t.Fatalf("named keep: %q", fields)
	}
}

func TestKeepIsScopedToRepeatCopies(t *testing.T) {
	route := "### K\nGET /k\n\n{{#repeat 2}}{{$uuid | keep}}={{$uuid | keep}};{{/repeat}}"
	w := serve(t, route, httptest.NewRequest("GET", "/k", nil))
	copies := strings.Split(strings.TrimSuffix(w.Body.String(), ";"), ";")
	if len(copies) != 2 {
		t.Fatalf("body = %q", w.Body)
	}
	a, b, _ := strings.Cut(copies[0], "=")
	c, _, _ := strings.Cut(copies[1], "=")
	if a != b || a == c {
		t.Fatalf("keep should repeat within a copy and differ between copies: %q", copies)
	}

	// A value kept before the block is shared by every copy.
	route = "### K\nGET /k\n\n{{$uuid | keep}}|{{#repeat 2}}{{$uuid | keep}};{{/repeat}}"
	w = serve(t, route, httptest.NewRequest("GET", "/k", nil))
	outer, inner, _ := strings.Cut(w.Body.String(), "|")
	if inner != outer+";"+outer+";" {
		t.Fatalf("body = %q", w.Body)
	}
}

func TestSeedMakesResponsesReproducible(t *testing.T) {
	route := "### S\n@seed = demo\nGET /s/:id\n\n{{$uuid}} {{$name}} {{$randomInt 1 1000000}} {{$timestamp}} {{#repeat 1 9}}x{{/repeat}}"
	first := serve(t, route, httptest.NewRequest("GET", "/s/1", nil)).Body.String()
	again := serve(t, route, httptest.NewRequest("GET", "/s/1", nil)).Body.String()
	other := serve(t, route, httptest.NewRequest("GET", "/s/2", nil)).Body.String()
	if first != again {
		t.Fatalf("same request differs:\n%s\n%s", first, again)
	}
	if first == other {
		t.Fatal("different requests should get different values")
	}
	unseeded := strings.Replace(route, "@seed = demo\n", "", 1)
	if serve(t, unseeded, httptest.NewRequest("GET", "/s/1", nil)).Body.String() == serve(t, unseeded, httptest.NewRequest("GET", "/s/1", nil)).Body.String() {
		t.Fatal("unseeded responses should differ")
	}
}

func TestExplicitNamespaces(t *testing.T) {
	route := "### N\nGET /n/:id\n\n{{$path.id}} {{$query.id}} {{$id}} {{$header.X-Token}} {{$path.nope}} {{$query.nope | default -}}"
	r := httptest.NewRequest("GET", "/n/7?id=9", nil)
	r.Header.Set("X-Token", "abc")
	if got := serve(t, route, r).Body.String(); got != "7 9 7 abc {{$path.nope}} -" {
		t.Fatalf("body = %q", got)
	}
}

func TestTemplatedBodyFilePaths(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "users"), 0o755)
	os.WriteFile(filepath.Join(dir, "users", "1.json"), []byte(`{"id":"{{$id}}"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.json"), []byte(`secret`), 0o644)
	source := filepath.Join(dir, "api.http")
	route := "### User\nGET /users/:id\n\n< ./users/{{$id}}.json"

	w := serveFrom(t, source, route, httptest.NewRequest("GET", "/users/1", nil))
	if w.Code != http.StatusOK || w.Body.String() != `{"id":"1"}` || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("got %d %q %v", w.Code, w.Body, w.Header())
	}
	for _, path := range []string{"/users/2", "/users/..%2Fsecret", "/users/%2E%2E%2Fsecret"} {
		w = serveFrom(t, source, route, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d %q, want 404", path, w.Code, w.Body)
		}
	}
}

func TestTemplateModes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "body.json"), []byte(`{"x":"{{$q}}"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "body.bin"), []byte(`bin {{$q}}`), 0o644)
	source := filepath.Join(dir, "api.http")
	tests := []struct{ name, route, want string }{
		{"auto file", "### T\nGET /t\n\n< body.json", `{"x":"v"}`},
		{"off file", "### T\n@template = off\nGET /t\n\n< body.json", `{"x":"{{$q}}"}`},
		{"auto binary", "### T\nGET /t\n\n< body.bin", `bin {{$q}}`},
		{"on binary", "### T\n@template = on\nGET /t\n\n< body.bin", `bin v`},
		{"off inline", "### T\n@template = off\nGET /t\nX-Q: {{$q}}\n\n{{$q}}", `{{$q}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := serveFrom(t, source, tt.route, httptest.NewRequest("GET", "/t?q=v", nil))
			if got := w.Body.String(); got != tt.want {
				t.Fatalf("body = %q, want %q", got, tt.want)
			}
			if tt.name == "off inline" && w.Header().Get("X-Q") != "{{$q}}" {
				t.Fatalf("header = %q", w.Header().Get("X-Q"))
			}
		})
	}
}

func TestTrimOffKeepsTheBodyExactly(t *testing.T) {
	route := "### T\r\n@trim = off\r\nGET /t\r\n\r\n\r\n  line one\r\n\r\nline two\n\n"
	w := serve(t, route, httptest.NewRequest("GET", "/t", nil))
	if got, want := w.Body.String(), "\r\n  line one\r\n\r\nline two\n\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	w = serve(t, strings.Replace(route, "@trim = off\r\n", "", 1), httptest.NewRequest("GET", "/t", nil))
	if got := w.Body.String(); strings.HasPrefix(got, "\r\n") || strings.HasSuffix(got, "\n\n") {
		t.Fatalf("default trimming changed: %q", got)
	}
}

func TestStreamChunks(t *testing.T) {
	split := func(body string, sse bool) []string {
		var out []string
		for _, chunk := range streamChunks([]byte(body), sse) {
			out = append(out, string(chunk))
		}
		return out
	}
	if got := split("a\nb\r\nc", false); strings.Join(got, "|") != "a\n|b\r\n|c" {
		t.Fatalf("lines = %q", got)
	}
	if got := split("data: 1\nid: 1\n\ndata: 2\r\n\r\ndata: 3", true); strings.Join(got, "|") != "data: 1\nid: 1\n\n|data: 2\r\n\r\n|data: 3" {
		t.Fatalf("events = %q", got)
	}
}

func TestStreamingResponse(t *testing.T) {
	methods, err := restclient.Parse("fixture.http", strings.NewReader("### S\n@stream = 30ms\nGET /s\nContent-Type: text/event-stream\n\ndata: 1\n\ndata: 2\n\ndata: 3"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(s)
	defer srv.Close()

	started := time.Now()
	resp, err := http.Get(srv.URL + "/s")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ContentLength != -1 {
		t.Fatalf("streamed response has Content-Length %d", resp.ContentLength)
	}
	first := make([]byte, 64)
	n, _ := resp.Body.Read(first)
	if string(first[:n]) != "data: 1\n\n" {
		t.Fatalf("first chunk = %q", first[:n])
	}
	rest, _ := io.ReadAll(resp.Body)
	if string(first[:n])+string(rest) != "data: 1\n\ndata: 2\n\ndata: 3" {
		t.Fatalf("body = %q", string(first[:n])+string(rest))
	}
	if elapsed := time.Since(started); elapsed < 60*time.Millisecond {
		t.Fatalf("stream finished in %v, want at least two 30ms pauses", elapsed)
	}
}

func faultServer(t *testing.T, route string) (*Server, *httptest.Server) {
	t.Helper()
	methods, err := restclient.Parse("fixture.http", strings.NewReader(route))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv
}

func lastEvent(t *testing.T, s *Server) RequestEvent {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events) == 0 {
		t.Fatal("no request event")
	}
	return s.events[len(s.events)-1]
}

func TestFaultReset(t *testing.T) {
	s, srv := faultServer(t, "### F\n@fault = reset\nGET /f\n\nhello")
	resp, err := http.Get(srv.URL + "/f")
	if err == nil {
		resp.Body.Close()
		t.Fatalf("reset fault answered %d", resp.StatusCode)
	}
	event := lastEvent(t, s)
	if event.Response.Fault != "reset" || event.Response.Status != 0 {
		t.Fatalf("event response = %+v", event.Response)
	}
}

func TestFaultTruncate(t *testing.T) {
	s, srv := faultServer(t, "### F\n@fault = truncate\nGET /f\n\n0123456789")
	resp, err := http.Get(srv.URL + "/f")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if !errors.Is(err, io.ErrUnexpectedEOF) || string(body) != "01234" || resp.ContentLength != 10 {
		t.Fatalf("got %q, %v, Content-Length %d", body, err, resp.ContentLength)
	}
	event := lastEvent(t, s)
	if event.Response.Fault != "truncate" || event.Response.Status != http.StatusOK {
		t.Fatalf("event response = %+v", event.Response)
	}
}

func TestFaultRateZeroNeverFires(t *testing.T) {
	_, srv := faultServer(t, "### F\n@fault = reset 0%\nGET /f\n\nhello")
	resp, err := http.Get(srv.URL + "/f")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestRandomDelayRange(t *testing.T) {
	methods, err := restclient.Parse("fixture.http", strings.NewReader("### D\n@delay = 20ms..40ms\nGET /d\n\nok"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.DiscardHandler))
	if m := s.methods[0]; m.Delay != 20*time.Millisecond || m.DelayMax != 40*time.Millisecond {
		t.Fatalf("delay = %v..%v", m.Delay, m.DelayMax)
	}
	if got := describeRoute(s.methods[0], 0, 0).Delay; got != "20ms..40ms" {
		t.Fatalf("route delay = %q", got)
	}
	started := time.Now()
	s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/d", nil))
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("responded after %v, want at least 20ms", elapsed)
	}
}

func TestTemplateWarnings(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	methods, err := restclient.Parse("fixture.http", strings.NewReader(
		"### W\nGET /w/:id\n\n{{$id | shout}} {{$path.nope}} {{$path.id}} {{$query.x}} {{$header.X}} {{$gone | default x}} {{#repeat 2}}{{$index}}{{/repeat}} {{$index}} {{/repeat}}"))
	if err != nil {
		t.Fatal(err)
	}
	New(methods, logger)
	out := logs.String()
	for _, want := range []string{`unknown filter \"shout\"`, "no :nope parameter", "placeholder=index", "has no matching {{#repeat}}"} {
		if !strings.Contains(out, want) {
			t.Errorf("warnings missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"placeholder=gone", "placeholder=query.x", "placeholder=header.X", "placeholder=path.id"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected warning %q:\n%s", unwanted, out)
		}
	}
	if n := len(regexp.MustCompile(`placeholder=index`).FindAllString(out, -1)); n != 1 {
		t.Errorf("want one $index warning (outside the repeat), got %d", n)
	}
}

func FuzzRenderTemplate(f *testing.F) {
	for _, seed := range []string{
		`[{{#repeat 2}}{"i":{{$index}},"n":"{{$name | upper}}"}{{/repeat}}]`,
		`{{#repeat 1 3}}{{#repeat 2}}x{{/repeat}}{{/repeat}}{{/repeat}}`,
		`{{$q | default "a|b" | keep k}} {{$q | add 1}} {{v}} {{%v}}`,
		`{"s":"{{#repeat 2}}\"{{$uuid}}{{/repeat}}"}`,
	} {
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, input string, jsonMode bool) {
		method := &restclient.Method{Variables: map[string]string{"v": "{{v}} {{$q}}"}}
		values := templateValues{values: map[string]string{"q": "7"}}
		_, _ = newRenderer(method, values).text(input, jsonMode)
	})
}
