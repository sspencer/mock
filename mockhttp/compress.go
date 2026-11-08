package mockhttp

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"net/http"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// responseEncoding picks the Content-Encoding for a route's response: the
// @compress encoding, or for "auto" the first of gzip and deflate that the
// request's Accept-Encoding allows. "" sends the body as written.
func responseEncoding(method *restclient.Method, r *http.Request) string {
	switch method.Compress {
	case "gzip", "deflate":
		return method.Compress
	case "auto":
		accepted := acceptedEncodings(r.Header.Values("Accept-Encoding"))
		for _, encoding := range []string{"gzip", "deflate"} {
			if q, ok := accepted[encoding]; ok && q > 0 {
				return encoding
			}
			if q, ok := accepted["*"]; ok && q > 0 {
				if _, named := accepted[encoding]; !named {
					return encoding
				}
			}
		}
	}
	return ""
}

// acceptedEncodings parses Accept-Encoding into lower-case codings and their
// q-values (1 when absent).
func acceptedEncodings(headers []string) map[string]float64 {
	accepted := make(map[string]float64)
	for _, header := range headers {
		for item := range strings.SplitSeq(header, ",") {
			coding, params, _ := strings.Cut(strings.TrimSpace(item), ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			if coding == "" {
				continue
			}
			q := 1.0
			for param := range strings.SplitSeq(params, ";") {
				name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
				if ok && strings.EqualFold(strings.TrimSpace(name), "q") {
					if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
						q = parsed
					}
				}
			}
			accepted[coding] = q
		}
	}
	return accepted
}

// compressBody encodes body with gzip or deflate.
func compressBody(encoding string, body []byte) []byte {
	var buf bytes.Buffer
	switch encoding {
	case "gzip":
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(body)
		_ = w.Close()
	case "deflate":
		// HTTP's "deflate" coding is the zlib format (RFC 9110).
		w := zlib.NewWriter(&buf)
		_, _ = w.Write(body)
		_ = w.Close()
	default:
		return body
	}
	return buf.Bytes()
}
