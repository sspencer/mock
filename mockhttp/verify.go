package mockhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Calls returns the retained requests that call names, oldest first. A call
// is either a method and path, such as "POST /users" (the path must match
// exactly; add ?query to require that exact query string too), or the name
// of the route that answered, such as "Create user". Callbacks are not
// counted.
func (s *Server) Calls(call string) []RequestEvent {
	method, target, ok := SplitCall(call)
	if !ok {
		return s.Requests(RequestFilter{Route: strings.TrimSpace(call)})
	}
	path, _, hasQuery := strings.Cut(target, "?")
	events := s.Requests(RequestFilter{Method: method, Path: path})
	if !hasQuery {
		return events
	}
	out := events[:0]
	for _, event := range events {
		if event.Request.URL == target {
			out = append(out, event)
		}
	}
	return out
}

// SplitCall splits a call such as "POST /users" into its method and target.
// ok is false when call is not an upper-case method followed by a path, so
// it names a route instead.
func SplitCall(call string) (method, target string, ok bool) {
	method, target, ok = strings.Cut(strings.TrimSpace(call), " ")
	target = strings.TrimSpace(target)
	if !ok || method == "" || !strings.HasPrefix(target, "/") {
		return "", "", false
	}
	for _, c := range method {
		if c < 'A' || c > 'Z' {
			return "", "", false
		}
	}
	return method, target, true
}

// Expectation is one check for Verify: how often a call was received, or,
// with AllMatched, that every request matched a route. Without Times,
// AtLeast or AtMost, a call must have been received at least once.
type Expectation struct {
	// Call names requests as Calls does: "POST /orders" or a route name.
	Call string `json:"call,omitempty"`
	// Times is the exact count; AtLeast and AtMost bound it.
	Times   *int `json:"times,omitempty"`
	AtLeast *int `json:"atLeast,omitempty"`
	AtMost  *int `json:"atMost,omitempty"`
	// AllMatched checks that no retained request went unmatched (requests
	// forwarded with --upstream count as unmatched).
	AllMatched bool `json:"allMatched,omitempty"`
}

// VerifyResult is the outcome of one Expectation.
type VerifyResult struct {
	Call       string `json:"call,omitempty"`
	AllMatched bool   `json:"allMatched,omitempty"`
	// Want describes the expected count, such as "exactly 1 time".
	Want string `json:"want"`
	// Got is the number of matching requests (unmatched ones for
	// AllMatched).
	Got int  `json:"got"`
	OK  bool `json:"ok"`
	// Message explains a failure in one line.
	Message string `json:"message,omitempty"`
}

// VerifyReport is the answer to POST /verify.
type VerifyReport struct {
	OK      bool           `json:"ok"`
	Results []VerifyResult `json:"results"`
	// Message is a readable summary: the failures, then the requests
	// received, when any expectation failed.
	Message string `json:"message"`
}

// Verify checks the expectations against the retained request log.
func (s *Server) Verify(expectations []Expectation) VerifyReport {
	report := VerifyReport{OK: true, Results: make([]VerifyResult, 0, len(expectations))}
	var failures []string
	for _, exp := range expectations {
		result := s.verifyOne(exp)
		report.Results = append(report.Results, result)
		if !result.OK {
			report.OK = false
			failures = append(failures, result.Message)
		}
	}
	if report.OK {
		report.Message = fmt.Sprintf("verify: %s passed", pluralize(len(expectations), "expectation"))
		return report
	}
	report.Message = fmt.Sprintf("verify: %d of %s failed\n  %s\n%s", len(failures), pluralize(len(expectations), "expectation"),
		strings.Join(failures, "\n  "), s.receivedSummary())
	return report
}

func (s *Server) verifyOne(exp Expectation) VerifyResult {
	if exp.AllMatched {
		unmatched := false
		events := s.Requests(RequestFilter{Matched: &unmatched})
		result := VerifyResult{AllMatched: true, Want: "no unmatched requests", Got: len(events), OK: len(events) == 0}
		if !result.OK {
			var calls []string
			for _, event := range events {
				calls = append(calls, event.Request.Method+" "+event.Request.URL)
			}
			result.Message = fmt.Sprintf("%s matched no route: %s", pluralize(len(events), "request"), strings.Join(calls, ", "))
		}
		return result
	}
	got := len(s.Calls(exp.Call))
	result := VerifyResult{Call: exp.Call, Want: exp.describe(), Got: got, OK: exp.accepts(got)}
	if !result.OK {
		result.Message = fmt.Sprintf("%s: called %s, want %s", exp.Call, pluralize(got, "time"), result.Want)
	}
	return result
}

