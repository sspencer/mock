package mockhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jaswdr/faker"
	"github.com/sspencer/mock/restclient"
)

// warnInvalidJSONBody warns when a route that answers with a JSON content type
// renders a body that does not parse. The body is rendered without a request:
// placeholders that read the request become 1, which is valid JSON both bare
// and inside a string, so only mistakes in the template itself are reported.
func warnInvalidJSONBody(logger *slog.Logger, method restclient.Method) {
	body, file, ok := sampleJSONBody(method)
	if !ok {
		return
	}
	err := json.Unmarshal(body, new(any))
	if err == nil {
		return
	}
	message := "response body is not valid JSON: " + err.Error()
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		message += fmt.Sprintf(" (line %d of the rendered body)", 1+bytes.Count(body[:min(int(syntax.Offset), len(body))], []byte("\n")))
	}
	args := []any{"source", method.Source, "line", method.Line, "method", method.Name}
	if file != "" {
		args = append(args, "file", file)
	}
	logger.Warn(message, args...)
}

// sampleTime is the clock for sample renders, so lint results are stable.
var sampleTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// sampleJSONBody renders a JSON route's body with sample request values. ok is
// false when the route is not JSON, has no body, or its body cannot be known
// before a request (a templated file path) or read.
func sampleJSONBody(method restclient.Method) (body []byte, file string, ok bool) {
	status := method.Status
	if status == 0 {
		status = 200
	}
	if !statusAllowsBody(status) {
		return nil, "", false
	}
	path := ""
	if method.File != "" {
		if method.Body != "" || restclient.FileIsTemplate(method.File) {
			return nil, "", false
		}
		path = restclient.ResolveFile(method.Source, method.File)
	}
	r := &renderer{method: &method, root: &renderScope{}, active: make(map[string]bool), sample: true}
	if !r.isJSON(path) {
		return nil, "", false
	}
	r.rng = rand.New(rand.NewPCG(1, 2))
	r.faker = faker.Faker{Generator: rngGenerator{r.rng}}
	r.now = sampleTime
	if path == "" {
		if strings.TrimSpace(method.Body) == "" {
			return nil, "", false
		}
		if method.Template == "off" {
			return []byte(method.Body), "", true
		}
		rendered, err := r.text(method.Body, true)
		return []byte(rendered), "", err == nil
	}
	raw, err := readResponseFile(method.Source, method.File)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil, "", false
	}
	if !fileIsTemplated(method, path, raw) {
		return raw, method.File, true
	}
	rendered, err := r.text(string(raw), true)
	return []byte(rendered), method.File, err == nil
}
