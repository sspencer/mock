package mockhttp

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sspencer/mock/restclient"
)

// RouteInfo describes a fixture without reading potentially large body files.
type RouteInfo struct {
	ID       string `json:"id"`
	Revision uint64 `json:"revision"`
	Name     string `json:"name"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Query    string `json:"query,omitempty"`
	Port     int    `json:"port,omitempty"`
	Source   string `json:"source"`
	Line     int    `json:"line"`
	Status   int    `json:"status"`
	// Delay is "250ms", or a range such as "100ms..800ms".
	Delay string `json:"delay"`
	// Stream is the @stream pause between body chunks, when set.
	Stream string `json:"stream,omitempty"`
	// Compress is the @compress mode, when set.
	Compress string `json:"compress,omitempty"`
	// Fixture is the @fixture data file, as written, when set.
	Fixture string `json:"fixture,omitempty"`
	// Callback describes the @callback, such as "POST {{$json.callbackUrl}}
	// after 1s", when set.
	Callback string `json:"callback,omitempty"`
	// RateLimit is the @rate-limit budget, such as "5/1s", when set.
	RateLimit string `json:"rateLimit,omitempty"`
	// Proxy is the @proxy URL matching requests are forwarded to, when set.
	Proxy string `json:"proxy,omitempty"`
	// Fault is the @fault, such as "reset" or "truncate 25%", when set.
	Fault         string            `json:"fault,omitempty"`
	File          string            `json:"file,omitempty"`
	Headers       http.Header       `json:"headers"`
	MatchHeaders  http.Header       `json:"matchHeaders"`
	MatchQuery    url.Values        `json:"matchQuery,omitempty"`
	MatchPath     map[string]string `json:"matchPath,omitempty"`
	MatchBody     string            `json:"matchBody,omitempty"`
	MatchJSON     map[string]string `json:"matchJson,omitempty"`
	MatchXML      map[string]string `json:"matchXml,omitempty"`
	MatchForm     url.Values        `json:"matchForm,omitempty"`
	MatchCookies  url.Values        `json:"matchCookies,omitempty"`
	Variables     map[string]string `json:"variables"`
	Body          string            `json:"body"`
	BodyTruncated bool              `json:"bodyTruncated"`
	// StatusTemplate is the placeholder the status is rendered from, when set.
	StatusTemplate string `json:"statusTemplate,omitempty"`
	// Require is the @require reference, such as
	// "$fixture.users[id=$path.id]", when set.
	Require string `json:"require,omitempty"`
	// Missing is the @missing setting, "next" or a status, when set.
	Missing string `json:"missing,omitempty"`
	// ETag is set for @etag = auto.
	ETag bool `json:"etag,omitempty"`
	// DefaultContentType is the @default.header or -H Content-Type, sent
	// when the type cannot be guessed from the file extension or the body.
	DefaultContentType string `json:"defaultContentType,omitempty"`
	// Fallback is set for an @fallback = true route, tried after the others.
	Fallback bool `json:"fallback,omitempty"`
	// Chance is the @match.chance share, such as "20%", when set.
	Chance string `json:"chance,omitempty"`
	// Hits counts the requests this route has answered, carried across
	// reloads for unchanged routes.
	Hits uint64 `json:"hits"`
}

type MatchInfo struct {
	Route      *RouteInfo      `json:"route,omitempty"`
	Revision   uint64          `json:"revision"`
	Candidates []RouteMismatch `json:"candidates,omitempty"`
	// Upstream is where the request was forwarded: the --upstream base URL for
	// an unmatched request, or the @proxy target of the matched route.
	Upstream string `json:"upstream,omitempty"`
}

type RouteMismatch struct {
	Name    string   `json:"name"`
	Method  string   `json:"method"`
	Path    string   `json:"path"`
	Port    int      `json:"port,omitempty"`
	Source  string   `json:"source"`
	Line    int      `json:"line"`
	Reasons []string `json:"reasons"`
	score   int
}

type ConfigState struct {
	Revision    uint64 `json:"revision"`
	RouteCount  int    `json:"routeCount"`
	LastAttempt string `json:"lastAttempt"`
	LastSuccess string `json:"lastSuccess"`
	Loading     bool   `json:"loading"`
	Error       string `json:"error,omitempty"`
}

func describeRoute(method restclient.Method, index int, revision uint64) RouteInfo {
	body := method.Body
	truncated := len(body) > maxLoggedBodyBytes
	if truncated {
		body = body[:maxLoggedBodyBytes]
	}
	return RouteInfo{ID: fmt.Sprintf("%d:%d", revision, index), Revision: revision, Name: method.Name, Method: method.Method,
		Path: (&url.URL{Path: method.Path, RawPath: method.EscapedPath}).EscapedPath(), Query: method.Query.Encode(), Port: method.Port, Source: method.Source, Line: method.Line, Status: method.Status,
		Delay: describeDelay(method), Stream: describeStream(method), Fault: describeFault(method), Proxy: method.Proxy, Fixture: method.Fixture, Compress: method.Compress, Callback: describeCallback(method), RateLimit: describeRateLimit(method), DefaultContentType: method.DefaultContentType, StatusTemplate: method.StatusTemplate, Require: describeRequire(method), Missing: describeMissing(method), ETag: method.ETag, Fallback: method.Fallback, Chance: describeChance(method), File: method.File, Headers: method.Headers.Clone(), MatchHeaders: method.MatchHeaders.Clone(),
		MatchQuery: cloneValues(method.MatchQuery), MatchPath: cloneOptional(method.MatchPath),
		MatchBody: method.MatchBody, MatchJSON: cloneOptional(method.MatchJSON), MatchXML: cloneOptional(method.MatchXML), MatchForm: cloneValues(method.MatchForm), MatchCookies: cloneValues(method.MatchCookies),
		Variables: cloneVariables(method.Variables), Body: body, BodyTruncated: truncated}
}

func describeDelay(method restclient.Method) string {
	if method.DelayMax > method.Delay {
		return method.Delay.String() + ".." + method.DelayMax.String()
	}
	return method.Delay.String()
}

func describeStream(method restclient.Method) string {
	if method.Stream <= 0 {
		return ""
	}
	return method.Stream.String()
}

func describeFault(method restclient.Method) string {
	if method.Fault == "" || method.FaultRate >= 1 {
		return method.Fault
	}
	return method.Fault + " " + strconv.FormatFloat(method.FaultRate*100, 'f', -1, 64) + "%"
}

func cloneValues(values url.Values) url.Values {
	if len(values) == 0 {
		return nil
	}
	out := make(url.Values, len(values))
	for key, list := range values {
		out[key] = append([]string(nil), list...)
	}
	return out
}

// cloneOptional copies a matcher map, returning nil when it is empty so JSON
// omits it.
func cloneOptional(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	return maps.Clone(values)
}

func cloneVariables(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func (s *Server) BeginReload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Loading = true
	s.config.LastAttempt = time.Now().UTC().Format(time.RFC3339Nano)
}

func (s *Server) ReloadFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config.Loading = false
	s.config.Error = err.Error()
}

func (s *Server) ServeState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", 405)
		return
	}
	s.mu.Lock()
	state := s.config
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(state)
}

func mismatch(method restclient.Method, facts *requestFacts) RouteMismatch {
	r, actual, queryErr, headers := facts.r, facts.query, facts.queryErr, facts.headers
	pattern := routePattern(method)
	result := RouteMismatch{Name: method.Name, Method: method.Method, Path: pattern, Port: method.Port, Source: method.Source, Line: method.Line}
	if localPort := facts.localPort; !portMatches(method.Port, localPort) {
		result.Reasons = append(result.Reasons, fmt.Sprintf("Served on port %d; request arrived on port %d", method.Port, localPort))
		result.score += 3
	}
	params, pathOK := matchPath(pattern, r.URL.EscapedPath())
	if !pathOK {
		result.Reasons = append(result.Reasons, fmt.Sprintf("Path %q does not match %q", r.URL.EscapedPath(), pattern))
		result.score += 10
		result.score += abs(len(splitPath(pattern)) - len(splitPath(r.URL.EscapedPath())))
	}
	if !methodAnswers(method, r.Method) {
		result.Reasons = append(result.Reasons, fmt.Sprintf("Expected method %s; received %s", method.Method, r.Method))
		result.score += 4
	}
	if queryErr != nil {
		result.Reasons = append(result.Reasons, "Request query is malformed: "+queryErr.Error())
		result.score++
	} else {
		for _, key := range sortedKeys(method.Query) {
			expected := method.Query[key]
			if !queryMatches(url.Values{key: expected}, actual) {
				result.Reasons = append(result.Reasons, fmt.Sprintf("Query %s: expected %q; received %q", key, expected, actual[key]))
				result.score++
			}
		}
		for _, key := range sortedKeys(method.MatchQuery) {
			for _, expected := range method.MatchQuery[key] {
				matcher := restclient.MustMatcher(expected)
				if matcher.Match(actual[key]) {
					continue
				}
				result.Reasons = append(result.Reasons, "Query "+key+": "+matcherReason(matcher, actual[key], true))
				result.score++
			}
		}
	}
	if pathOK {
		for _, key := range slices.Sorted(maps.Keys(method.MatchPath)) {
			matcher := restclient.MustMatcher(method.MatchPath[key])
			if value, ok := params[key]; !ok || !matcher.Match([]string{value}) {
				result.Reasons = append(result.Reasons, "Path parameter "+key+": "+matcherReason(matcher, []string{params[key]}, true))
				result.score++
			}
		}
	}
	for _, key := range sortedKeys(method.MatchHeaders) {
		for _, expected := range method.MatchHeaders[key] {
			matcher := restclient.MustMatcher(expected)
			actualValues := headers.Values(key)
			if matcher.Match(actualValues) {
				continue
			}
			// Do not repeat request header values (often credentials) in diagnostic summaries.
			result.Reasons = append(result.Reasons, "Header "+key+": "+matcherReason(matcher, actualValues, false))
			result.score++
		}
	}
	for _, name := range sortedKeys(method.MatchCookies) {
		for _, expected := range method.MatchCookies[name] {
			matcher := restclient.MustMatcher(expected)
			actualValues := facts.cookieValues(name)
			if matcher.Match(actualValues) {
				continue
			}
			// Cookies often carry sessions; describe the requirement, not the value.
			result.Reasons = append(result.Reasons, "Cookie "+name+": "+matcherReason(matcher, actualValues, false))
			result.score++
		}
	}
	if method.MatchBody != "" && !facts.bodyMatches(method.MatchBody) {
		reason := "Body: "
		if facts.body.unavailable {
			reason += errBodyUnavailable.Error()
		} else {
			// The body may be large; describe the requirement, not the body.
			reason += matcherReason(restclient.MustBodyMatcher(method.MatchBody), facts.bodyValues(), false)
		}
		result.Reasons = append(result.Reasons, reason)
		result.score++
	}
	for _, path := range slices.Sorted(maps.Keys(method.MatchJSON)) {
		if facts.jsonMatches(path, method.MatchJSON[path]) {
			continue
		}
		values, err := facts.jsonValues(path)
		reason := "JSON " + path + ": "
		if err != nil {
			reason += err.Error()
		} else {
			reason += matcherReason(restclient.MustMatcher(method.MatchJSON[path]), values, true)
		}
		result.Reasons = append(result.Reasons, reason)
		result.score++
	}
	for _, path := range slices.Sorted(maps.Keys(method.MatchXML)) {
		if facts.xmlMatches(path, method.MatchXML[path]) {
			continue
		}
		values, err := facts.xmlValues(path)
		reason := "XML " + path + ": "
		if err != nil {
			reason += err.Error()
		} else {
			reason += matcherReason(restclient.MustMatcher(method.MatchXML[path]), values, true)
		}
		result.Reasons = append(result.Reasons, reason)
		result.score++
	}
	for _, name := range sortedKeys(method.MatchForm) {
		for _, expected := range method.MatchForm[name] {
			if facts.formMatches(name, expected) {
				continue
			}
			values, err := facts.formValues(name)
			reason := "Form " + name + ": "
			if err != nil {
				reason += err.Error()
			} else {
				reason += matcherReason(restclient.MustMatcher(expected), values, true)
			}
			result.Reasons = append(result.Reasons, reason)
			result.score++
		}
	}
	return result
}

// maxReasonValues caps how many received values a mismatch reason quotes.
const maxReasonValues = 5

// matcherReason explains why actual values failed a matcher. Received values
// are quoted only when showValues is set.
func matcherReason(matcher restclient.Matcher, actual []string, showValues bool) string {
	received := ""
	if showValues {
		// A @json path with "*" can select a whole array; show the start of it.
		if extra := len(actual) - maxReasonValues; extra > 0 {
			received = fmt.Sprintf("; received %q and %d more", actual[:maxReasonValues], extra)
		} else {
			received = fmt.Sprintf("; received %q", actual)
		}
	}
	switch {
	case matcher.Kind == restclient.MatchAbsent:
		return "must be absent" + received
	case len(actual) == 0:
		return "required value is missing"
	case matcher.Kind == restclient.MatchRegexp:
		return fmt.Sprintf("does not match pattern %q", matcher.Pattern()) + received
	case matcher.Kind == restclient.MatchNotRegexp:
		if strings.TrimSpace(strings.Join(actual, "")) == "" {
			return "must not be blank"
		}
		return fmt.Sprintf("must not match pattern %q", matcher.Pattern()) + received
	case matcher.Kind == restclient.MatchNotExact:
		if strings.TrimSpace(strings.Join(actual, "")) == "" {
			return "must not be blank"
		}
		if showValues {
			return fmt.Sprintf("must not be %q", matcher.Raw[1:]) + received
		}
		return "value is excluded"
	case matcher.Kind == restclient.MatchPresent:
		return "must not be blank"
	}
	if showValues {
		return fmt.Sprintf("expected %q", matcher.Raw) + received
	}
	return "required value does not match"
}
func sortedKeys[M ~map[string][]string](values M) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func closestRoutes(methods []restclient.Method, facts *requestFacts) []RouteMismatch {
	candidates := make([]RouteMismatch, 0, len(methods))
	for _, method := range methods {
		candidate := mismatch(method, facts)
		if len(candidate.Reasons) == 0 {
			// It matched, but a @require, @missing = next or @match.chance
			// draw turned the request away.
			if values, ok := facts.match(&method, facts.r.Method); ok {
				path, _ := matchPath(routePattern(method), facts.r.URL.EscapedPath())
				if reason := unavailable(&method, templateValues{values: values, path: path, facts: facts}); reason != "" {
					candidate.Reasons = []string{reason}
				}
			}
			if len(candidate.Reasons) == 0 && method.Chance > 0 {
				candidate.Reasons = []string{"Skipped by @match.chance = " + describeChance(method) + " for this request"}
			}
		}
		candidates = append(candidates, candidate)
	}
	slices.SortStableFunc(candidates, func(a, b RouteMismatch) int { return a.score - b.score })
	if len(candidates) > 5 {
		candidates = candidates[:5]
	}
	for i := range candidates {
		if len(candidates[i].Reasons) == 0 {
			candidates[i].Reasons = []string{"No matching response available"}
		}
	}
	return candidates
}
