package mockhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// AllRoutes is the route selector that applies a fault override to every
// route.
const AllRoutes = "*"

// FaultOverride is a route switch set at run time for a route or for all
// routes (POST /faults, Server.SetFault, the admin UI's route switches).
// Delay, Fault and Throttle win over the route's own @delay, @fault or
// @throttle and over the --delay, --fault and --throttle defaults; an empty
// field leaves them. Use "0s" for no delay and "none" for no fault or no
// throttle. Off turns the route off, so later routes answer as if it did not
// match, and Status forces the response status.
type FaultOverride struct {
	Delay    string `json:"delay,omitempty"`
	Fault    string `json:"fault,omitempty"`
	Throttle string `json:"throttle,omitempty"`
	Off      bool   `json:"off,omitempty"`
	Status   int    `json:"status,omitempty"`
}

// FaultInfo is one active override, as GET /faults lists it.
type FaultInfo struct {
	// Route is the route's name, or "*" for every route.
	Route  string `json:"route"`
	Source string `json:"source,omitempty"`
	Line   int    `json:"line,omitempty"`
	FaultOverride
}

// faultEntry is a parsed override and the route it was set for.
type faultEntry struct {
	info        FaultInfo
	hasDelay    bool
	delay       restclient.Delay
	hasFault    bool
	fault       string
	faultRate   float64
	hasThrottle bool
	throttle    int64
	off         bool
	status      int
}

// parseOverride checks an override and parses its fields.
func parseOverride(o FaultOverride) (faultEntry, error) {
	entry := faultEntry{info: FaultInfo{FaultOverride: o}}
	var errs []error
	if text := strings.TrimSpace(o.Delay); text != "" {
		delay, err := restclient.ParseDelay(text)
		errs = append(errs, prefixError("delay", err))
		entry.hasDelay, entry.delay = true, delay
	}
	if text := strings.TrimSpace(o.Fault); text != "" {
		entry.hasFault = true
		if !strings.EqualFold(text, "none") {
			fault, rate, err := restclient.ParseFault(text)
			errs = append(errs, prefixError("fault", err))
			entry.fault, entry.faultRate = fault, rate
		}
	}
	if text := strings.TrimSpace(o.Throttle); text != "" {
		entry.hasThrottle = true
		if !strings.EqualFold(text, "none") {
			rate, err := restclient.ParseThrottle(text)
			errs = append(errs, prefixError("throttle", err))
			entry.throttle = rate
		}
	}
	if o.Status != 0 && (o.Status < 200 || o.Status > 999) {
		errs = append(errs, fmt.Errorf("status %d must be between 200 and 999", o.Status))
	}
	entry.off, entry.status = o.Off, o.Status
	if err := errors.Join(errs...); err != nil {
		return faultEntry{}, err
	}
	if !entry.hasDelay && !entry.hasFault && !entry.hasThrottle && !entry.off && entry.status == 0 {
		return faultEntry{}, errors.New(`set at least one of delay, fault, throttle, off or status (use "0s" or "none" to switch a delay, fault or throttle off)`)
	}
	return entry, nil
}

func prefixError(field string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %w", field, err)
}

// SetFault switches on an override for routes chosen by route: "*" for every
// route, a route ID from /routes, or a route name (every route with that
// name). It replaces an earlier override for the same routes and lasts until
// ClearFaults, across reloads that keep the route. It returns the names of
// the routes it applies to.
func (s *Server) SetFault(route string, o FaultOverride) ([]string, error) {
	entry, err := parseOverride(o)
	if err != nil {
		return nil, err
	}
	keys, infos, err := s.selectRoutes(route)
	if err != nil {
		return nil, err
	}
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	if s.faults == nil {
		s.faults = make(map[string]faultEntry)
	}
	var names []string
	for i, key := range keys {
		e := entry
		e.info.Route, e.info.Source, e.info.Line = infos[i].Route, infos[i].Source, infos[i].Line
		s.faults[key] = e
		names = append(names, infos[i].Route)
	}
	s.faultCount.Store(int64(len(s.faults)))
	return names, nil
}

// ClearFaults switches off the overrides chosen by route (as for SetFault),
// or all of them when route is "". It reports how many it removed.
func (s *Server) ClearFaults(route string) (int, error) {
	var keys []string
	if route != "" {
		var err error
		if keys, _, err = s.selectRoutes(route); err != nil {
			return 0, err
		}
	}
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	removed := 0
	if route == "" {
		removed = len(s.faults)
		clear(s.faults)
	}
	for _, key := range keys {
		if _, ok := s.faults[key]; ok {
			delete(s.faults, key)
			removed++
		}
	}
	s.faultCount.Store(int64(len(s.faults)))
	return removed, nil
}

// Faults lists the active overrides, "*" first and then by source line.
func (s *Server) Faults() []FaultInfo {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	out := make([]FaultInfo, 0, len(s.faults))
	for _, entry := range s.faults {
		out = append(out, entry.info)
	}
	slices.SortFunc(out, func(a, b FaultInfo) int {
		switch {
		case a.Route == AllRoutes:
			return -1
		case b.Route == AllRoutes:
			return 1
		case a.Source != b.Source:
			return strings.Compare(a.Source, b.Source)
		}
		return a.Line - b.Line
	})
	return out
}

