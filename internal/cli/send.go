package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/sspencer/mock/restclient"
)

const sendUsage = "usage: mock send [-all] [-url URL] [-port N] [-H 'Name: value'] [-only TAGS] [-skip TAGS] [-delay 0s] [-timeout 10s] [-json] [-var name=value] [-env NAME [-env-file FILE]] <file.http|dir> [file.http|dir...]"

// sendResult is the outcome of sending one route's sample request.
type sendResult struct {
	Name       string  `json:"name"`
	Method     string  `json:"method"`
	URL        string  `json:"url"`
	Target     string  `json:"target"`
	Source     string  `json:"source"`
	Line       int     `json:"line"`
	Status     int     `json:"status,omitempty"`
	StatusText string  `json:"statusText,omitempty"`
	DurationMs float64 `json:"durationMs"`
	Error      string  `json:"error,omitempty"`
	Skipped    bool    `json:"skipped,omitempty"`
	Reason     string  `json:"reason,omitempty"`
}

type sendReport struct {
	OK      bool         `json:"ok"`
	Sent    int          `json:"sent"`
	Errors  int          `json:"errors"`
	Skipped int          `json:"skipped"`
	Results []sendResult `json:"results"`
}

// runSend loads request files and fires a sample request for each route
// against a running mock server.
func runSend(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock send", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	shared, err := newSampleFlags(fs, sendUsage, "base `URL` of the running mock server (default MOCK_URL, else http://127.0.0.1:PORT)", "send")
	if err != nil {
		return err
	}
	all := fs.Bool("all", false, "also send routes with unsafe methods (POST, PUT, PATCH, DELETE, ...)")
	delay := fs.Duration("delay", 0, "wait this long between requests, such as 100ms")
	timeout := fs.Duration("timeout", 10*time.Second, "time limit for each request")
	asJSON := fs.Bool("json", false, "print the results as JSON")

	files, help, err := shared.parse(args, stdout)
	if err != nil || help {
		return err
	}
	if *delay < 0 {
		return usageError("-delay must not be negative")
	}
	if *timeout <= 0 {
		return usageError("-timeout must be positive")
	}
	baseURL, urlSet, err := shared.target()
	if err != nil {
		return err
	}
	methods, err := shared.load(files)
	if err != nil {
		return err
	}
	port, headers := *shared.port, http.Header(shared.headers)

	client := &http.Client{
		Timeout: *timeout,
		// Report each route's own 3xx status instead of following redirects.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	report := sendReport{OK: true, Results: make([]sendResult, 0, len(methods))}
	waited := false
	for _, method := range methods {
		sample := buildSampleRequest(method)
		if !*all && !safeMethod(sample.Method) {
			// A mock started with --upstream forwards a sample its route
			// does not match, so unsafe methods could reach a real API.
			report.Skipped++
			report.Results = append(report.Results, sendResult{
				Name: method.Name, Method: sample.Method, Target: sample.Target,
				Source: method.Source, Line: method.Line,
				Skipped: true, Reason: "not a safe method; use -all to send it",
			})
			continue
		}
		if waited && *delay > 0 {
			timer := time.NewTimer(*delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return runError("%v", ctx.Err())
			case <-timer.C:
			}
		}
		if err := ctx.Err(); err != nil {
			return runError("%v", err)
		}
		waited = true
		res := sendSampleRoute(ctx, client, baseURL, urlSet, port, headers, method, sample)
		report.Sent++
		if res.Error != "" {
			report.Errors++
			report.OK = false
		}
		report.Results = append(report.Results, res)
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return runError("%v", err)
		}
	} else if err := printSendReport(stdout, report); err != nil {
		return runError("%v", err)
	}
	if !report.OK {
		return &exitError{code: 1, msg: fmt.Sprintf("send failed: %d of %d requests errored", report.Errors, report.Sent)}
	}
	return nil
}

// parseSendURL accepts an absolute http or https base URL, optionally with a
// base path.
func parseSendURL(label, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, usageError("invalid %s %q: %v", label, raw, err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, usageError("invalid %s %q: use an http or https URL such as http://127.0.0.1:8080", label, raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return nil, usageError("invalid %s %q: only a scheme, host, port and base path are allowed", label, raw)
	}
	return u, nil
}

// sendBaseURL returns the base URL for method: an explicit -url applies to
// every route; otherwise a route's own @port overrides the default port.
func sendBaseURL(base *url.URL, explicitURL bool, defaultPort int, routePort int) string {
	if explicitURL && base != nil {
		return strings.TrimSuffix(base.String(), "/")
	}
	if routePort > 0 {
		if base != nil {
			u := *base
			u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(routePort))
			return strings.TrimSuffix(u.String(), "/")
		}
		return fmt.Sprintf("http://%s:%d", defaultBind, routePort)
	}
	if base != nil {
		return strings.TrimSuffix(base.String(), "/")
	}
	return fmt.Sprintf("http://%s:%d", defaultBind, defaultPort)
}

// sendSampleRoute builds and sends method's sample request to the target server.
func sendSampleRoute(ctx context.Context, client *http.Client, base *url.URL, explicitURL bool, defaultPort int, extra http.Header, method restclient.Method, sample sampleRequest) sendResult {
	fullURL := sendBaseURL(base, explicitURL, defaultPort, method.Port) + sample.Target
	res := sendResult{
		Name:   method.Name,
		Method: sample.Method,
		URL:    fullURL,
		Target: sample.Target,
		Source: method.Source,
		Line:   method.Line,
	}

	var body io.Reader
	if sample.Body != "" {
		body = strings.NewReader(sample.Body)
	}
	req, err := http.NewRequestWithContext(ctx, sample.Method, fullURL, body)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	req.Header, req.Host = mergeExtraHeaders(sample.Headers, sample.Host, extra)
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "mock-send")
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.DurationMs = float64(time.Since(start).Microseconds()) / 1000
		res.Error = unwrapURLError(err).Error()
		return res
	}
	defer resp.Body.Close()
	res.Status = resp.StatusCode
	res.StatusText = http.StatusText(resp.StatusCode)
	// A body cut short (a truncate fault, a reset, the timeout) is a failure.
	_, err = io.Copy(io.Discard, resp.Body)
	res.DurationMs = float64(time.Since(start).Microseconds()) / 1000
	if err != nil {
		res.Error = "reading the response body: " + unwrapURLError(err).Error()
	}
	return res
}

