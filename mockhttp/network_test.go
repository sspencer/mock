package mockhttp

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

// rawResponse sends a GET over a plain TCP connection and returns what came
// back before the server closed it.
func rawResponse(t *testing.T, srv *httptest.Server, path string) []byte {
	t.Helper()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, "GET "+path+" HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(conn)
	return data
}

func TestFaultEmptyAndGarbage(t *testing.T) {
	s, srv := faultServer(t, "### Empty\n@fault = empty\nGET /empty\n\nhello\n\n### Garbage\n@fault = garbage\nGET /garbage\n\nhello\n")
	if got := rawResponse(t, srv, "/empty"); len(got) != 0 {
		t.Errorf("empty fault sent %q", got)
	}
	if event := lastEvent(t, s); event.Response.Fault != "empty" {
		t.Errorf("logged fault = %q", event.Response.Fault)
	}
	got := rawResponse(t, srv, "/garbage")
	if len(got) < 64 || got[0] != 0 || strings.HasPrefix(string(got), "HTTP/") {
		t.Errorf("garbage fault sent %d bytes: %q", len(got), got)
	}
	if _, err := http.ReadResponse(bufio.NewReader(strings.NewReader(string(got))), nil); err == nil {
		t.Error("garbage parsed as an HTTP response")
	}
	if event := lastEvent(t, s); event.Response.Fault != "garbage" {
		t.Errorf("logged fault = %q", event.Response.Fault)
	}
}

func TestFaultHang(t *testing.T) {
	s, srv := faultServer(t, "### Hang\n@fault = hang\nGET /hang\n\nhello\n")
	client := &http.Client{Timeout: 150 * time.Millisecond}
	started := time.Now()
	resp, err := client.Get(srv.URL + "/hang")
	if err == nil {
		resp.Body.Close()
		t.Fatalf("hang fault answered %d", resp.StatusCode)
	}
	if elapsed := time.Since(started); elapsed < 140*time.Millisecond {
		t.Errorf("client gave up after %s, before its timeout", elapsed)
	}
	if event := lastEvent(t, s); event.Response.Fault != "hang" {
		t.Errorf("logged fault = %q", event.Response.Fault)
	}

	// Close releases a hanging handler.
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Get(srv.URL + "/hang")
		if err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	s.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not release a hanging request")
	}
}

func TestThrottle(t *testing.T) {
	body := strings.Repeat("x", 300)
	s := newDirServer(t, map[string]string{"blob.bin": body}, "### Slow\n@throttle = 1kb/s\nGET /slow\n\n"+body+"\n\n### File\n@throttle = 1kb/s\nGET /file\n\n< blob.bin\n")
	for _, path := range []string{"/slow", "/file"} {
		started := time.Now()
		code, got := serveRequest(t, s, "GET", path, "", "")
		elapsed := time.Since(started)
		// 300 bytes at 1024 B/s go in 102-byte slices, 100ms apart.
		if code != 200 || got != body || elapsed < 180*time.Millisecond || elapsed > 2*time.Second {
			t.Errorf("GET %s = %d, %d bytes in %s", path, code, len(got), elapsed)
		}
	}
	if got := describeRoute(s.Methods()[0], 0, 1).Throttle; got != "1kb/s" {
		t.Errorf("RouteInfo.Throttle = %q", got)
	}
	for rate, want := range map[int64]string{512: "512b/s", 32 * 1024: "32kb/s", 3 << 19: "1.5mb/s", 1500: "1.46484375kb/s"} {
		if got := describeThrottle(rate); got != want {
			t.Errorf("describeThrottle(%d) = %q, want %q", rate, got, want)
		}
	}
}

