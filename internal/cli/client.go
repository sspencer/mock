package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

const clientUsage = "usage: mock client [-url URL] [-port N] [-H 'Name: value'] [-only TAGS] [-skip TAGS] [-var name=value] [-env NAME [-env-file FILE]] <file.http|dir> [file.http|dir...]"

// runClient loads request files and writes a REST Client .http file to stdout
// containing a sample request for each route.
func runClient(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mock client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	shared, err := newSampleFlags(fs, clientUsage, "base `URL` for generated requests (default MOCK_URL, else http://127.0.0.1:PORT)", "include")
	if err != nil {
		return err
	}
	files, help, err := shared.parse(args, stdout)
	if err != nil || help {
		return err
	}
	baseURL, urlSet, err := shared.target()
	if err != nil {
		return err
	}
	methods, err := shared.load(files)
	if err != nil {
		return err
	}

	_, err = io.WriteString(stdout, formatRESTClientFile(methods, baseURL, urlSet, *shared.port, http.Header(shared.headers)))
	return err
}

// formatRESTClientFile renders methods as a REST Client .http file with
// @baseUrl variables at the top and one ### request section per route.
func formatRESTClientFile(methods []restclient.Method, base *url.URL, explicitURL bool, defaultPort int, extra http.Header) string {
	type baseVar struct {
		name string
		url  string
	}
	var vars []baseVar
	varByURL := map[string]string{}
	taken := map[string]bool{}
	varFor := func(routePort int) string {
		u := sendBaseURL(base, explicitURL, defaultPort, routePort)
		if name, ok := varByURL[u]; ok {
			return name
		}
		// The first base URL is baseUrl; others are named after the port
		// they resolve to, with a suffix when two URLs share a port.
		name := "baseUrl"
		if len(vars) > 0 {
			port := urlPort(u)
			name = "baseUrl_" + port
			for n := 2; taken[name]; n++ {
				name = "baseUrl_" + port + "_" + strconv.Itoa(n)
			}
		}
		taken[name] = true
		varByURL[u] = name
		vars = append(vars, baseVar{name: name, url: u})
		return name
	}

	// Pre-register every base URL in route order so @baseUrl definitions sit
	// at the top of the file.
	routeVars := make([]string, len(methods))
	for i, method := range methods {
		routeVars[i] = varFor(method.Port)
	}

	var b strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&b, "@%s = %s\n", v.name, v.url)
	}

	for i, method := range methods {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		sample := buildSampleRequest(method)
		title := strings.TrimSpace(method.Name)
		if title == "" {
			title = sample.Method + " " + sample.Target
		}
		fmt.Fprintf(&b, "### %s\n", title)
		fmt.Fprintf(&b, "%s {{%s}}%s\n", sample.Method, routeVars[i], sample.Target)

		headers, host := mergeExtraHeaders(sample.Headers, sample.Host, extra)
		if host != "" {
			fmt.Fprintf(&b, "Host: %s\n", host)
		}
		for _, name := range slices.Sorted(maps.Keys(headers)) {
			for _, value := range headers[name] {
				fmt.Fprintf(&b, "%s: %s\n", name, value)
			}
		}
		if sample.Body != "" {
			b.WriteByte('\n')
			b.WriteString(prettyClientBody(sample.Body))
			if !strings.HasSuffix(sample.Body, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// urlPort is the port of an http or https URL, explicit or by scheme.
func urlPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "0"
	}
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// prettyClientBody indents JSON bodies for readability and leaves other
// bodies unchanged.
func prettyClientBody(body string) string {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return body
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(body), "", "  "); err != nil {
		return body
	}
	return buf.String()
}
