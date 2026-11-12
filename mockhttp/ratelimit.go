package mockhttp

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sspencer/mock/restclient"
)

// rateLimiter is a sliding-window log of the requests a @rate-limit route
// accepted. It keeps at most the last limit timestamps.
type rateLimiter struct {
	mu     sync.Mutex
	times  []time.Time
	window time.Duration
}

// rateDecision is the outcome of one request against a budget: whether it
// was allowed, how many requests remain in the window, and how long until
// the oldest counted request leaves it (and frees a request).
type rateDecision struct {
	ok        bool
	remaining int
	reset     time.Duration
}

// allow records a request at now if fewer than limit were accepted in the
// window before it. Rejected requests do not use up the budget.
func (l *rateLimiter) allow(now time.Time, limit int, window time.Duration) rateDecision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.window = window
	cutoff := now.Add(-window)
	expired := 0
	for expired < len(l.times) && !l.times[expired].After(cutoff) {
		expired++
	}
	l.times = l.times[expired:]
	if len(l.times) > limit { // The limit was lowered by a reload.
		l.times = l.times[len(l.times)-limit:]
	}
	ok := len(l.times) < limit
	if ok {
		l.times = append(l.times, now)
	}
	return rateDecision{ok: ok, remaining: limit - len(l.times), reset: l.times[0].Add(window).Sub(now)}
}

// idle reports whether every request the limiter counted has left its
// window, so dropping it changes nothing.
func (l *rateLimiter) idle(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.times) == 0 || !l.times[len(l.times)-1].Add(l.window).After(now)
}

// maxLimiters is how many budgets (routes times @rate-limit.key values) are
// kept before idle ones are swept.
const maxLimiters = 10000

// limiter returns the rate limiter for a route and @rate-limit.key value.
// Limiters are keyed by route identity, so a reload that keeps the route
// keeps its budgets.
func (s *Server) limiter(method *restclient.Method, client string) *rateLimiter {
	key := routeKey(*method) + "\x01" + client
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	l := s.limiters[key]
	if l == nil {
		if s.limiters == nil {
			s.limiters = make(map[string]*rateLimiter)
		}
		if len(s.limiters) >= maxLimiters {
			now := time.Now()
			for k, old := range s.limiters {
				if old.idle(now) {
					delete(s.limiters, k)
				}
			}
		}
		l = new(rateLimiter)
		s.limiters[key] = l
	}
	return l
}

// pruneLimiters drops limiters of routes no longer configured with a
// @rate-limit.
func (s *Server) pruneLimiters(methods []restclient.Method) {
	keep := make(map[string]bool)
	for _, method := range methods {
		if method.RateLimit > 0 {
			keep[routeKey(method)] = true
		}
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	for key := range s.limiters {
		route, _, _ := strings.Cut(key, "\x01")
		if !keep[route] {
			delete(s.limiters, key)
		}
	}
}

// rateLimit counts a request against a @rate-limit route's budget (per
// @rate-limit.key value, when set). It returns the X-RateLimit-* headers for
// the response; when the budget is used up it has already answered 429 with
// Retry-After and over is true.
func (s *Server) rateLimit(w http.ResponseWriter, method *restclient.Method, values templateValues) (headers http.Header, over bool) {
	if method.RateLimit <= 0 {
		return nil, false
	}
	client := ""
	if method.RateLimitKey != "" {
		client, _ = newRenderer(method, values).text(method.RateLimitKey, false)
	}
	decision := s.limiter(method, client).allow(time.Now(), method.RateLimit, method.RateWindow)
	headers = http.Header{
		"X-Ratelimit-Limit":     {strconv.Itoa(method.RateLimit)},
		"X-Ratelimit-Remaining": {strconv.Itoa(decision.remaining)},
		"X-Ratelimit-Reset":     {strconv.Itoa(max(0, int(math.Ceil(decision.reset.Seconds()))))},
	}
	if decision.ok {
		return headers, false
	}
	for name, value := range headers {
		w.Header()[name] = value
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(decision.reset.Seconds())))))
	http.Error(w, fmt.Sprintf("mock: rate limit of %s exceeded", describeRateLimit(*method)), http.StatusTooManyRequests)
	return nil, true
}

// describeRateLimit gives a @rate-limit as written in canonical form, such as
// "5/1s", or "" for none.
func describeRateLimit(method restclient.Method) string {
	if method.RateLimit <= 0 {
		return ""
	}
	return strconv.Itoa(method.RateLimit) + "/" + method.RateWindow.String()
}
