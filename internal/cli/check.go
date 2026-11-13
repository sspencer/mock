package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

const checkUsage = "usage: mock check -upstream URL [-all] [-json] [-depth 3] [-check-header Name] [-header 'Name: value'] [-var name=value] [-env NAME [-env-file FILE]] [-timeout 10s] <file.http|dir> [file.http|dir...]"

// maxCheckBody caps how much of each response body is read for the JSON
// field comparison.
const maxCheckBody = 4 << 20

// checkResult is the drift check of one route.
type checkResult struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	// Target is the sample request's path and query.
	Target string `json:"target"`
	Source string `json:"source"`
	Line   int    `json:"line"`
	// Result is "ok", "drift", "skipped" or "error".
	Result string `json:"result"`
	// Reason says why a route was skipped or could not be checked.
	Reason      string         `json:"reason,omitempty"`
	Differences []string       `json:"differences,omitempty"`
	Mock        *checkResponse `json:"mock,omitempty"`
	Upstream    *checkResponse `json:"upstream,omitempty"`
}

// checkResponse summarizes one side's answer to the sample request.
type checkResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType,omitempty"`
	body        []byte
	headers     http.Header
}

type checkReport struct {
	OK      bool          `json:"ok"`
	Results []checkResult `json:"results"`
}

// headerFlag collects repeated -header "Name: value" flags.
type headerFlag http.Header

func (h *headerFlag) String() string { return "" }

func (h *headerFlag) Set(raw string) error {
	name, value, ok := strings.Cut(raw, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !ok || !restclient.ValidHeaderName(name) || !restclient.ValidHeaderValue(value) {
		return fmt.Errorf("%q: use \"Name: value\"", raw)
	}
	if *h == nil {
		*h = headerFlag{}
	}
	http.Header(*h).Add(name, value)
	return nil
}

// runCheck sends each route's sample request to the mock and to the real
// API and reports differences in status, Content-Type and top-level JSON
// fields.
func runCheck(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	upstreamRaw := fs.String("upstream", "", "the real API's base `URL`")
	all := fs.Bool("all", false, "also send routes with unsafe methods (POST, PUT, PATCH, DELETE)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	timeout := fs.Duration("timeout", 10*time.Second, "time limit for each upstream request")
	var headers headerFlag
	fs.Var(&headers, "header", "add `Name: value` to every upstream request (repeatable)")
	depth := fs.Int("depth", 3, "compare JSON bodies this many levels deep (1: top-level keys only)")
	var checkHeaders listFlag
	fs.Var(&checkHeaders, "check-header", "also compare this response header `Name` (repeatable)")
	var vars varFlag
	fs.Var(&vars, "var", "set `name=value` in every request file (repeatable)")
	var env envFlags
	fs.StringVar(&env.Name, "env", "", "use the REST Client environment `NAME` for variables no @name defines")
	fs.StringVar(&env.File, "env-file", "", "read -env environments from `FILE` (default http-client.env.json, else .vscode/settings.json)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fmt.Fprintln(stdout, checkUsage)
			fs.PrintDefaults()
			return nil
		}
		return usageError("%v\n%s", err, checkUsage)
	}
	if *upstreamRaw == "" {
		return usageError("missing -upstream URL\n%s", checkUsage)
	}
	upstream, err := parseUpstream(*upstreamRaw)
	if err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return usageError("missing request files\n%s", checkUsage)
	}
	if *timeout <= 0 {
		return usageError("-timeout must be positive")
	}
	if *depth < 1 {
		return usageError("-depth must be at least 1")
	}
	if err := env.check("-env-file", "-env"); err != nil {
		return err
	}
	opts, err := env.options(vars)
	if err != nil {
		return runError("%v", err)
	}
	methods, err := loadValidationMethods(fs.Args(), opts)
	if err != nil {
		return runError("%v", err)
	}

	client := &http.Client{
		Timeout: *timeout,
		// Compare the status the API answers, not where it redirects to.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	report := checkReport{OK: true, Results: []checkResult{}}
	for _, method := range methods {
		result := checkRoute(ctx, client, upstream, http.Header(headers), method, *all, compareOptions{depth: *depth, headers: checkHeaders})
		if result.Result == "drift" || result.Result == "error" {
			report.OK = false
		}
		report.Results = append(report.Results, result)
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return runError("%v", err)
		}
	} else {
		printCheckReport(stdout, report)
	}
	if !report.OK {
		counts := countResults(report.Results)
		return &exitError{code: 1, msg: fmt.Sprintf("drift check failed: %d drifted, %d errors", counts["drift"], counts["error"])}
	}
	return nil
}

