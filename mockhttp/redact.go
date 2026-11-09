package mockhttp

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// redactedValue replaces a credential in redacted request events.
const redactedValue = "[redacted]"

// SetRedact turns credential redaction on or off for request events recorded
// from now on (--redact). Redaction masks credential-like header, cookie,
// query, JSON and form values in the request log, the event stream and the
// request and callback log lines. Matching and the request hook always see
// the real request.
func (s *Server) SetRedact(on bool) {
	s.redact.Store(on)
}

// redactEvent returns event with credentials masked when redaction is on.
func (s *Server) redactEvent(event RequestEvent) RequestEvent {
	if !s.redact.Load() {
		return event
	}
	return redactEvent(event)
}

// sensitiveWords mark a header, cookie, query, JSON or form name as a
// credential when its lowercase form, with "-" and "_" removed, contains one.
var sensitiveWords = []string{"token", "secret", "passw", "apikey", "authorization", "credential", "cookie", "signature", "session", "privatekey"}

// sensitiveNames are credential names too short to match as substrings.
var sensitiveNames = map[string]bool{"auth": true, "key": true, "sig": true, "pwd": true, "jwt": true, "otp": true, "pin": true}

// sensitiveName reports whether name looks like it holds a credential.
func sensitiveName(name string) bool {
	folded := strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
	if sensitiveNames[folded] {
		return true
	}
	for _, word := range sensitiveWords {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}

func redactEvent(event RequestEvent) RequestEvent {
	request := event.Request
	request.Headers = redactHeaders(request.Headers)
	request.URL = redactURL(request.URL)
	request.Body = redactEventBody(request.Body, firstHeader(event.Request.Headers, "Content-Type"))
	request.Details = redactDetails(request.Details, true)
	event.Request = request

	response := event.Response
	response.Headers = redactHeaders(response.Headers)
	response.Body = redactEventBody(response.Body, firstHeader(event.Response.Headers, "Content-Type"))
	response.Details = redactDetails(response.Details, false)
	if event.Callback != nil {
		// A failed callback's error, and the details that repeat it, can quote
		// the callback URL.
		info := *event.Callback
		info.Error = redactURLsInText(info.Error)
		event.Callback = &info
		if info.Error != "" {
			response.Details = redactURLsInText(response.Details)
		}
	}
	event.Response = response

	if len(event.Match.Candidates) > 0 {
		candidates := make([]RouteMismatch, len(event.Match.Candidates))
		for i, candidate := range event.Match.Candidates {
			reasons := make([]string, len(candidate.Reasons))
			for j, reason := range candidate.Reasons {
				reasons[j] = redactReason(reason)
			}
			candidate.Reasons = reasons
			candidates[i] = candidate
		}
		event.Match.Candidates = candidates
	}
	return event
}

func firstHeader(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	return headers.Get(name)
}

func redactHeaders(headers http.Header) http.Header {
	if headers == nil {
		return nil
	}
	out := make(http.Header, len(headers))
	for name, values := range headers {
		out[name] = make([]string, len(values))
		for i, value := range values {
			out[name][i] = redactHeaderValue(name, value)
		}
	}
	return out
}

// redactHeaderValue masks a credential header. Cookie headers keep the
// cookie names, Set-Cookie keeps its attributes, and Authorization keeps its
// scheme, so the log still shows what was sent.
func redactHeaderValue(name, value string) string {
	switch {
	case strings.EqualFold(name, "Cookie"):
		pairs := strings.Split(value, ";")
		for i, pair := range pairs {
			if key, _, ok := strings.Cut(pair, "="); ok {
				pairs[i] = key + "=" + redactedValue
			}
		}
		return strings.Join(pairs, ";")
	case strings.EqualFold(name, "Set-Cookie"):
		first, attributes, _ := strings.Cut(value, ";")
		if key, _, ok := strings.Cut(first, "="); ok {
			first = key + "=" + redactedValue
		}
		if attributes != "" {
			return first + ";" + attributes
		}
		return first
	case strings.EqualFold(name, "Authorization"), strings.EqualFold(name, "Proxy-Authorization"):
		if scheme, _, ok := strings.Cut(strings.TrimSpace(value), " "); ok {
			return scheme + " " + redactedValue
		}
		return redactedValue
	case sensitiveName(name):
		return redactedValue
	}
	return value
}

// redactURL masks sensitive query values in a request URI or URL, keeping
// the raw form of every other parameter.
func redactURL(raw string) string {
	base, query, ok := strings.Cut(raw, "?")
	if !ok {
		return raw
	}
	fragment := ""
	if i := strings.IndexByte(query, '#'); i >= 0 {
		query, fragment = query[:i], query[i:]
	}
	return base + "?" + redactPairs(query, "&") + fragment
}

// urlInText finds absolute URLs in free text, such as an error message.
var urlInText = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>]+`)

// redactURLsInText masks sensitive query values in every URL in text.
func redactURLsInText(text string) string {
	if !strings.Contains(text, "://") {
		return text
	}
	return urlInText.ReplaceAllStringFunc(text, redactURL)
}

// redactPairs masks sensitive values in name=value pairs joined by sep, as
// in a query string or a URL-encoded form.
func redactPairs(text, sep string) string {
	pairs := strings.Split(text, sep)
	changed := false
	for i, pair := range pairs {
		key, _, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		name, err := url.QueryUnescape(key)
		if err != nil {
			name = key
		}
		if sensitiveName(name) {
			pairs[i] = key + "=" + url.QueryEscape(redactedValue)
			changed = true
		}
	}
	if !changed {
		return text
	}
	return strings.Join(pairs, sep)
}

// redactBody masks the values of sensitive JSON members (strings, numbers,
// literals, objects and arrays alike) and URL-encoded form values. Other
// bodies are returned unchanged. A body with no media type or text/plain
// (what a route without Content-Type sends) is treated as JSON when it starts
// like JSON. JSON is rewritten in place so its formatting is kept.
func redactBody(text, contentType string) string {
	if text == "" {
		return text
	}
	media, _, _ := mime.ParseMediaType(contentType)
	switch {
	case media == "application/x-www-form-urlencoded":
		return redactPairs(text, "&")
	case media == "application/json" || strings.HasSuffix(media, "+json") || (media == "" || media == "text/plain") && looksLikeJSON(text):
		return redactJSON(text)
	}
	return text
}

// redactJSON replaces the value of every member with a sensitive name with
// "[redacted]". It scans the text rather than decoding it, so formatting is
// kept and a truncated body is still masked up to its end.
func redactJSON(text string) string {
	var out strings.Builder
	last := 0
	for i := 0; i < len(text); {
		if text[i] != '"' {
			i++
			continue
		}
		end := jsonStringEnd(text, i)
		if end < 0 {
			break
		}
		colon := skipJSONSpace(text, end)
		if colon < len(text) && text[colon] == ':' && sensitiveName(jsonStringText(text[i:end])) {
			start := skipJSONSpace(text, colon+1)
			if stop := jsonValueEnd(text, start); stop > start {
				out.WriteString(text[last:start])
				out.WriteString(`"` + redactedValue + `"`)
				last, i = stop, stop
				continue
			}
		}
		i = end
	}
	if last == 0 {
		return text
	}
	out.WriteString(text[last:])
	return out.String()
}

// jsonStringEnd returns the index just past the string starting at text[i]
// (a quote), or -1 when it is not terminated.
func jsonStringEnd(text string, i int) int {
	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return -1
}

// jsonStringText decodes a quoted JSON string, or returns it unquoted as is.
func jsonStringText(quoted string) string {
	var value string
	if json.Unmarshal([]byte(quoted), &value) == nil {
		return value
	}
	return strings.Trim(quoted, `"`)
}

