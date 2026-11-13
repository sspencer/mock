package cli

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sspencer/mock/mockhttp"
)

// unusedRoutes returns the routes that never answered a request.
func unusedRoutes(coverage mockhttp.Coverage) []mockhttp.RouteHits {
	var unused []mockhttp.RouteHits
	for _, route := range coverage.Routes {
		if route.Hits == 0 {
			unused = append(unused, route)
		}
	}
	return unused
}

// strictReport writes the --strict shutdown summary: requests no route
// answered, then routes that never answered a request. It returns an error,
// so the command exits 1, when there were unmatched requests, or unused
// routes with failUnused (--strict=unused); otherwise unused routes are
// listed but do not fail the run.
func strictReport(w io.Writer, coverage mockhttp.Coverage, failUnused bool) error {
	unused := unusedRoutes(coverage)
	if coverage.UnmatchedTotal > 0 {
		fmt.Fprintf(w, "strict: %s no route matched\n", plural(coverage.UnmatchedTotal, "request"))
		var listed uint64
		for _, request := range coverage.Unmatched {
			proxied := ""
			if request.Proxied {
				proxied = " (proxied)"
			}
			fmt.Fprintf(w, "  %dx %s %s%s\n", request.Count, request.Method, request.Path, proxied)
			listed += request.Count
		}
		if rest := coverage.UnmatchedTotal - listed; rest > 0 {
			fmt.Fprintf(w, "  ... and %s to other paths\n", plural(rest, "request"))
		}
	}
	if len(unused) > 0 {
		fmt.Fprintf(w, "strict: %d of %s never matched a request\n", len(unused), plural(uint64(len(coverage.Routes)), "route"))
		for _, route := range unused {
			fmt.Fprintf(w, "  %s:%d  %s %s  %s\n", route.Source, route.Line, route.Method, route.Path, route.Name)
		}
	}
	switch {
	case coverage.UnmatchedTotal > 0 && failUnused && len(unused) > 0:
		return runError("strict: %s unmatched, %s never matched", plural(coverage.UnmatchedTotal, "request"), plural(uint64(len(unused)), "route"))
	case coverage.UnmatchedTotal > 0:
		return runError("strict: %s unmatched", plural(coverage.UnmatchedTotal, "request"))
	case failUnused && len(unused) > 0:
		return runError("strict: %s never matched", plural(uint64(len(unused)), "route"))
	}
	if len(unused) == 0 {
		fmt.Fprintf(w, "strict: every request matched a route, and all %s matched a request\n", plural(uint64(len(coverage.Routes)), "route"))
	}
	return nil
}

func plural(n uint64, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.FormatUint(n, 10) + " " + noun + "s"
}

// strictReportFormat is the --strict-report format its file name implies:
// "json" for .json, "junit" for .xml, else "".
func strictReportFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json"
	case ".xml":
		return "junit"
	}
	return ""
}

// writeStrictReport writes the strict report to path (--strict-report), as
// JSON or JUnit XML by its extension.
func writeStrictReport(path string, coverage mockhttp.Coverage, failUnused bool) error {
	var data []byte
	var err error
	if strictReportFormat(path) == "json" {
		data, err = strictJSON(coverage, failUnused)
	} else {
		data, err = strictJUnit(coverage, failUnused)
	}
	if err == nil {
		err = os.WriteFile(path, data, 0o644)
	}
	if err != nil {
		return runError("strict: cannot write the report: %v", err)
	}
	return nil
}

// strictJSON is the report as JSON: whether the run passed, the unmatched
// requests, and every route with its hit count, plus the unused ones.
func strictJSON(coverage mockhttp.Coverage, failUnused bool) ([]byte, error) {
	unused := unusedRoutes(coverage)
	report := struct {
		Passed         bool                        `json:"passed"`
		FailUnused     bool                        `json:"failUnused"`
		UnmatchedTotal uint64                      `json:"unmatchedTotal"`
		Unmatched      []mockhttp.UnmatchedRequest `json:"unmatched"`
		Routes         []mockhttp.RouteHits        `json:"routes"`
		Unused         []mockhttp.RouteHits        `json:"unused"`
	}{
		Passed:         coverage.UnmatchedTotal == 0 && !(failUnused && len(unused) > 0),
		FailUnused:     failUnused,
		UnmatchedTotal: coverage.UnmatchedTotal,
		Unmatched:      nonNil(coverage.Unmatched),
		Routes:         nonNil(coverage.Routes),
		Unused:         nonNil(unused),
	}
	data, err := json.MarshalIndent(report, "", "  ")
	return append(data, '\n'), err
}

func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	File      string        `xml:"file,attr,omitempty"`
	Line      int           `xml:"line,attr,omitempty"`
	Failure   *junitMessage `xml:"failure"`
	Skipped   *junitMessage `xml:"skipped"`
}

type junitMessage struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func (s *junitSuite) add(c junitCase) {
	s.Tests++
	if c.Failure != nil {
		s.Failures++
	}
	if c.Skipped != nil {
		s.Skipped++
	}
	s.Cases = append(s.Cases, c)
}

// strictJUnit is the report as JUnit XML: a "requests" suite with a failed
// test case per unmatched method and path (one passing case when there are
// none), and a "routes" suite with a case per route, failed when it never
// matched with failUnused and skipped when it never matched without it.
func strictJUnit(coverage mockhttp.Coverage, failUnused bool) ([]byte, error) {
	requests := junitSuite{Name: "mock requests"}
	var listed uint64
	for _, request := range coverage.Unmatched {
		listed += request.Count
		text := plural(request.Count, "request") + " no route matched"
		if request.Proxied {
			text += " (forwarded with --upstream)"
		}
		requests.add(junitCase{Name: request.Method + " " + request.Path, ClassName: "unmatched",
			Failure: &junitMessage{Message: text, Text: text}})
	}
	if rest := coverage.UnmatchedTotal - listed; rest > 0 {
		text := plural(rest, "request") + " to other paths no route matched"
		requests.add(junitCase{Name: "other paths", ClassName: "unmatched", Failure: &junitMessage{Message: text, Text: text}})
	}
	if coverage.UnmatchedTotal == 0 {
		requests.add(junitCase{Name: "every request matched a route", ClassName: "unmatched"})
	}
	routes := junitSuite{Name: "mock routes"}
	for _, route := range coverage.Routes {
		c := junitCase{Name: strings.TrimSpace(route.Method + " " + route.Path + "  " + route.Name), ClassName: route.Source, File: route.Source, Line: route.Line}
		if route.Hits == 0 {
			text := fmt.Sprintf("%s:%d: route never matched a request", route.Source, route.Line)
			if failUnused {
				c.Failure = &junitMessage{Message: "never matched a request", Text: text}
			} else {
				c.Skipped = &junitMessage{Message: "never matched a request", Text: text}
			}
		}
		routes.add(c)
	}
	suites := junitSuites{Name: "mock strict", Suites: []junitSuite{requests, routes}}
	for _, suite := range suites.Suites {
		suites.Tests += suite.Tests
		suites.Failures += suite.Failures
		suites.Skipped += suite.Skipped
	}
	data, err := xml.MarshalIndent(suites, "", "  ")
	return append(append([]byte(xml.Header), data...), '\n'), err
}
