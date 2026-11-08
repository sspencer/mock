package cli

import (
	"fmt"
	"io"
	"strconv"

	"github.com/sspencer/mock/mockhttp"
)

// strictReport writes the --strict shutdown summary: requests no route
// answered, then routes that never answered a request. It returns an error,
// so the command exits 1, when there were unmatched requests. Unused routes
// are listed but do not fail the run.
func strictReport(w io.Writer, coverage mockhttp.Coverage) error {
	var unused []mockhttp.RouteHits
	for _, route := range coverage.Routes {
		if route.Hits == 0 {
			unused = append(unused, route)
		}
	}
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
	if coverage.UnmatchedTotal > 0 {
		return runError("strict: %s unmatched", plural(coverage.UnmatchedTotal, "request"))
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
