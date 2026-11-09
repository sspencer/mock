package mockhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sspencer/mock/restclient"
)

// callbackTimeout bounds each @callback request, from connecting to reading
// the response. Callbacks are never retried.
var callbackTimeout = 10 * time.Second

// CallbackInfo marks a request event as an outbound @callback rather than a
// request the mock received.
type CallbackInfo struct {
	// Trigger is the ID of the request event whose response sent it.
	Trigger uint64 `json:"trigger"`
	// Error says why no response was received, such as a timeout, a refused
	// connection or an invalid rendered URL. Response.Status is 0 then.
	Error string `json:"error,omitempty"`
}

// pendingCallback is a rendered @callback waiting for its delay.
type pendingCallback struct {
	method  string
	url     string
	headers http.Header
	body    string
	delay   time.Duration
	trigger uint64
	route   MatchInfo
	err     error
}

// renderCallback renders the route's @callback against the request that
// triggered it. It runs in the handler, while the request is still readable.
func renderCallback(method *restclient.Method, render *renderer) pendingCallback {
	c := method.Callback
	p := pendingCallback{method: c.Method, delay: c.Delay, headers: make(http.Header)}
	target, err := render.text(c.URL, false)
	if err == nil {
		if restclient.FileIsTemplate(target) {
			err = fmt.Errorf("callback URL %q has a placeholder with no value", target)
		} else {
			err = restclient.ValidateCallbackURL(target)
		}
	}
	p.url = target
	for name, values := range c.Headers {
		for _, value := range values {
			rendered, renderErr := render.text(value, false)
			if renderErr == nil && !restclient.ValidHeaderValue(rendered) {
				renderErr = fmt.Errorf("callback header %s has an invalid rendered value", name)
			}
			err = errors.Join(err, renderErr)
			p.headers.Add(name, rendered)
		}
	}
	if c.Body != "" {
		if p.headers.Get("Content-Type") == "" {
			p.headers.Set("Content-Type", callbackContentType(c.Body))
		}
		mediaType, _, _ := mime.ParseMediaType(p.headers.Get("Content-Type"))
		body, renderErr := render.text(c.Body, mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"))
		err = errors.Join(err, renderErr)
		p.body = body
	}
	p.err = err
	return p
}

// callbackContentType guesses a callback body's type when no
// @callback.header.Content-Type is given.
func callbackContentType(body string) string {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return "application/json"
	}
	return "text/plain; charset=utf-8"
}

// scheduleCallback sends a rendered @callback after its delay, in the
// background. Close cancels callbacks that have not finished.
func (s *Server) scheduleCallback(r *http.Request, method *restclient.Method, render *renderer, trigger uint64, match MatchInfo) {
	if method.Callback == nil {
		return
	}
	p := renderCallback(method, render)
	p.trigger, p.route = trigger, match
	if s.looped(r) {
		p.err = errors.New("not sent: the request came from this mock's own callback or proxy, so sending would loop")
	}
	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	if s.closed {
		return
	}
	s.callbacks.Go(func() { s.sendCallback(p) })
}

// Close cancels @callback requests that are waiting or in flight and waits
// for them to finish. The server keeps answering requests, but sends no
// further callbacks.
func (s *Server) Close() {
	s.callbackMu.Lock()
	s.closed = true
	s.callbackMu.Unlock()
	s.cancelCallbacks()
	s.callbacks.Wait()
}

