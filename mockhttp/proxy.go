package mockhttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// upstreamProxy forwards requests that no route matches to a real service.
type upstreamProxy struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
	// via is this server's Via pseudonym. A request that already carries it has
	// come back from the upstream, so forwarding it again would loop.
	via string
}

// SetUpstream forwards requests that no route matches to target and returns
// its response, instead of answering 404. The request path is appended to
// target's path. A nil target restores 404s.
func (s *Server) SetUpstream(target *url.URL) {
	if target == nil {
		s.upstream.Store(nil)
		return
	}
	target = &url.URL{Scheme: target.Scheme, Host: target.Host, Path: target.Path, RawPath: target.RawPath}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	up := &upstreamProxy{target: target, via: "mock-" + s.session[:8]}
	up.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target) // Also sends the upstream's own Host header.
			pr.SetXForwarded()
			pr.Out.Header.Add("Via", fmt.Sprintf("%d.%d %s", pr.In.ProtoMajor, pr.In.ProtoMinor, up.via))
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if !errors.Is(err, context.Canceled) {
				logger := s.logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.Warn("upstream request failed", "upstream", target.String(), "method", r.Method, "path", r.URL.RequestURI(), "error", err)
			}
			http.Error(w, "mock: upstream request failed: "+err.Error(), http.StatusBadGateway)
		},
	}
	s.upstream.Store(up)
}

func (up *upstreamProxy) serve(w http.ResponseWriter, r *http.Request) {
	if up.looped(r) {
		http.Error(w, "mock: request loop detected: the upstream sent this request back to mock (check --upstream)", http.StatusLoopDetected)
		return
	}
	up.proxy.ServeHTTP(w, r)
}

func (up *upstreamProxy) looped(r *http.Request) bool {
	for _, header := range r.Header.Values("Via") {
		for entry := range strings.SplitSeq(header, ",") {
			if fields := strings.Fields(entry); len(fields) >= 2 && fields[1] == up.via {
				return true
			}
		}
	}
	return false
}