// unwrapURLError drops the *url.Error wrapper, which repeats the method and URL.
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

func printSendReport(w io.Writer, report sendReport) error {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range report.Results {
		if r.Skipped {
			fmt.Fprintf(table, "%s\t%s\tSKIP\t-\t%s\t(%s)\n", r.Method, r.Target, r.Name, r.Reason)
			continue
		}
		dur := fmt.Sprintf("%.1fms", r.DurationMs)
		if r.Status != 0 && r.Error != "" {
			fmt.Fprintf(table, "%s\t%s\tERR %d\t%s\t%s\t(%s)\n", r.Method, r.Target, r.Status, dur, r.Name, r.Error)
			continue
		}
		if r.Error != "" {
			fmt.Fprintf(table, "%s\t%s\tERR\t%s\t%s\t(%s)\n", r.Method, r.Target, dur, r.Name, r.Error)
			continue
		}
		fmt.Fprintf(table, "%s\t%s\t%d\t%s\t%s\n", r.Method, r.Target, r.Status, dur, r.Name)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	summary := plural(uint64(report.Errors), "error")
	if report.Skipped > 0 {
		summary += fmt.Sprintf(", %d skipped", report.Skipped)
	}
	_, err := fmt.Fprintf(w, "sent %s (%s)\n", plural(uint64(report.Sent), "request"), summary)
	return err
}
