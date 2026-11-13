package mockhttp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sspencer/mock/restclient"
)

const orderScenario = `### Pending order
@scenario = order
@state = started
GET /orders/1

pending

### Pay
@scenario = order
@next-state = paid
POST /orders/1/pay

ok

### Paid order
@scenario = order
@state = paid
GET /orders/1

paid
`

func scenarioServer(t *testing.T, text string) *Server {
	t.Helper()
	methods, err := restclient.Parse("orders.http", strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func serveMethod(s *Server, method, path string) (int, string) {
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code, rec.Body.String()
}

func TestScenarioMovesBetweenStates(t *testing.T) {
	s := scenarioServer(t, orderScenario)
	if _, body := serveMethod(s, "GET", "/orders/1"); body != "pending" {
		t.Fatalf("before paying: %q", body)
	}
	if info := s.Explain(httptest.NewRequest("POST", "/orders/1/pay", nil)); info.Route == nil {
		t.Fatal("Explain found no route")
	}
	if _, body := serveMethod(s, "GET", "/orders/1"); body != "pending" {
		t.Fatalf("Explain moved the scenario: %q", body)
	}
	serveMethod(s, "POST", "/orders/1/pay")
	if _, body := serveMethod(s, "GET", "/orders/1"); body != "paid" {
		t.Fatalf("after paying: %q", body)
	}
	got := s.Scenarios()
	if len(got) != 1 || got[0].Name != "order" || got[0].State != "paid" || strings.Join(got[0].States, ",") != "paid,started" {
		t.Errorf("Scenarios() = %+v", got)
	}

	// Reloading keeps the state; Reset starts it over.
	methods, _ := restclient.Parse("orders.http", strings.NewReader(orderScenario))
	s.SetMethods(methods)
	if _, body := serveMethod(s, "GET", "/orders/1"); body != "paid" {
		t.Errorf("after reload: %q", body)
	}
	s.Reset()
	if _, body := serveMethod(s, "GET", "/orders/1"); body != "pending" {
		t.Errorf("after reset: %q", body)
	}
	s.SetScenario("order", "paid")
	if _, body := serveMethod(s, "GET", "/orders/1"); body != "paid" {
		t.Errorf("after SetScenario: %q", body)
	}
}

func TestScenarioReasonAndRouteInfo(t *testing.T) {
	s := scenarioServer(t, "### Paid\n@scenario = order\n@state = paid\n@next-state = shipped\nGET /orders/1\n\npaid\n")
	if code, _ := serveMethod(s, "GET", "/orders/1"); code != 404 {
		t.Fatalf("code = %d, want 404", code)
	}
	reasons := s.events[len(s.events)-1].Match.Candidates[0].Reasons
	if want := `Scenario order is in state "started"; this route answers in state "paid"`; len(reasons) != 1 || !strings.HasPrefix(reasons[0], want) {
		t.Errorf("reasons = %q, want %q", reasons, want)
	}
	route := s.Routes()[0]
	if route.Scenario != "order" || route.State != "paid" || route.NextState != "shipped" || route.ScenarioState != "started" {
		t.Errorf("route = %+v", route)
	}
	s.SetScenario("order", "paid")
	if got := s.Routes()[0].ScenarioState; got != "paid" {
		t.Errorf("ScenarioState after SetScenario = %q, want paid", got)
	}
}

func TestScenarioConcurrentTransitionsAnswerOnce(t *testing.T) {
	s := scenarioServer(t, "### Claim\n@scenario = prize\n@state = started\n@next-state = claimed\nPOST /claim\n\nyours\n\n### Gone\nPOST /claim\n\ngone\n")
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	for range 50 {
		wg.Go(func() {
			if _, body := serveMethod(s, "POST", "/claim"); body == "yours" {
				mu.Lock()
				won++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if won != 1 {
		t.Errorf("%d requests claimed the prize, want 1", won)
	}
}

func TestScenarioShadowing(t *testing.T) {
	methods, err := restclient.Parse("orders.http", strings.NewReader(orderScenario))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_ = New(methods, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	if strings.Contains(buf.String(), "route can never match") {
		t.Errorf("a @state route shadowed a later one: %s", buf.String())
	}
}

func TestServeScenarios(t *testing.T) {
	s := scenarioServer(t, orderScenario)
	req := httptest.NewRequest("PUT", "/scenarios", strings.NewReader(`{"name":"order","state":"paid"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeScenarios(rec, req)
	var got []ScenarioInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 || got[0].State != "paid" {
		t.Fatalf("PUT /scenarios = %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	s.ServeScenarios(rec, httptest.NewRequest("PUT", "/scenarios", strings.NewReader(`{"name":"order"}`)))
	if rec.Code != http.StatusForbidden {
		t.Errorf("PUT without a JSON content type = %d, want 403", rec.Code)
	}
	req = httptest.NewRequest("PUT", "/scenarios", strings.NewReader(`{"state":"paid"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.ServeScenarios(rec, req)
	if rec.Code != 400 {
		t.Errorf("PUT without a name = %d, want 400", rec.Code)
	}
}