func TestLognormalDelay(t *testing.T) {
	delay, _ := restclient.ParseDelay("p50 100ms p99 1s")
	var samples []time.Duration
	for range 4000 {
		samples = append(samples, sampleDelay(delay.Min, 0, delay.Sigma))
	}
	slices.Sort(samples)
	p50, p99 := samples[len(samples)/2], samples[len(samples)*99/100]
	if math.Abs(float64(p50-100*time.Millisecond)) > float64(15*time.Millisecond) || p99 < 700*time.Millisecond || p99 > 1400*time.Millisecond {
		t.Errorf("p50 = %s, p99 = %s; want about 100ms and 1s", p50, p99)
	}
	if limit := time.Duration(float64(delay.Min) * math.Exp(z9999*delay.Sigma)); samples[len(samples)-1] > limit {
		t.Errorf("max sample %s is over the cap %s", samples[len(samples)-1], limit)
	}
	if got := describeDelay(restclient.Method{Delay: delay.Min, DelaySigma: delay.Sigma}); got != "p50 100ms p99 1s" {
		t.Errorf("describeDelay = %q", got)
	}
	if got := sampleDelay(10*time.Millisecond, 20*time.Millisecond, 0); got < 10*time.Millisecond || got > 20*time.Millisecond {
		t.Errorf("uniform sample = %s", got)
	}
}

func TestRateLimitKeyAndHeaders(t *testing.T) {
	s := newTestServer(t, "### Search\n@rate-limit = 2/1m\n@rate-limit.key = {{$header.Authorization}}\nGET /search\n\nok\n")
	get := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/search", nil)
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	for i, want := range []string{"1", "0"} {
		rec := get("alice")
		if rec.Code != 200 || rec.Header().Get("X-RateLimit-Limit") != "2" || rec.Header().Get("X-RateLimit-Remaining") != want || rec.Header().Get("X-RateLimit-Reset") != "60" {
			t.Errorf("alice request %d = %d %v", i, rec.Code, rec.Header())
		}
	}
	over := get("alice")
	if over.Code != 429 || over.Header().Get("X-RateLimit-Remaining") != "0" || over.Header().Get("Retry-After") != "60" {
		t.Errorf("alice over budget = %d %v", over.Code, over.Header())
	}
	if rec := get("bob"); rec.Code != 200 || rec.Header().Get("X-RateLimit-Remaining") != "1" {
		t.Errorf("bob = %d %v", rec.Code, rec.Header())
	}
	if rec := get(""); rec.Code != 200 {
		t.Errorf("no key = %d", rec.Code)
	}
}

