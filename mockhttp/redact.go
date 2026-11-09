package mockhttp

import (
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
// query, JSON and form values in the request log, the event stream, the
// request hook and the request log line. Matching always sees the real
// request.
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

// jsonStringMember finds "name": "value" members in JSON text.
var jsonStringMember = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"(\s*:\s*)"(?:[^"\\]|\\.)*"`)

// redactBody masks sensitive JSON string members and URL-encoded form
// values. Other bodies are returned unchanged. A body with no media type or
// text/plain (what a route without Content-Type sends) is treated as JSON when
// it starts like JSON. JSON is rewritten in place so its formatting is kept.
func redactBody(text, contentType string) string {
	if text == "" {
		return text
	}
	media, _, _ := mime.ParseMediaType(contentType)
	switch {
	case media == "application/x-www-form-urlencoded":
		return redactPairs(text, "&")
	case media == "application/json" || strings.HasSuffix(media, "+json") || (media == "" || media == "text/plain") && looksLikeJSON(text):
		return jsonStringMember.ReplaceAllStringFunc(text, func(member string) string {
			parts := jsonStringMember.FindStringSubmatch(member)
			if !sensitiveName(parts[1]) {
				return member
			}
			return `"` + parts[1] + `"` + parts[2] + `"` + redactedValue + `"`
		})
	}
	return text
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
