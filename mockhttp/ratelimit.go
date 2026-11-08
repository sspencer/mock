package mockhttp

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sspencer/mock/restclient"
)

// rateLimiter is a sliding-window log of the requests a @rate-limit route
// accepted. It keeps at most the last limit timestamps.
type rateLimiter struct {
	mu    sync.Mutex
	times []time.Time
}

// allow records a request at now if fewer than limit were accepted in the
// window before it. Otherwise it returns how long until one expires.
// Rejected requests do not use up the budget.
func (l *rateLimiter) allow(now time.Time, limit int, window time.Duration) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-window)
	expired := 0
	for expired < len(l.times) && !l.times[expired].After(cutoff) {
		expired++
	}
	l.times = l.times[expired:]
	if len(l.times) > limit { // The limit was lowered by a reload.
		l.times = l.times[len(l.times)-limit:]
	}
	if len(l.times) == limit {
		return false, l.times[0].Add(window).Sub(now)
	}
	l.times = append(l.times, now)
	return true, 0
}

// limiter returns the rate limiter for a route. Limiters are keyed by route
// identity, so a reload that keeps the route keeps its budget.
func (s *Server) limiter(method *restclient.Method) *rateLimiter {
	key := routeKey(*method)
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	l := s.limiters[key]
	if l == nil {
		if s.limiters == nil {
			s.limiters = make(map[string]*rateLimiter)
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
		if !keep[key] {
			delete(s.limiters, key)
		}
	}
}

// overRateLimit answers 429 with Retry-After when a @rate-limit route has
// used its budget, and reports whether it did.
func (s *Server) overRateLimit(w http.ResponseWriter, method *restclient.Method) bool {
	if method.RateLimit <= 0 {
		return false
	}
	ok, wait := s.limiter(method).allow(time.Now(), method.RateLimit, method.RateWindow)
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
	http.Error(w, fmt.Sprintf("mock: rate limit of %s exceeded", describeRateLimit(*method)), http.StatusTooManyRequests)
	return true
}

// describeRateLimit gives a @rate-limit as written in canonical form, such as
// "5/1s", or "" for none.
func describeRateLimit(method restclient.Method) string {
	if method.RateLimit <= 0 {
		return ""
	}
	return strconv.Itoa(method.RateLimit) + "/" + method.RateWindow.String()
}