func (s *Server) sendCallback(p pendingCallback) {
	if p.err == nil && !sleep(s.callbackCtx, p.delay) {
		return
	}
	startedAt := time.Now()
	event := RequestEvent{Callback: &CallbackInfo{Trigger: p.trigger}, Match: p.route}
	event.Request = EventRequest{
		Method: p.method, HTTPVersion: "HTTP/1.1", Headers: p.headers.Clone(),
		Body: eventBody(loggedBody{text: p.body}, int64(len(p.body))),
		URL:  p.url, Time: formatRequestTime(startedAt), StartedAt: startedAt.UTC().Format(time.RFC3339Nano),
	}
	err := p.err
	var response *http.Response
	var req *http.Request
	if err == nil {
		ctx, cancel := context.WithTimeout(s.callbackCtx, callbackTimeout)
		defer cancel()
		req, err = http.NewRequestWithContext(ctx, p.method, p.url, strings.NewReader(p.body))
		if err == nil {
			req.Header = p.headers.Clone()
			req.Header.Add("Via", "1.1 "+s.via())
			if req.Header.Get("User-Agent") == "" {
				req.Header.Set("User-Agent", "mock-callback")
			}
			event.Request.Scheme, event.Request.Host, event.Request.URL = req.URL.Scheme, req.URL.Host, req.URL.RequestURI()
			event.Request.Headers = req.Header.Clone()
			response, err = callbackClient.Do(req)
		}
	}
	if req != nil {
		req.Host = req.URL.Host
		event.Request.Details = requestDetails(req, loggedBody{text: p.body})
	} else {
		event.Request.Details = fmt.Sprintf("%s %s", p.method, p.url)
	}
	var responseBody loggedBody
	if err == nil {
		defer response.Body.Close()
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxLoggedBodyBytes+1))
		responseBody = loggedBody{text: string(data)}
		if len(data) > maxLoggedBodyBytes {
			responseBody = loggedBody{text: string(data[:maxLoggedBodyBytes]), truncated: true}
		}
		if readErr != nil {
			responseBody.readError = readErr.Error()
		}
		event.Response = EventResponse{
			Status: response.StatusCode, HTTPVersion: response.Proto, Headers: response.Header.Clone(),
			Body: eventBody(responseBody, response.ContentLength), StatusText: statusText(response.StatusCode),
		}
	} else {
		event.Callback.Error = callbackError(err)
		event.Response = EventResponse{StatusText: "Callback failed", Details: "Callback failed: " + event.Callback.Error}
	}
	elapsed := time.Since(startedAt)
	event.Response.Time = elapsed.Round(time.Microsecond).String()
	event.Response.ElapsedMs = elapsed.Milliseconds()
	if err == nil {
		var details strings.Builder
		fmt.Fprintf(&details, "%s %d %s\n", response.Proto, response.StatusCode, statusText(response.StatusCode))
		writeSortedHeaders(&details, response.Header)
		if !responseBody.empty() {
			fmt.Fprintf(&details, "\n%s", responseBody.detailsText())
		}
		event.Response.Details = strings.TrimRight(details.String(), "\n")
	}
	if s.callbackCtx.Err() != nil {
		return // Shutting down: the request was cancelled, not failed.
	}
	s.publishRequest(s.redactEvent(event))

	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	target, errorText := p.url, event.Callback.Error
	if s.redact.Load() {
		target, errorText = redactURL(target), redactURLsInText(errorText)
	}
	attrs := []any{"method", p.method, "url", target, "trigger", p.trigger, "duration", elapsed}
	if err != nil {
		logger.Warn("callback failed", append(attrs, "error", errorText)...)
		return
	}
	logger.Info("callback sent", append(attrs, "status", response.StatusCode)...)
}

// callbackError describes a failed callback without the "Post \"url\":"
// prefix net/http adds.
func callbackError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("timed out after %s", callbackTimeout)
	}
	return err.Error()
}

// callbackClient does not follow redirects, so the log shows the response
// the callback target actually sent.
var callbackClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	Transport:     &http.Transport{Proxy: http.ProxyFromEnvironment, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second},
}

// describeCallback summarizes a route's @callback for the admin UI.
func describeCallback(method restclient.Method) string {
	c := method.Callback
	if c == nil {
		return ""
	}
	text := c.Method + " " + c.URL
	if c.Delay > 0 {
		text += " after " + c.Delay.String()
	}
	return text
}