func skipJSONSpace(text string, i int) int {
	for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r') {
		i++
	}
	return i
}

// jsonValueEnd returns the index just past the JSON value starting at
// text[i], or len(text) when the value runs to the end of a truncated body.
func jsonValueEnd(text string, i int) int {
	if i >= len(text) {
		return i
	}
	switch text[i] {
	case '"':
		if end := jsonStringEnd(text, i); end >= 0 {
			return end
		}
		return len(text)
	case '{', '[':
		depth := 0
		for j := i; j < len(text); j++ {
			switch text[j] {
			case '"':
				end := jsonStringEnd(text, j)
				if end < 0 {
					return len(text)
				}
				j = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1
				}
			}
		}
		return len(text)
	case ',', '}', ']', ':':
		return i
	}
	j := i
	for j < len(text) && !strings.ContainsRune(", \t\n\r}]", rune(text[j])) {
		j++
	}
	return j
}

func looksLikeJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

func redactEventBody(body EventBody, contentType string) EventBody {
	if body.Encoding != "" {
		return body
	}
	body.Text = redactBody(body.Text, contentType)
	return body
}

// redactDetails masks credentials in an event's raw request or response
// text: the request line's query, header lines and the body.
func redactDetails(details string, request bool) string {
	if details == "" {
		return details
	}
	head, body, hasBody := strings.Cut(details, "\n\n")
	lines := strings.Split(head, "\n")
	contentType := ""
	for i, line := range lines {
		if i == 0 {
			if request {
				if method, rest, ok := strings.Cut(line, " "); ok {
					target, proto, _ := strings.Cut(rest, " ")
					lines[0] = strings.TrimRight(method+" "+redactURL(target)+" "+proto, " ")
				}
			}
			continue
		}
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		if strings.EqualFold(name, "Content-Type") {
			contentType = value
		}
		lines[i] = name + ": " + redactHeaderValue(name, value)
	}
	out := strings.Join(lines, "\n")
	if hasBody {
		out += "\n\n" + redactBody(body, contentType)
	}
	return out
}

// reasonSubjects are the mismatch reasons that quote received values.
var reasonSubjects = []string{"Query ", "Path parameter ", "JSON ", "XML ", "Form "}

// redactReason drops the received values from a near-miss reason about a
// credential-like query, path, JSON, XML or form value.
func redactReason(reason string) string {
	for _, prefix := range reasonSubjects {
		rest, ok := strings.CutPrefix(reason, prefix)
		if !ok {
			continue
		}
		name, _, ok := strings.Cut(rest, ": ")
		if !ok || !pathHasSensitiveName(name) {
			return reason
		}
		if i := strings.Index(reason, "; received "); i >= 0 {
			return reason[:i] + "; received " + redactedValue
		}
		return reason
	}
	return reason
}

// pathHasSensitiveName reports whether any segment of a name or JSON/XML
// path, such as $.user.password, looks like a credential.
func pathHasSensitiveName(path string) bool {
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool {
		return !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
	}) {
		if sensitiveName(segment) {
			return true
		}
	}
	return false
}
