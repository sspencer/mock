package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/sspencer/mock/mockhttp"
)

const routesUsage = "usage: mock routes [-json] [-port N] [-only TAGS] [-skip TAGS] [-var name=value] [-env NAME [-env-file FILE]] <file.http|dir> [file.http|dir...]"

// runRoutes prints the route table of request files without serving them:
// each route's method, path, port, status, name, source line, tags and
// request matchers, in the order mock tries them.
func runRoutes(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock routes", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "print the routes as JSON, as GET /routes describes them")
	defaultPort, _, err := portFromEnv("MOCK_PORT", 8080)
	if err != nil {
		return usageError("%v", err)
	}
	port := fs.Int("port", defaultPort, "the default port, for routes that set none (as mock -p)")
	var tags tagFilter
	fs.Var((*tagsFlag)(&tags.Only), "only", "list only routes with one of these @tag `names` (comma-separated, repeatable)")
	fs.Var((*tagsFlag)(&tags.Skip), "skip", "leave out routes with any of these @tag `names`")
	var vars varFlag
	fs.Var(&vars, "var", "set `name=value` in every request file (repeatable)")
	var env envFlags
	fs.StringVar(&env.Name, "env", "", "use the REST Client environment `NAME`")
	fs.StringVar(&env.File, "env-file", "", "read -env environments from `FILE`")

	// Flags may come before, between or after the files.
	var files []string
	for rest := args; ; {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				fs.SetOutput(stdout)
				fmt.Fprintln(stdout, routesUsage)
				fs.PrintDefaults()
				return nil
			}
			return usageError("%v\n%s", err, routesUsage)
		}
		if fs.NArg() == 0 {
			break
		}
		files = append(files, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	if len(files) == 0 {
		return usageError("need request files\n%s", routesUsage)
	}
	if *port < 1 || *port > 65535 {
		return usageError("invalid -port %d (expected 1-65535)", *port)
	}
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
	if methods, err = tags.apply(methods); err != nil {
		return runError("%v", err)
	}
	server := mockhttp.New(methods, slog.New(slog.DiscardHandler))
	defer server.Close()
	routes := server.Routes()
	for i := range routes {
		if routes[i].Port == 0 {
			routes[i].Port = *port
		}
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(routes)
	}
	return printRoutes(stdout, routes)
}

// printRoutes writes the route table as aligned columns.
func printRoutes(w io.Writer, routes []mockhttp.RouteInfo) error {
	table := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(table, "METHOD\tPATH\tPORT\tSTATUS\tNAME\tSOURCE\tTAGS\tMATCHES")
	for _, route := range routes {
		path := route.Path
		if route.Query != "" {
			path += "?" + route.Query
		}
		status := strconv.Itoa(route.Status)
		switch {
		case route.Proxy != "":
			status = "proxy"
		case route.StatusTemplate != "":
			status = route.StatusTemplate
		}
		fmt.Fprintf(table, "%s\t%s\t%d\t%s\t%s\t%s:%d\t%s\t%s\n", route.Method, path, route.Port, status, route.Name,
			route.Source, route.Line, strings.Join(route.Tags, ","), strings.Join(routeMatchers(route), "; "))
	}
	return table.Flush()
}

// routeMatchers lists a route's request matchers as short "kind name: value"
// text, in a fixed order, such as "header X-Tenant: acme".
func routeMatchers(route mockhttp.RouteInfo) []string {
	var out []string
	multi := func(kind string, values map[string][]string) {
		for _, name := range slices.Sorted(maps.Keys(values)) {
			for _, value := range values[name] {
				out = append(out, kind+" "+name+": "+value)
			}
		}
	}
	single := func(kind string, values map[string]string) {
		for _, name := range slices.Sorted(maps.Keys(values)) {
			out = append(out, kind+" "+name+": "+values[name])
		}
	}
	if route.MatchHost != "" {
		out = append(out, "host: "+route.MatchHost)
	}
	multi("header", route.MatchHeaders)
	multi("query", route.MatchQuery)
	single("path", route.MatchPath)
	multi("cookie", route.MatchCookies)
	if route.MatchBody != "" {
		out = append(out, "body: "+route.MatchBody)
	}
	if route.MatchJSONBody != "" {
		out = append(out, "json: "+route.MatchJSONBody)
	}
	single("json", route.MatchJSON)
	single("xml", route.MatchXML)
	multi("form", route.MatchForm)
	if route.Chance != "" {
		out = append(out, "chance: "+route.Chance)
	}
	if route.Scenario != "" {
		scenario := "scenario " + route.Scenario
		if route.State != "" {
			scenario += " in " + route.State
		}
		if route.NextState != "" {
			scenario += " -> " + route.NextState
		}
		out = append(out, scenario)
	}
	if route.Times > 0 {
		out = append(out, "times: "+strconv.Itoa(route.Times))
	}
	if route.Require != "" {
		out = append(out, "require: "+route.Require)
	}
	if route.Fallback {
		out = append(out, "fallback")
	}
	return out
}