// checkRoute sends method's sample request to the mock and to upstream and
// compares the answers.
func checkRoute(ctx context.Context, client *http.Client, upstream *url.URL, extra http.Header, method restclient.Method, all bool, opts compareOptions) checkResult {
	sample := buildSampleRequest(method)
	result := checkResult{Name: method.Name, Method: method.Method, Target: sample.Target, Source: method.Source, Line: method.Line}
	switch {
	case method.Proxy != "":
		result.Result, result.Reason = "skipped", "@proxy route forwards to a real service already"
		return result
	case !all && !safeMethod(sample.Method):
		result.Result, result.Reason = "skipped", "not a safe method; use -all to send it"
		return result
	}

	mock, err := mockResponse(method, sample)
	if err != nil {
		result.Result, result.Reason = "error", err.Error()
		return result
	}
	result.Mock = mock
	real, err := upstreamResponse(ctx, client, upstream, extra, sample)
	if err != nil {
		result.Result, result.Reason = "error", err.Error()
		return result
	}
	result.Upstream = real
	result.Differences = compareResponses(sample.Method, mock, real, opts)
	result.Result = "ok"
	if len(result.Differences) > 0 {
		result.Result = "drift"
	}
	return result
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// mockResponse serves the sample request from a server holding only this
// route, without its delay, faults, streaming, rate limit, callback or
// @scenario state.
func mockResponse(method restclient.Method, sample sampleRequest) (*checkResponse, error) {
	method.Port, method.Fault, method.Stream, method.RateLimit, method.Callbacks = 0, "", 0, 0, nil
	method.Delay, method.DelayMax, method.DelaySigma, method.Throttle = 0, 0, 0, 0
	method.Scenario, method.State, method.NextState = "", "", ""
	method.Variables = maps.Clone(method.Variables)
	delete(method.Variables, "delay")
	server := mockhttp.New([]restclient.Method{method}, slog.New(slog.DiscardHandler))
	defer server.Close()

	request := httptest.NewRequest(sample.Method, sample.Target, strings.NewReader(sample.Body))
	request.Header = sample.Headers.Clone()
	if sample.Host != "" {
		request.Host = sample.Host
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	events := server.Requests(mockhttp.RequestFilter{})
	if len(events) == 0 || events[len(events)-1].Match.Route == nil {
		return nil, errors.New("the sample request does not match its own route (a ~regex or negated matcher needs a value the sample cannot guess)")
	}
	response := recorder.Result()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxCheckBody))
	return &checkResponse{Status: response.StatusCode, ContentType: response.Header.Get("Content-Type"), body: body, headers: response.Header.Clone()}, nil
}

// upstreamResponse sends the sample request to the real API, under its base
// path, with the -header extras.
func upstreamResponse(ctx context.Context, client *http.Client, upstream *url.URL, extra http.Header, sample sampleRequest) (*checkResponse, error) {
	target := strings.TrimSuffix(upstream.String(), "/") + sample.Target
	var body io.Reader
	if sample.Body != "" {
		body = strings.NewReader(sample.Body)
	}
	request, err := http.NewRequestWithContext(ctx, sample.Method, target, body)
	if err != nil {
		return nil, err
	}
	request.Header = sample.Headers.Clone()
	for name, values := range extra {
		request.Header[name] = slices.Clone(values)
	}
	request.Header.Set("User-Agent", "mock-check")
	response, err := client.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("upstream request failed: %v", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxCheckBody))
	if err != nil {
		return nil, fmt.Errorf("upstream response failed: %v", err)
	}
	return &checkResponse{Status: response.StatusCode, ContentType: response.Header.Get("Content-Type"), body: data, headers: response.Header.Clone()}, nil
}

// compareResponses lists how the real answer differs from the mock's: the
// status, the Content-Type media type, and, when both are the same JSON
// status, the top-level fields (of the first element, for arrays).
// compareOptions says how deep mock check compares JSON bodies and which
// response headers it compares.
type compareOptions struct {
	// depth is how many levels of nested objects are compared; 1 compares
	// only the top-level keys.
	depth   int
	headers []string
}

func compareResponses(method string, mock, real *checkResponse, opts compareOptions) []string {
	var differences []string
	if mock.Status != real.Status {
		differences = append(differences, fmt.Sprintf("status: mock %d, upstream %d", mock.Status, real.Status))
	}
	mockType, realType := mediaType(mock.ContentType), mediaType(real.ContentType)
	if mockType != realType {
		differences = append(differences, fmt.Sprintf("content type: mock %s, upstream %s", orNone(mockType), orNone(realType)))
	}
	for _, name := range opts.headers {
		mockValue := strings.Join(mock.headers.Values(name), ", ")
		realValue := strings.Join(real.headers.Values(name), ", ")
		if mockValue != realValue {
			differences = append(differences, fmt.Sprintf("header %s: mock %s, upstream %s", http.CanonicalHeaderKey(name), orNone(quoteValue(mockValue)), orNone(quoteValue(realValue))))
		}
	}
	if mock.Status != real.Status || method == http.MethodHead || !isJSONType(mockType) || !isJSONType(realType) {
		return differences
	}
	return append(differences, compareJSONFields(mock.body, real.body, max(opts.depth, 1))...)
}

