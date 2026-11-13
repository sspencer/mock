package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"
)

const importUsage = "usage: mock import [-curl] [-host HOST] [-path PREFIX] <file.har|file.sh|->"

// harFile is the part of a HAR 1.2 file that mock import reads.
type harFile struct {
	Log struct {
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

type harEntry struct {
	Request struct {
		Method   string      `json:"method"`
		URL      string      `json:"url"`
		Headers  []harHeader `json:"headers"`
		PostData *struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status  int         `json:"status"`
		Headers []harHeader `json:"headers"`
		Content struct {
			Size     int64  `json:"size"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Encoding string `json:"encoding"`
		} `json:"content"`
	} `json:"response"`
}

type harHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func harHeaders(list []harHeader) http.Header {
	headers := http.Header{}
	for _, h := range list {
		// HTTP/2 pseudo-headers such as ":authority" are not headers.
		if h.Name != "" && !strings.HasPrefix(h.Name, ":") {
			headers.Add(h.Name, h.Value)
		}
	}
	return headers
}

// runImport converts a HAR file, or with -curl curl commands, into .http
// sections on stdout: one per distinct method and path, drafted like the
// admin UI's Copy as route with the captured response (a placeholder for
// curl, which captures none).
func runImport(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flagSet := flag.NewFlagSet("mock import", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	host := flagSet.String("host", "", "")
	prefix := flagSet.String("path", "", "")
	curl := flagSet.Bool("curl", false, "")
	if err := flagSet.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, importUsage)
			return nil
		}
		return usageError("%v\n%s", err, importUsage)
	}
	if flagSet.NArg() != 1 {
		return usageError("%s", importUsage)
	}
	name := flagSet.Arg(0)
	var data []byte
	var err error
	if name == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(name)
	}
	if err != nil {
		return runError("%v", err)
	}
	var exchanges []exchange
	var skipped int
	if *curl {
		if exchanges, skipped, err = curlExchanges(string(data), *host, *prefix); err != nil {
			return runError("%s: %v", name, err)
		}
	} else {
		var har harFile
		if err := json.Unmarshal(data, &har); err != nil {
			return runError("%s: not a HAR file: %v (use -curl for curl commands)", name, err)
		}
		exchanges, skipped = harExchanges(har.Log.Entries, *host, *prefix)
	}
	if len(exchanges) == 0 {
		return runError("%s: no requests to import (%d skipped)", name, skipped)
	}
	text, count := draftSections(exchanges, 0)
	if _, err := io.WriteString(stdout, text); err != nil {
		return err
	}
	fmt.Fprintf(stderr, "imported %d route(s) from %d request(s)", count, len(exchanges))
	if skipped > 0 {
		fmt.Fprintf(stderr, ", skipped %d", skipped)
	}
	fmt.Fprintln(stderr)
	return nil
}

// harExchanges converts HAR entries, keeping those whose host and path match
// the filters. CORS preflights and entries without an http(s) URL are skipped.
func harExchanges(entries []harEntry, host, prefix string) ([]exchange, int) {
	var exchanges []exchange
	skipped := 0
	for _, entry := range entries {
		x, ok := harExchange(entry, host, prefix)
		if !ok {
			skipped++
			continue
		}
		exchanges = append(exchanges, x)
	}
	return exchanges, skipped
}

func harExchange(entry harEntry, host, prefix string) (exchange, bool) {
	request, response := entry.Request, entry.Response
	u, err := url.Parse(request.URL)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || request.Method == "" {
		return exchange{}, false
	}
	if host != "" && !strings.EqualFold(u.Host, host) && !strings.EqualFold(u.Hostname(), host) {
		return exchange{}, false
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if prefix != "" && !strings.HasPrefix(path, prefix) {
		return exchange{}, false
	}
	requestHeaders := harHeaders(request.Headers)
	if request.Method == http.MethodOptions && requestHeaders.Get("Access-Control-Request-Method") != "" {
		return exchange{}, false
	}
	target := path
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	x := exchange{
		Method:          strings.ToUpper(request.Method),
		Target:          target,
		RequestHeaders:  requestHeaders,
		Status:          response.Status,
		ResponseHeaders: harHeaders(response.Headers),
		host:            u.Host,
	}
	if request.PostData != nil {
		x.RequestBody = request.PostData.Text
		if x.RequestHeaders.Get("Content-Type") == "" && request.PostData.MimeType != "" {
			x.RequestHeaders.Set("Content-Type", request.PostData.MimeType)
		}
	}
	content := response.Content
	if x.ResponseHeaders.Get("Content-Type") == "" && content.MimeType != "" {
		x.ResponseHeaders.Set("Content-Type", content.MimeType)
	}
	x.ResponseBody, x.ResponseOK = harBody(content.Text, content.Encoding, content.Size)
	return x, true
}

// harBody returns a response body as text. It is not usable when it is
// binary or the HAR left out a body it had.
func harBody(text, encoding string, size int64) (string, bool) {
	if encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return "", false
		}
		text = string(decoded)
	} else if encoding != "" {
		return "", false
	}
	if text == "" && size > 0 {
		return "", false
	}
	return text, utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}
