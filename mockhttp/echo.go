package mockhttp

import "strings"

// requestEchoPath returns the JSON path of a $request placeholder key: "" for
// $request itself, or the part after "request." (or after "request" for a
// quoted key).
func requestEchoPath(key string) (string, bool) {
	if key == "request" {
		return "", true
	}
	if path, ok := strings.CutPrefix(key, "request."); ok && path != "" {
		return path, true
	}
	if path, ok := strings.CutPrefix(key, "request"); ok && strings.HasPrefix(path, "[") {
		return path, true
	}
	return "", false
}

// requestDocument describes the request as a JSON-ready object for
// {{$request}}: method, url, path, query, host, headers, body and, when the
// body is JSON, json. Query and header values are arrays, since both can
// repeat. It is built once per request.
func (f *requestFacts) requestDocument() any {
	if f.request != nil {
		return f.request
	}
	query := map[string]any{}
	for name, values := range f.query {
		query[name] = stringsToAny(values)
	}
	headers := map[string]any{}
	for name, values := range f.headers {
		headers[name] = stringsToAny(values)
	}
	doc := map[string]any{
		"method":  f.r.Method,
		"url":     f.r.URL.RequestURI(),
		"path":    f.r.URL.Path,
		"query":   query,
		"host":    f.r.Host,
		"headers": headers,
	}
	if f.body.unavailable {
		doc["body"] = nil
		doc["bodyUnavailable"] = true
	} else {
		doc["body"] = string(f.body.data)
		if root, err := f.jsonDocument(); err == nil && root != nil {
			doc["json"] = root
		}
	}
	f.request = doc
	return doc
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

// requestValue resolves {{$request}} and {{$request.path}}.
func (f *requestFacts) requestValue(path string) (string, bool) {
	for _, node := range selectJSONNodes(f.requestDocument(), path) {
		if text, ok := jsonText(node); ok {
			return text, true
		}
	}
	return "", false
}
