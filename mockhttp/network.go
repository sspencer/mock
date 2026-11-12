package mockhttp

import (
	"context"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"time"
)

// throttleTick is how often a throttled response writes a slice of its body.
const throttleTick = 100 * time.Millisecond

// throttledWriter paces a response body at rate bytes per second: it writes
// one slice per tick and flushes it, so the client sees a slow, steady
// download. Headers and status pass straight through.
type throttledWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	ctx        context.Context
	rate       int64
}

func (t *throttledWriter) Write(p []byte) (int, error) {
	chunk := max(1, int(t.rate*int64(throttleTick)/int64(time.Second)))
	tick := time.Duration(chunk) * time.Second / time.Duration(t.rate)
	written := 0
	for written < len(p) {
		if written > 0 && !sleep(t.ctx, tick) {
			return written, t.ctx.Err()
		}
		end := min(written+chunk, len(p))
		_ = t.controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		n, err := t.ResponseWriter.Write(p[written:end])
		written += n
		if err != nil {
			return written, err
		}
		_ = t.controller.Flush()
	}
	return written, nil
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (t *throttledWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// describeThrottle formats a rate in bytes per second as written in
// @throttle, such as "32kb/s", "1.5mb/s" or "100b/s", or "" for none.
func describeThrottle(rate int64) string {
	switch {
	case rate <= 0:
		return ""
	case rate >= 1<<20 && rate%(1<<10) == 0:
		return strconv.FormatFloat(float64(rate)/(1<<20), 'f', -1, 64) + "mb/s"
	case rate >= 1<<10:
		return strconv.FormatFloat(float64(rate)/(1<<10), 'f', -1, 64) + "kb/s"
	}
	return strconv.FormatInt(rate, 10) + "b/s"
}

// abortsConnection reports whether a fault answers without an HTTP
// response, so the request is logged before it happens.
func abortsConnection(fault string) bool {
	switch fault {
	case "reset", "hang", "empty", "garbage":
		return true
	}
	return false
}

// injectConnectionFault breaks the connection for reset, hang, empty and
// garbage. It never returns normally: net/http must not send a response
// afterwards.
func (s *Server) injectConnectionFault(w http.ResponseWriter, r *http.Request, fault string) {
	switch fault {
	case "reset":
		resetConnection(w)
	case "hang":
		// A replayed request has no client of its own to time out; the
		// admin request would wait forever.
		if resendStateOf(r) == nil {
			select {
			case <-r.Context().Done():
			case <-s.callbackCtx.Done():
			}
		}
	case "empty":
		if conn, _, err := http.NewResponseController(w).Hijack(); err == nil {
			_ = conn.Close()
		}
	case "garbage":
		if conn, buffered, err := http.NewResponseController(w).Hijack(); err == nil {
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_, _ = buffered.Write(garbageBytes())
			_ = buffered.Flush()
			closeConn(conn)
		}
	}
	panic(http.ErrAbortHandler)
}

// garbageBytes is what @fault = garbage sends instead of a response: random
// bytes that no HTTP client can parse, starting with a NUL so they cannot be
// read as an HTTP/0.9 body either.
func garbageBytes() []byte {
	b := make([]byte, 64+rand.IntN(192))
	for i := range b {
		b[i] = byte(rand.Uint32())
	}
	b[0] = 0
	return b
}

func closeConn(conn net.Conn) { _ = conn.Close() }
