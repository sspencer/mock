package mockhttp

import (
	"bytes"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestHostMatcher(t *testing.T) {
	methods, err := restclient.Parse("hosts.http", strings.NewReader(`### Billing
@match.host = billing.example.test
GET /status

billing

### Pinned port
@match.host = auth.example.test:9000
GET /status

auth 9000

### Any shop
@match.host = ~.*\.shop\.test
GET /status

shop

### Default
GET /status

default
`))
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for host, want := range map[string]string{
		"billing.example.test":      "billing",
		"BILLING.example.test:8080": "billing",
		"billing.example.test.":     "billing",
		"auth.example.test:9000":    "auth 9000",
		"auth.example.test:9001":    "default",
		"eu.shop.test":              "shop",
		"localhost:8080":            "default",
		"":                          "default",
	} {
		req := httptest.NewRequest("GET", "/status", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if got := rec.Body.String(); got != want {
			t.Errorf("Host %q answered %q, want %q", host, got, want)
		}
	}

	only := New(methods[:1], slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest("GET", "/status", nil)
	req.Host = "localhost:8080"
	only.ServeHTTP(httptest.NewRecorder(), req)
	reasons := strings.Join(only.events[len(only.events)-1].Match.Candidates[0].Reasons, "\n")
	if want := `Host: expected "billing.example.test"; received ["localhost"]`; !strings.Contains(reasons, "Host: ") || !strings.Contains(reasons, "localhost") {
		t.Errorf("reasons = %q, want something like %q", reasons, want)
	}
	if routes := only.Routes(); routes[0].MatchHost != "billing.example.test" {
		t.Errorf("RouteInfo.MatchHost = %q", routes[0].MatchHost)
	}
}

func TestHostMatcherShadowing(t *testing.T) {
	warnings := func(text string) int {
		t.Helper()
		methods, err := restclient.Parse("hosts.http", strings.NewReader(text))
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_ = New(methods, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
		return strings.Count(buf.String(), "route can never match")
	}
	if n := warnings("### A\n@match.host = a.test\nGET /x\n\n### B\n@match.host = b.test\nGET /x\n"); n != 0 {
		t.Errorf("different hosts: %d warnings, want 0", n)
	}
	if n := warnings("### Any\nGET /x\n\n### A\n@match.host = a.test\nGET /x\n"); n != 1 {
		t.Errorf("any host first: %d warnings, want 1", n)
	}
}
