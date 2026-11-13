package mockhttp

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// scenarioState is the current state of the named @scenario. Scenarios
// start in restclient.ScenarioStart.
func (s *Server) scenarioState(name string) string {
	s.scenarioMu.Lock()
	defer s.scenarioMu.Unlock()
	if state, ok := s.scenarios[name]; ok {
		return state
	}
	return restclient.ScenarioStart
}

// scenarioReady reports whether a route's @scenario is in its @state.
func (s *Server) scenarioReady(method *restclient.Method) bool {
	return method.Scenario == "" || method.State == "" || s.scenarioState(method.Scenario) == method.State
}

// advanceScenario moves a route's @scenario to its @next-state as the route
// answers. It reports false when another request moved the scenario out of
// the route's @state first, so the route must not answer.
func (s *Server) advanceScenario(method *restclient.Method) bool {
	if method.Scenario == "" {
		return true
	}
	s.scenarioMu.Lock()
	defer s.scenarioMu.Unlock()
	current, ok := s.scenarios[method.Scenario]
	if !ok {
		current = restclient.ScenarioStart
	}
	if method.State != "" && current != method.State {
		return false
	}
	if method.NextState != "" {
		if s.scenarios == nil {
			s.scenarios = make(map[string]string)
		}
		s.scenarios[method.Scenario] = method.NextState
	}
	return true
}

// scenarioReason explains why a route that matches a request is waiting for
// its @scenario, or returns "".
func (s *Server) scenarioReason(method *restclient.Method) string {
	if s.scenarioReady(method) {
		return ""
	}
	return fmt.Sprintf("Scenario %s is in state %q; this route answers in state %q (POST /reset starts it over)",
		method.Scenario, s.scenarioState(method.Scenario), method.State)
}

// ScenarioInfo describes a @scenario for GET /scenarios: its current state
// and the states its routes name, sorted.
type ScenarioInfo struct {
	Name   string   `json:"name"`
	State  string   `json:"state"`
	States []string `json:"states"`
}

// Scenarios lists every @scenario the current routes name, by name.
func (s *Server) Scenarios() []ScenarioInfo {
	s.mu.Lock()
	methods := s.methods
	s.mu.Unlock()
	states := map[string]map[string]bool{}
	for _, method := range methods {
		if method.Scenario == "" {
			continue
		}
		if states[method.Scenario] == nil {
			states[method.Scenario] = map[string]bool{restclient.ScenarioStart: true}
		}
		for _, state := range []string{method.State, method.NextState} {
			if state != "" {
				states[method.Scenario][state] = true
			}
		}
	}
	out := make([]ScenarioInfo, 0, len(states))
	for _, name := range slices.Sorted(maps.Keys(states)) {
		out = append(out, ScenarioInfo{Name: name, State: s.scenarioState(name), States: slices.Sorted(maps.Keys(states[name]))})
	}
	return out
}

// SetScenario puts the named @scenario in state, so a test can start from
// the middle of a flow. An empty state means restclient.ScenarioStart.
func (s *Server) SetScenario(name, state string) {
	if state == "" {
		state = restclient.ScenarioStart
	}
	s.scenarioMu.Lock()
	defer s.scenarioMu.Unlock()
	if s.scenarios == nil {
		s.scenarios = make(map[string]string)
	}
	s.scenarios[name] = state
}

// resetScenarios returns every @scenario to restclient.ScenarioStart.
func (s *Server) resetScenarios() {
	s.scenarioMu.Lock()
	clear(s.scenarios)
	s.scenarioMu.Unlock()
}

// ServeScenarios handles the admin /scenarios endpoint: GET lists the
// scenarios (see Scenarios), and PUT with {"name": ..., "state": ...} sets
// one (see SetScenario) and answers with the list.
func (s *Server) ServeScenarios(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		if !clearRequestAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var body struct{ Name, State string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		body.Name, body.State = strings.TrimSpace(body.Name), strings.TrimSpace(body.State)
		if body.Name == "" || strings.ContainsAny(body.Name+body.State, " \t") {
			http.Error(w, `need {"name": "scenario", "state": "state"} with names without spaces`, http.StatusBadRequest)
			return
		}
		s.SetScenario(body.Name, body.State)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.Scenarios())
}
