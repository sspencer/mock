package restclient

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseDelayDistributions(t *testing.T) {
	delay, err := ParseDelay("lognormal 90ms 0.4")
	if err != nil || delay.Min != 90*time.Millisecond || delay.Sigma != 0.4 || delay.Max != 0 {
		t.Fatalf("lognormal = %+v, %v", delay, err)
	}
	delay, err = ParseDelay(" P50 80ms  p99 900ms ")
	if err != nil || delay.Min != 80*time.Millisecond {
		t.Fatalf("p50/p99 = %+v, %v", delay, err)
	}
	if p99 := delay.P99(); math.Abs(float64(p99-900*time.Millisecond)) > float64(time.Millisecond) {
		t.Errorf("P99 = %s, want 900ms", p99)
	}
	for _, bad := range []string{"lognormal 0s 0.4", "lognormal 90ms 0", "lognormal 90ms 4", "lognormal 90ms", "p50 80ms p99 80ms", "p50 1ms p99 1h", "p50 80ms p90 900ms"} {
		if _, err := ParseDelay(bad); err == nil {
			t.Errorf("ParseDelay(%q) error = nil", bad)
		}
	}
}

func TestParseThrottle(t *testing.T) {
	for raw, want := range map[string]int64{"32kb/s": 32 * 1024, "1.5MB/s": 3 << 19, " 512b/s ": 512, "0.5kb/s": 512} {
		if got, err := ParseThrottle(raw); err != nil || got != want {
			t.Errorf("ParseThrottle(%q) = %d, %v; want %d", raw, got, err, want)
		}
	}
	for _, bad := range []string{"", "32kb", "32/s", "0kb/s", "-1kb/s", "fast/s", "0.1b/s", "32kbps"} {
		if _, err := ParseThrottle(bad); err == nil {
			t.Errorf("ParseThrottle(%q) error = nil", bad)
		}
	}
}

func TestParseNetworkDirectives(t *testing.T) {
	methods, err := Parse("test.http", strings.NewReader(`### Download
@throttle = 64kb/s
@delay = p50 50ms p99 400ms
@fault = hang 10%
@rate-limit = 5/1m
@rate-limit.key = {{$header.Authorization}}
GET /download
`))
	if err != nil {
		t.Fatal(err)
	}
	m := methods[0]
	if m.Throttle != 64*1024 || m.DelaySigma == 0 || m.Delay != 50*time.Millisecond || m.Fault != "hang" || m.FaultRate != 0.1 || m.RateLimitKey != "{{$header.Authorization}}" {
		t.Errorf("method = throttle %d, delay %s σ%v, fault %s %v, key %q", m.Throttle, m.Delay, m.DelaySigma, m.Fault, m.FaultRate, m.RateLimitKey)
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Errorf("unused = %v", unused)
	}
	for _, fault := range []string{"empty", "garbage 50%", "HANG"} {
		if _, _, err := ParseFault(fault); err != nil {
			t.Errorf("ParseFault(%q) = %v", fault, err)
		}
	}
	for _, bad := range []string{"@throttle = quick", "@rate-limit.key =", "@fault = explode"} {
		if _, err := Parse("test.http", strings.NewReader("### X\n"+bad+"\nGET /x\n")); err == nil || !strings.Contains(err.Error(), "test.http:2:") {
			t.Errorf("%q error = %v", bad, err)
		}
	}
}
