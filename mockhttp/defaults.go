package mockhttp

import (
	"time"

	"github.com/sspencer/mock/restclient"
)

// RouteDefaults is the latency and fault applied to every matched route that
// does not set its own (the --delay and --fault flags). A route's @delay or
// @fault, even "@delay = 0" or "@fault = reset 0%", takes precedence.
type RouteDefaults struct {
	// Delay is the fixed delay, the low end of a range up to DelayMax, or
	// the median of a lognormal delay with shape DelaySigma.
	Delay, DelayMax time.Duration
	DelaySigma      float64
	// Throttle is the transfer rate in bytes per second for routes without
	// a @throttle of their own; 0 is unlimited.
	Throttle int64
	// Fault is "reset" or "truncate"; FaultRate is the share of responses
	// it applies to, between 0 and 1.
	Fault     string
	FaultRate float64
}

// SetRouteDefaults sets the latency and fault for routes that set none. It
// is safe to call while serving.
func (s *Server) SetRouteDefaults(d RouteDefaults) {
	if d == (RouteDefaults{}) {
		s.defaults.Store(nil)
		return
	}
	s.defaults.Store(&d)
}

// withDefaults returns method, or a copy carrying the route defaults where
// the route sets no @delay or @fault of its own.
func (s *Server) withDefaults(method *restclient.Method) *restclient.Method {
	d := s.defaults.Load()
	if d == nil {
		return method
	}
	_, ownDelay := method.Variables["delay"]
	useDelay := !ownDelay && d.Delay+d.DelayMax > 0
	useFault := method.Fault == "" && d.Fault != ""
	_, ownThrottle := method.Variables["throttle"]
	useThrottle := !ownThrottle && d.Throttle > 0
	if !useDelay && !useFault && !useThrottle {
		return method
	}
	copied := *method
	if useDelay {
		copied.Delay, copied.DelayMax, copied.DelaySigma = d.Delay, d.DelayMax, d.DelaySigma
	}
	if useThrottle {
		copied.Throttle = d.Throttle
	}
	if useFault {
		copied.Fault, copied.FaultRate = d.Fault, d.FaultRate
	}
	return &copied
}

// DelayString describes the default delay, such as "250ms" or
// "50ms..300ms", or "" when there is none.
func (d RouteDefaults) DelayString() string {
	if d.Delay+d.DelayMax == 0 {
		return ""
	}
	return describeDelay(restclient.Method{Delay: d.Delay, DelayMax: d.DelayMax, DelaySigma: d.DelaySigma})
}

// ThrottleString describes the default throttle, such as "32kb/s", or "".
func (d RouteDefaults) ThrottleString() string {
	return describeThrottle(d.Throttle)
}

// FaultString describes the default fault, such as "reset 5%", or "".
func (d RouteDefaults) FaultString() string {
	return describeFault(restclient.Method{Fault: d.Fault, FaultRate: d.FaultRate})
}