func (e Expectation) accepts(got int) bool {
	switch {
	case e.Times != nil:
		return got == *e.Times
	case e.AtLeast == nil && e.AtMost == nil:
		return got >= 1
	}
	return (e.AtLeast == nil || got >= *e.AtLeast) && (e.AtMost == nil || got <= *e.AtMost)
}

func (e Expectation) describe() string {
	switch {
	case e.Times != nil && *e.Times == 0:
		return "never"
	case e.Times != nil:
		return "exactly " + pluralize(*e.Times, "time")
	case e.AtLeast != nil && e.AtMost != nil:
		return fmt.Sprintf("%d to %d times", *e.AtLeast, *e.AtMost)
	case e.AtLeast != nil:
		return "at least " + pluralize(*e.AtLeast, "time")
	case e.AtMost != nil:
		return "at most " + pluralize(*e.AtMost, "time")
	}
	return "at least 1 time"
}

func (e Expectation) check() error {
	if e.AllMatched {
		if e.Call != "" || e.Times != nil || e.AtLeast != nil || e.AtMost != nil {
			return errors.New(`"allMatched" cannot be combined with "call", "times", "atLeast" or "atMost"`)
		}
		return nil
	}
	if strings.TrimSpace(e.Call) == "" {
		return errors.New(`each expectation needs "call", such as "POST /orders" or a route name, or "allMatched": true`)
	}
	if e.Times != nil && (e.AtLeast != nil || e.AtMost != nil) {
		return fmt.Errorf(`%s: use "times" or "atLeast"/"atMost", not both`, e.Call)
	}
	for _, n := range []*int{e.Times, e.AtLeast, e.AtMost} {
		if n != nil && *n < 0 {
			return fmt.Errorf("%s: counts must not be negative", e.Call)
		}
	}
	if e.AtLeast != nil && e.AtMost != nil && *e.AtLeast > *e.AtMost {
		return fmt.Errorf(`%s: "atLeast" is above "atMost"`, e.Call)
	}
	return nil
}

// receivedSummary lists the retained requests for a failure message.
func (s *Server) receivedSummary() string {
	events := s.Requests(RequestFilter{})
	if len(events) == 0 {
		return "no requests received"
	}
	var b strings.Builder
	b.WriteString("requests received:")
	for _, event := range events {
		route := "no route"
		if event.Match.Route != nil {
			route = fmt.Sprintf("%q", event.Match.Route.Name)
		}
		fmt.Fprintf(&b, "\n  %s %s -> %d (%s)", event.Request.Method, event.Request.URL, event.Response.Status, route)
	}
	return b.String()
}

func pluralize(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// parseExpectations reads one expectation object or an array of them.
func parseExpectations(data []byte) ([]Expectation, error) {
	data = bytes.TrimSpace(data)
	var list []Expectation
	decode := func(v any) error {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		return dec.Decode(v)
	}
	if len(data) > 0 && data[0] == '[' {
		if err := decode(&list); err != nil {
			return nil, err
		}
	} else {
		var one Expectation
		if err := decode(&one); err != nil {
			return nil, err
		}
		list = []Expectation{one}
	}
	if len(list) == 0 {
		return nil, errors.New("no expectations given")
	}
	for _, exp := range list {
		if err := exp.check(); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// ServeVerify handles POST /verify: the body is an expectation such as
// {"call": "POST /orders", "times": 1}, or an array of them. It answers 200
// when all hold and 422 when any fails, with a VerifyReport as JSON, or only
// its message when the request accepts text/plain and not JSON.
func (s *Server) ServeVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !clearRequestAllowed(r) {
		http.Error(w, "forbidden: send Content-Type: application/json or X-Requested-With", http.StatusForbidden)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "cannot read the body: "+err.Error(), http.StatusBadRequest)
		return
	}
	expectations, err := parseExpectations(data)
	if err != nil {
		http.Error(w, "invalid expectations: "+err.Error(), http.StatusBadRequest)
		return
	}
	report := s.Verify(expectations)
	status := http.StatusOK
	if !report.OK {
		status = http.StatusUnprocessableEntity
	}
	w.Header().Set("Cache-Control", "no-store")
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/plain") && !strings.Contains(accept, "json") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, report.Message+"\n")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(report)
}