func quoteValue(value string) string {
	if value == "" {
		return ""
	}
	return strconv.Quote(value)
}

// compareJSONFields compares the shape of two JSON bodies: values of
// different types, and object keys missing on either side for objects at
// most depth levels deep (the body is level 1). Arrays compare their first
// elements. null matches any type, since APIs often send null for an
// optional field.
func compareJSONFields(mockBody, realBody []byte, depth int) []string {
	var mockValue, realValue any
	if len(realBody) == 0 && len(mockBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(mockBody, &mockValue); err != nil {
		return []string{"json: the mock body is not valid JSON"}
	}
	if err := json.Unmarshal(realBody, &realValue); err != nil {
		return []string{"json: the upstream body is not valid JSON"}
	}
	var differences []string
	compareJSONValues("", mockValue, realValue, 1, depth, &differences)
	return differences
}

func compareJSONValues(path string, mockValue, realValue any, level, depth int, differences *[]string) {
	where := "json"
	if path != "" {
		where = "json " + path
	}
	if mockValue == nil || realValue == nil {
		return
	}
	if kind(mockValue) != kind(realValue) {
		*differences = append(*differences, fmt.Sprintf("%s: mock has %s, upstream %s", where, kind(mockValue), kind(realValue)))
		return
	}
	switch mockValue := mockValue.(type) {
	case []any:
		realList := realValue.([]any)
		if len(mockValue) > 0 && len(realList) > 0 {
			compareJSONValues(path+"[0]", mockValue[0], realList[0], level, depth, differences)
		}
	case map[string]any:
		if level > depth {
			return // Deeper than -depth: its type matched, its keys are not compared.
		}
		realObject := realValue.(map[string]any)
		var missing, extra, common []string
		for key := range realObject {
			if _, ok := mockValue[key]; !ok {
				missing = append(missing, key)
			}
		}
		for key := range mockValue {
			if _, ok := realObject[key]; ok {
				common = append(common, key)
			} else {
				extra = append(extra, key)
			}
		}
		if len(missing) > 0 {
			slices.Sort(missing)
			*differences = append(*differences, fmt.Sprintf("%s: missing from mock: %s", where, strings.Join(missing, ", ")))
		}
		if len(extra) > 0 {
			slices.Sort(extra)
			*differences = append(*differences, fmt.Sprintf("%s: only in mock: %s", where, strings.Join(extra, ", ")))
		}
		slices.Sort(common)
		for _, key := range common {
			compareJSONValues(jsonPathJoin(path, key), mockValue[key], realObject[key], level+1, depth, differences)
		}
	}
}

// jsonPathJoin adds a key to a path for messages, as user.address or
// meta["a.b"].
func jsonPathJoin(path, key string) string {
	if strings.ContainsAny(key, ". []\"") {
		return path + `["` + key + `"]`
	}
	if path == "" {
		return key
	}
	return path + "." + key
}

// kind names a decoded JSON value's type for messages.
func kind(value any) string {
	switch value.(type) {
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case float64:
		return "a number"
	case bool:
		return "a boolean"
	default:
		return "null"
	}
}

func mediaType(contentType string) string {
	if contentType == "" {
		return ""
	}
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	return parsed
}

func isJSONType(mediaType string) bool {
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func orNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func countResults(results []checkResult) map[string]int {
	counts := map[string]int{}
	for _, result := range results {
		counts[result.Result]++
	}
	return counts
}

func printCheckReport(w io.Writer, report checkReport) {
	for _, r := range report.Results {
		where := fmt.Sprintf("%s:%d %s", r.Source, r.Line, r.Name)
		switch r.Result {
		case "ok":
			fmt.Fprintf(w, "ok     %s %s (%s)\n", r.Method, r.Target, where)
		case "skipped":
			fmt.Fprintf(w, "skip   %s %s (%s): %s\n", r.Method, r.Target, where, r.Reason)
		case "error":
			fmt.Fprintf(w, "error  %s %s (%s): %s\n", r.Method, r.Target, where, r.Reason)
		default:
			fmt.Fprintf(w, "drift  %s %s (%s)\n", r.Method, r.Target, where)
			for _, difference := range r.Differences {
				fmt.Fprintf(w, "         %s\n", difference)
			}
		}
	}
	counts := countResults(report.Results)
	fmt.Fprintf(w, "checked %d routes: %d ok, %d drifted, %d errors, %d skipped\n",
		len(report.Results), counts["ok"], counts["drift"], counts["error"], counts["skipped"])
}
