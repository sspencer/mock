package mockhttp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func TestRouteDefaultsApplyToRoutesWithoutTheirOwn(t *testing.T) {
	methods, err := restclient.Parse("test.http", strings.NewReader(`### Plain
GET /plain

ok

### Own delay
@delay = 0
GET /fast

ok

### Own fault
@fault = reset 0%
GET /safe

ok
`))
	if err != nil {
		t.Fatal(err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server.SetRouteDefaults(RouteDefaults{Delay: 40 * time.Millisecond, Fault: "reset", FaultRate: 1})

	plain := server.withDefaults(&server.methods[0])
	if plain.Delay != 40*time.Millisecond || plain.Fault != "reset" {
		t.Errorf("plain route = delay %s fault %q, want defaults", plain.Delay, plain.Fault)
	}
	if server.methods[0].Delay != 0 || server.methods[0].Fault != "" {
		t.Error("withDefaults changed the stored route")
	}
	if fast := server.withDefaults(&server.methods[1]); fast.Delay != 0 || fast.Fault != "reset" {
		t.Errorf("@delay = 0 route = delay %s fault %q, want own delay and default fault", fast.Delay, fast.Fault)
	}
	if safe := server.withDefaults(&server.methods[2]); safe.Delay != 40*time.Millisecond || safe.FaultRate != 0 {
		t.Errorf("@fault = reset 0%% route = delay %s rate %v, want default delay and own fault", safe.Delay, safe.FaultRate)
	}

	server.SetRouteDefaults(RouteDefaults{Delay: 40 * time.Millisecond})
	start := time.Now()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/safe", nil))
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond || response.Code != http.StatusOK {
		t.Errorf("GET /safe = %d after %s, want 200 after at least 40ms", response.Code, elapsed)
	}

	server.SetRouteDefaults(RouteDefaults{})
	if got := server.withDefaults(&server.methods[0]); got != &server.methods[0] {
		t.Error("cleared defaults still copy the route")
	}
}

func TestRouteDefaultsStrings(t *testing.T) {
	d := RouteDefaults{Delay: 50 * time.Millisecond, DelayMax: 300 * time.Millisecond, Fault: "reset", FaultRate: 0.05}
	if d.DelayString() != "50ms..300ms" || d.FaultString() != "reset 5%" {
		t.Errorf("strings = %q, %q", d.DelayString(), d.FaultString())
	}
	if (RouteDefaults{}).DelayString() != "" || (RouteDefaults{}).FaultString() != "" {
		t.Error("empty defaults should describe as empty")
	}
}
