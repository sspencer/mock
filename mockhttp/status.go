package mockhttp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// renderStatus renders a route's status template, such as
// "{{$query.status | default 200}}", into a status between 200 and 999.
func renderStatus(method *restclient.Method, render *renderer) (int, error) {
	text, err := render.text(method.StatusTemplate, false)
	if err != nil {
		return 0, err
	}
	text = strings.TrimSpace(text)
	status, err := strconv.Atoi(text)
	if err != nil || status < 200 || status > 999 {
		return 0, fmt.Errorf("rendered status %q is not a status between 200 and 999", text)
	}
	return status, nil
}

// bodyETag is the strong ETag of a response body: a quoted prefix of its
// SHA-256. It is taken after compression, so each encoding has its own.
func bodyETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:10]) + `"`
}

// etagMatches reports whether an If-None-Match header lists etag, or is "*".
// Weak tags (W/"...") compare by their value, as RFC 9110 asks for
// If-None-Match.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}