// selectRoutes resolves a route selector to override keys: "*", or the
// routeKey of each current route with that ID or name.
func (s *Server) selectRoutes(route string) ([]string, []FaultInfo, error) {
	route = strings.TrimSpace(route)
	if route == AllRoutes {
		return []string{AllRoutes}, []FaultInfo{{Route: AllRoutes}}, nil
	}
	if route == "" {
		return nil, nil, errors.New(`route is required: a route id from /routes, a route name, or "*" for every route`)
	}
	if method, ok := s.routeByID(route); ok {
		return []string{routeKey(method)}, []FaultInfo{{Route: method.Name, Source: method.Source, Line: method.Line}}, nil
	}
	s.mu.Lock()
	methods := s.methods
	s.mu.Unlock()
	var keys []string
	var infos []FaultInfo
	for _, method := range methods {
		if method.Name == route {
			keys = append(keys, routeKey(method))
			infos = append(infos, FaultInfo{Route: method.Name, Source: method.Source, Line: method.Line})
		}
	}
	if len(keys) == 0 {
		return nil, nil, fmt.Errorf("no route has the id or name %q", route)
	}
	return keys, infos, nil
}

// faultFor returns the override for a route: its own, else the one for all
// routes.
func (s *Server) faultFor(method restclient.Method) (faultEntry, bool) {
	if s.faultCount.Load() == 0 {
		return faultEntry{}, false
	}
	key := routeKey(method)
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	if entry, ok := s.faults[key]; ok {
		return entry, true
	}
	entry, ok := s.faults[AllRoutes]
	return entry, ok
}

// withOverride returns method, or a copy with the route's run-time override
// applied.
func (s *Server) withOverride(method *restclient.Method) *restclient.Method {
	entry, ok := s.faultFor(*method)
	if !ok {
		return method
	}
	copied := *method
	if entry.hasDelay {
		copied.Delay, copied.DelayMax, copied.DelaySigma = entry.delay.Min, entry.delay.Max, entry.delay.Sigma
	}
	if entry.hasFault {
		copied.Fault, copied.FaultRate = entry.fault, entry.faultRate
	}
	if entry.hasThrottle {
		copied.Throttle = entry.throttle
	}
	if entry.status != 0 {
		copied.Status, copied.StatusTemplate = entry.status, ""
	}
	return &copied
}

// switchedOff reports whether a route is turned off by a run-time switch.
func (s *Server) switchedOff(method restclient.Method) bool {
	entry, ok := s.faultFor(method)
	return ok && entry.off
}

// describeOverride summarizes a route's active override, such as
// "delay 2s · fault reset 50%", or "".
func (s *Server) describeOverride(method restclient.Method) string {
	entry, ok := s.faultFor(method)
	if !ok {
		return ""
	}
	var parts []string
	if entry.off {
		parts = append(parts, "off")
	}
	if entry.status != 0 {
		parts = append(parts, fmt.Sprintf("status %d", entry.status))
	}
	for _, field := range [][2]string{{"delay", entry.info.Delay}, {"fault", entry.info.Fault}, {"throttle", entry.info.Throttle}} {
		if field[1] != "" {
			parts = append(parts, field[0]+" "+strings.TrimSpace(field[1]))
		}
	}
	text := strings.Join(parts, " · ")
	if entry.info.Route == AllRoutes {
		text += " (all routes)"
	}
	return text
}

// faultRequest is the body of POST /faults.
type faultRequest struct {
	Route string `json:"route"`
	FaultOverride
}

// ServeFaults handles the admin /faults endpoint. GET lists the active
// overrides; POST {"route": ..., "delay": ..., "fault": ..., "throttle": ...}
// sets one; DELETE ?route=... clears one, or all without a route.
func (s *Server) ServeFaults(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Faults())
	case http.MethodPost:
		if !clearRequestAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var req faultRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			http.Error(w, "invalid fault request: "+err.Error(), http.StatusBadRequest)
			return
		}
		names, err := s.SetFault(req.Route, req.FaultOverride)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"routes": names})
	case http.MethodDelete:
		if !clearRequestAllowed(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		removed, err := s.ClearFaults(r.URL.Query().Get("route"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"removed": removed})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// switchedOffAt reports whether the route a near-miss candidate describes is
// switched off.
func (s *Server) switchedOffAt(methods []restclient.Method, candidate RouteMismatch) bool {
	if s.faultCount.Load() == 0 {
		return false
	}
	for _, method := range methods {
		if method.Source == candidate.Source && method.Line == candidate.Line && method.Name == candidate.Name {
			return s.switchedOff(method)
		}
	}
	return false
}

// ownSwitch returns the run-time switch set for this route itself, or nil.
func (s *Server) ownSwitch(method restclient.Method) *FaultOverride {
	if s.faultCount.Load() == 0 {
		return nil
	}
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	entry, ok := s.faults[routeKey(method)]
	if !ok {
		return nil
	}
	o := entry.info.FaultOverride
	return &o
}
