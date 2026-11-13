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
	"sync"
	"time"

	"github.com/sspencer/mock/restclient"
)

// upstreamProxy forwards requests that no route matches to a real service.
type upstreamProxy struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
}

// proxyTransport is shared by --upstream and every @proxy route, so
// connections to the same host are reused.
var proxyTransport = sync.OnceValue(func() http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	return transport
})

// via is this server's Via pseudonym. A request that already carries it has
// come back from a proxy target, so forwarding it again would loop.
func (s *Server) via() string {
	return "mock-" + s.session[:8]
}

// SetUpstream forwards requests that no route matches to target and returns
// its response, instead of answering 404. The request path is appended to
// target's path. A nil target restores 404s.
func (s *Server) SetUpstream(target *url.URL) {
	s.SetUpstreamWith(target, UpstreamOptions{})
}

// UpstreamOptions change the requests SetUpstreamWith forwards.
type UpstreamOptions struct {
	// Headers are set on every forwarded request, replacing the client's
	// values of the same name, such as credentials the client does not have.
	// They are not shown in the request log.
	Headers http.Header
	// StripPrefix is removed from the start of the request path before it is
	// appended to the target's path, when the path starts with it at a
	// segment boundary: "/api" turns /api/users into /users.
	StripPrefix string
}

// SetUpstreamWith is SetUpstream with options for the forwarded requests.
func (s *Server) SetUpstreamWith(target *url.URL, opts UpstreamOptions) {
	if target == nil {
		s.upstream.Store(nil)
		return
	}
	target = &url.URL{Scheme: target.Scheme, Host: target.Host, Path: target.Path, RawPath: target.RawPath}
	headers := opts.Headers.Clone()
	strip := strings.TrimSuffix(opts.StripPrefix, "/")
	s.upstream.Store(&upstreamProxy{
		target: target,
		proxy: s.reverseProxy(target.String(), func(pr *httputil.ProxyRequest) {
			if strip != "" {
				stripPathPrefix(pr.Out.URL, strip)
			}
			pr.SetURL(target) // Also sends the upstream's own Host header.
			for name, values := range headers {
				pr.Out.Header[name] = append([]string(nil), values...)
			}
		}),
	})
}

// stripPathPrefix removes prefix from u's path when the path is the prefix
// or continues it with a slash.
func stripPathPrefix(u *url.URL, prefix string) {
	escaped := u.EscapedPath()
	rest, ok := strings.CutPrefix(escaped, prefix)
	if !ok || rest != "" && !strings.HasPrefix(rest, "/") {
		return
	}
	if rest == "" {
		rest = "/"
	}
	path, err := url.PathUnescape(rest)
	if err != nil {
		return
	}
	u.Path, u.RawPath = path, ""
	if u.EscapedPath() != rest {
		u.RawPath = rest
	}
}

// reverseProxy builds a proxy that applies route, then adds the forwarding
// headers. Failures answer 502 and are logged against label.
func (s *Server) reverseProxy(label string, route func(*httputil.ProxyRequest)) *httputil.ReverseProxy {
	via := s.via()
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			route(pr)
			pr.SetXForwarded()
			pr.Out.Header.Add("Via", fmt.Sprintf("%d.%d %s", pr.In.ProtoMajor, pr.In.ProtoMinor, via))
		},
		Transport: proxyTransport(),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if !errors.Is(err, context.Canceled) {
				logger := s.logger
				if logger == nil {
					logger = slog.Default()
				}
				logger.Warn("upstream request failed", "upstream", label, "method", r.Method, "path", r.URL.RequestURI(), "error", err)
			}
			http.Error(w, "mock: upstream request failed: "+err.Error(), http.StatusBadGateway)
		},
	}
}

func (s *Server) serveUpstream(up *upstreamProxy, w http.ResponseWriter, r *http.Request) {
	if s.looped(r) {
		http.Error(w, "mock: request loop detected: the upstream sent this request back to mock (check --upstream)", http.StatusLoopDetected)
		return
	}
	up.proxy.ServeHTTP(w, r)
}

// proxyTarget works out where a @proxy route sends r. A plain @proxy URL gets
// the request path appended, like --upstream. A URL with placeholders is
// rendered with the request's values and used as the exact target; the
// request's query is kept unless the rendered URL has its own. exact reports
// which form applied.
func proxyTarget(method *restclient.Method, render *renderer) (target *url.URL, exact bool, err error) {
	raw := method.Proxy
	exact = restclient.PlaceholderPattern.MatchString(raw)
	if exact {
		if raw, err = render.text(raw, false); err != nil {
			return nil, true, err
		}
		if restclient.PlaceholderPattern.MatchString(raw) {
			return nil, true, fmt.Errorf("@proxy URL %q has a placeholder the request does not provide", raw)
		}
	}
	target, err = url.Parse(raw)
	if err != nil {
		return nil, exact, fmt.Errorf("@proxy URL %q: %w", raw, err)
	}
	return target, exact, nil
}

// serveRouteProxy forwards r for a matched @proxy route.
func (s *Server) serveRouteProxy(w http.ResponseWriter, r *http.Request, target *url.URL, exact bool) {
	if s.looped(r) {
		http.Error(w, "mock: request loop detected: the @proxy target sent this request back to mock", http.StatusLoopDetected)
		return
	}
	s.reverseProxy(target.String(), func(pr *httputil.ProxyRequest) {
		if !exact {
			pr.SetURL(target)
			return
		}
		out := pr.Out.URL
		out.Scheme, out.Host = target.Scheme, target.Host
		out.Path, out.RawPath = target.Path, target.RawPath
		if out.Path == "" {
			out.Path, out.RawPath = "/", ""
		}
		if target.RawQuery != "" || target.ForceQuery {
			out.RawQuery = target.RawQuery
		}
		pr.Out.Host = ""
	}).ServeHTTP(w, r)
}

func (s *Server) looped(r *http.Request) bool {
	via := s.via()
	for _, header := range r.Header.Values("Via") {
		for entry := range strings.SplitSeq(header, ",") {
			if fields := strings.Fields(entry); len(fields) >= 2 && fields[1] == via {
				return true
			}
		}
	}
	return false
}
