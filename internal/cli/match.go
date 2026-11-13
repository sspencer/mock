package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

const matchUsage = "usage: mock match [-H 'Name: value'] [-d body|@file] [-port N] [-response] [-json] [-var name=value] [-env NAME [-env-file FILE]] <file.http|dir> [file.http|dir...] METHOD URL"

// matchResult is what mock match -json prints.
type matchResult struct {
	Request    string                   `json:"request"`
	Matched    bool                     `json:"matched"`
	Route      *mockhttp.RouteInfo      `json:"route,omitempty"`
	Candidates []mockhttp.RouteMismatch `json:"candidates,omitempty"`
	Response   *matchResponse           `json:"response,omitempty"`
}

type matchResponse struct {
	Status  int         `json:"status"`
	Headers http.Header `json:"headers"`
	Body    string      `json:"body"`
}

// runMatch answers "which route gets this request?" for request files
// without starting a server: it prints the route that would answer, or the
// closest routes and why each did not match (exit 1).
func runMatch(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock match", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var headers headerFlag
	for _, name := range []string{"H", "header"} {
		fs.Var(&headers, name, "send `Name: value` with the request (repeatable)")
	}
	var data string
	for _, name := range []string{"d", "data"} {
		fs.StringVar(&data, name, "", "request `body`, or @file to read it from a file")
	}
	port := fs.Int("port", 0, "the mock port the request arrives on, for routes with a port")
	response := fs.Bool("response", false, "also render the response (no delay, fault, stream or callback)")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	var vars varFlag
	fs.Var(&vars, "var", "set `name=value` in every request file (repeatable)")
	var env envFlags
	fs.StringVar(&env.Name, "env", "", "use the REST Client environment `NAME`")
	fs.StringVar(&env.File, "env-file", "", "read -env environments from `FILE`")

	// Flags may come before, between or after the arguments.
	var positional []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fs.SetOutput(stdout)
				fmt.Fprintln(stdout, matchUsage)
				fs.PrintDefaults()
				return nil
			}
			return usageError("%v\n%s", err, matchUsage)
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(positional) < 3 {
		return usageError("need request files, a method and a URL\n%s", matchUsage)
	}
	files, method, target := positional[:len(positional)-2], strings.ToUpper(positional[len(positional)-2]), positional[len(positional)-1]
	if err := env.check("-env-file", "-env"); err != nil {
		return err
	}
	opts, err := env.options(vars)
	if err != nil {
		return runError("%v", err)
	}
	methods, err := loadValidationMethods(files, opts)
	if err != nil {
		return runError("%v", err)
	}
	if len(methods) == 0 {
		return runError("no routes in %s", strings.Join(files, ", "))
	}
	body := data
	if name, ok := strings.CutPrefix(data, "@"); ok {
		raw, err := os.ReadFile(name)
		if err != nil {
			return runError("%v", err)
		}
		body = string(raw)
	}
	build := func() (*http.Request, error) {
		return matchRequest(method, target, body, http.Header(headers), *port)
	}
	req, err := build()
	if err != nil {
		return usageError("%v", err)
	}
	server := mockhttp.New(methods, slog.New(slog.DiscardHandler))
	defer server.Close()
	info := server.Explain(req)
	result := matchResult{Request: method + " " + target, Matched: info.Route != nil, Route: info.Route, Candidates: info.Candidates}
	if *response && result.Matched {
		result.Response = renderMatch(methods, build)
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	} else {
		printMatch(stdout, result)
	}
	if !result.Matched {
		return &exitError{code: 1, msg: ""}
	}
	return nil
}

// matchRequest builds the request mock match explains. A full URL's port, or
// -port, is the port it arrives on, so routes with a port can match; without
// one, routes on every port can. A body with no Content-Type is sent as JSON
// when it looks like JSON, else as a URL-encoded form, as curl -d would.
func matchRequest(method, target, body string, headers http.Header, port int) (*http.Request, error) {
	if !strings.HasPrefix(target, "/") && !strings.Contains(target, "://") {
		target = "/" + target
	}
	u, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %v", target, err)
	}
	path := u.RequestURI()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if u.Host != "" {
		req.Host = u.Host
		if port == 0 && u.Port() != "" {
			_, _ = fmt.Sscan(u.Port(), &port)
		}
	}
	for name, values := range headers {
		req.Header[name] = append([]string(nil), values...)
	}
	// -H 'Host: x' is the request's host, as for a real client.
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
		req.Header.Del("Host")
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		trimmed := strings.TrimSpace(body)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}
	if body == "" {
		req.ContentLength = 0
	}
	if port > 0 {
		req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}))
	}
	return req, nil
}

// renderMatch serves the request in-process, from routes without delays,
// faults, streaming, throttling, rate limits or callbacks, and returns the
// response.
func renderMatch(methods []restclient.Method, build func() (*http.Request, error)) *matchResponse {
	quiet := make([]restclient.Method, len(methods))
	for i, method := range methods {
		method.Delay, method.DelayMax, method.DelaySigma, method.Fault, method.Stream = 0, 0, 0, "", 0
		method.Throttle, method.RateLimit, method.Callbacks = 0, 0, nil
		quiet[i] = method
	}
	server := mockhttp.New(quiet, slog.New(slog.DiscardHandler))
	defer server.Close()
	req, err := build()
	if err != nil {
		return nil
	}
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	headers := rec.Header().Clone()
	headers.Del("Date")
	return &matchResponse{Status: rec.Code, Headers: headers, Body: rec.Body.String()}
}

func printMatch(w io.Writer, result matchResult) {
	if result.Matched {
		route := result.Route
		fmt.Fprintf(w, "%s\n  answered by %q (%s:%d)\n", result.Request, route.Name, route.Source, route.Line)
		status := fmt.Sprint(route.Status)
		if route.StatusTemplate != "" {
			status = route.StatusTemplate
		}
		fmt.Fprintf(w, "  %s %s  status %s\n", route.Method, route.Path, status)
		if route.Proxy != "" {
			fmt.Fprintf(w, "  forwards to %s\n", route.Proxy)
		}
		if route.Chance != "" {
			fmt.Fprintf(w, "  note: @match.chance = %s, so it answers only that share; later routes answer the rest\n", route.Chance)
		}
		if route.Times > 0 {
			fmt.Fprintf(w, "  note: @times = %d, so it answers only the first %d of these requests\n", route.Times, route.Times)
		}
		if response := result.Response; response != nil {
			fmt.Fprintf(w, "\nHTTP %d %s\n", response.Status, http.StatusText(response.Status))
			names := make([]string, 0, len(response.Headers))
			for name := range response.Headers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				for _, value := range response.Headers[name] {
					fmt.Fprintf(w, "%s: %s\n", name, value)
				}
			}
			if response.Body != "" {
				fmt.Fprintf(w, "\n%s", response.Body)
				if !strings.HasSuffix(response.Body, "\n") {
					fmt.Fprintln(w)
				}
			}
		}
		return
	}
	fmt.Fprintf(w, "%s\n  no route matches\n", result.Request)
	if len(result.Candidates) == 0 {
		return
	}
	fmt.Fprintln(w, "\nclosest routes:")
	for _, candidate := range result.Candidates {
		port := ""
		if candidate.Port != 0 {
			port = fmt.Sprintf(":%d", candidate.Port)
		}
		fmt.Fprintf(w, "  %q (%s:%d)  %s %s%s\n", candidate.Name, candidate.Source, candidate.Line, candidate.Method, port, candidate.Path)
		for _, reason := range candidate.Reasons {
			fmt.Fprintf(w, "    - %s\n", reason)
		}
	}
}