func TestFaultOverrides(t *testing.T) {
	methods, err := restclient.Parse("api.http", strings.NewReader("### Users\nGET /users\n\nusers\n\n### Orders\n@delay = 5ms\nGET /orders\n\norders\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.DiscardHandler))
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	t.Cleanup(s.Close)
	admin := func(method, target, body string) (int, string) {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeFaults(rec, req)
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	timed := func(path string) (time.Duration, error) {
		started := time.Now()
		resp, err := http.Get(srv.URL + path)
		if err == nil {
			_, _ = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		return time.Since(started), err
	}

	if code, body := admin("POST", "/faults", `{"route": "Users", "delay": "120ms"}`); code != 200 || body != `{"routes":["Users"]}` {
		t.Fatalf("POST = %d %s", code, body)
	}
	if elapsed, err := timed("/users"); err != nil || elapsed < 110*time.Millisecond {
		t.Errorf("overridden route took %s (%v)", elapsed, err)
	}
	if elapsed, _ := timed("/orders"); elapsed > 100*time.Millisecond {
		t.Errorf("other route took %s", elapsed)
	}

	// "*" covers the rest; a route's own override wins over it.
	if code, _ := admin("POST", "/faults", `{"route": "*", "fault": "reset"}`); code != 200 {
		t.Fatalf("POST * = %d", code)
	}
	if _, err := timed("/orders"); err == nil {
		t.Error("orders answered despite the reset override for all routes")
	}
	if _, err := timed("/users"); err != nil {
		t.Errorf("users kept its own override but failed: %v", err)
	}
	var routes []RouteInfo
	rec := httptest.NewRecorder()
	s.ServeRoutes(rec, httptest.NewRequest("GET", "/routes", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &routes)
	if routes[0].Override != "delay 120ms" || routes[1].Override != "fault reset (all routes)" {
		t.Errorf("overrides in /routes = %q, %q", routes[0].Override, routes[1].Override)
	}

	// Overrides survive a reload that keeps the route.
	s.SetMethods(methods)
	if code, body := admin("GET", "/faults", ""); code != 200 || !strings.Contains(body, `"route":"*","fault":"reset"`) || !strings.Contains(body, `"route":"Users","source":"api.http","line":1,"delay":"120ms"`) {
		t.Errorf("GET = %d %s", code, body)
	}
	if code, body := admin("DELETE", "/faults?route=*", ""); code != 200 || body != `{"removed":1}` {
		t.Errorf("DELETE * = %d %s", code, body)
	}
	if _, err := timed("/orders"); err != nil {
		t.Errorf("orders still failing after the override was cleared: %v", err)
	}
	if code, body := admin("DELETE", "/faults", ""); code != 200 || body != `{"removed":1}` {
		t.Errorf("DELETE all = %d %s", code, body)
	}

	for body, want := range map[string]string{
		`{"route": "Nope", "delay": "1s"}`:    "no route has the id or name",
		`{"route": "Users"}`:                  "set at least one of",
		`{"route": "Users", "fault": "boom"}`: "fault must be",
		`{"delay": "1s"}`:                     "route is required",
		`{"route": "*", "throttle": "quick"}`: "throttle must be",
	} {
		if code, got := admin("POST", "/faults", body); code != 400 || !strings.Contains(got, want) {
			t.Errorf("POST %s = %d %s, want %q", body, code, got, want)
		}
	}
	req := httptest.NewRequest("POST", "/faults", strings.NewReader(`{"route":"*","delay":"1s"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	s.ServeFaults(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST = %d", rec.Code)
	}
	if names, err := s.SetFault("2:1", FaultOverride{Throttle: "none", Fault: "none"}); err != nil || len(names) != 1 || names[0] != "Orders" {
		t.Errorf("SetFault by id = %v, %v", names, err)
	}
}

func TestRouteSwitchesOffAndStatus(t *testing.T) {
	s := newTestServer(t, "### Orders\nGET /orders\n\nok\n\n### [404] Fallback\nGET /orders\n\nnone\n")
	if _, err := s.SetFault("Orders", FaultOverride{Off: true}); err != nil {
		t.Fatal(err)
	}
	if code, body := serveRequest(t, s, "GET", "/orders", "", ""); code != 404 || body != "none" {
		t.Errorf("switched off = %d %q, want the next route", code, body)
	}
	if info := s.Explain(httptest.NewRequest("GET", "/orders", nil)); info.Route == nil || info.Route.Name != "Fallback" {
		t.Errorf("Explain = %+v", info.Route)
	}
	if _, err := s.SetFault("Fallback", FaultOverride{Off: true}); err != nil {
		t.Fatal(err)
	}
	var event RequestEvent
	s.SetRequestHook(func(e RequestEvent) { event = e })
	serveRequest(t, s, "GET", "/orders", "", "")
	if len(event.Match.Candidates) == 0 || !strings.Contains(event.Match.Candidates[0].Reasons[0], "Switched off") {
		t.Errorf("candidates = %+v", event.Match.Candidates)
	}
	if _, err := s.SetFault("Orders", FaultOverride{Status: 503}); err != nil {
		t.Fatal(err)
	}
	if code, body := serveRequest(t, s, "GET", "/orders", "", ""); code != 503 || body != "ok" {
		t.Errorf("forced status = %d %q", code, body)
	}
	rec := httptest.NewRecorder()
	s.ServeRoutes(rec, httptest.NewRequest("GET", "/routes", nil))
	var routes []RouteInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &routes)
	if routes[0].Switch == nil || routes[0].Switch.Status != 503 || routes[0].Override != "status 503" || routes[1].Switch == nil || !routes[1].Switch.Off {
		t.Errorf("routes = %+v / %+v", routes[0].Switch, routes[1].Switch)
	}
	rec = httptest.NewRecorder()
	s.ServeState(rec, httptest.NewRequest("GET", "/state", nil))
	if !strings.Contains(rec.Body.String(), `"switches":2`) {
		t.Errorf("/state = %s", rec.Body.String())
	}
	if _, err := s.SetFault("Orders", FaultOverride{Status: 99}); err == nil {
		t.Error("status 99 accepted")
	}
}
