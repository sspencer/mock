package cli

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sspencer/mock/mockhttp"
	"github.com/sspencer/mock/restclient"
)

// recorder appends a draft section to a file for each request --upstream
// answered, once per method and path (-r).
type recorder struct {
	mu       sync.Mutex
	path     string
	mockPort int
	logger   *slog.Logger
	// seen holds the method and route path of every section in the file.
	seen map[string]bool
}

// newRecorder starts recording to path. Sections already in the file count
// as recorded, so a restart does not append duplicates.
func newRecorder(path string, mockPort int, logger *slog.Logger) (*recorder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	rec := &recorder{path: path, mockPort: mockPort, logger: logger, seen: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(data) > 0 {
		methods, err := restclient.Parse(path, bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("cannot record to %s: %w", path, err)
		}
		for _, method := range methods {
			rec.seen[method.Method+" "+method.EscapedPath] = true
		}
	}
	// Fail now, not on the first request, when the file can't be written.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	return rec, file.Close()
}

// record is the server's request hook.
func (rec *recorder) record(event mockhttp.RequestEvent) {
	x, ok := recordedExchange(event)
	if !ok {
		return
	}
	path, _, _ := strings.Cut(x.Target, "?")
	key := x.Method + " " + draftRoutePath(path)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.seen[key] {
		return
	}
	if err := rec.append(draftSection(x, rec.mockPort)); err != nil {
		rec.logger.Warn("recording failed", "file", rec.path, "error", err)
		return
	}
	rec.seen[key] = true
	rec.logger.Info("recorded route", "file", rec.path, "method", x.Method, "path", path, "status", event.Response.Status)
}

// append writes section to the end of the file, after a blank line when the
// file has content.
func (rec *recorder) append(section string) error {
	file, err := os.OpenFile(rec.path, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if size := info.Size(); size > 0 {
		tail := make([]byte, min(size, 2))
		if _, err := file.ReadAt(tail, size-int64(len(tail))); err != nil {
			return err
		}
		switch {
		case bytes.HasSuffix(tail, []byte("\n\n")):
		case bytes.HasSuffix(tail, []byte("\n")):
			section = "\n" + section
		default:
			section = "\n\n" + section
		}
	}
	_, err = io.WriteString(file, section)
	return err
}

// recordedExchange converts a request --upstream answered. Requests a route
// answered, callbacks, and mock's own 502 and 508 answers are not recorded.
func recordedExchange(event mockhttp.RequestEvent) (exchange, bool) {
	request, response := event.Request, event.Response
	if event.Match.Route != nil || event.Match.Upstream == "" || event.Callback != nil || request.Method == "" {
		return exchange{}, false
	}
	if response.Status == 0 || response.Fault != "" {
		return exchange{}, false
	}
	responseBody, responseOK := eventText(response.Body, response.Headers.Get("Content-Encoding"))
	if (response.Status == 502 || response.Status == 508) && strings.HasPrefix(responseBody, "mock: ") {
		return exchange{}, false
	}
	requestBody, _ := eventText(request.Body, request.Headers.Get("Content-Encoding"))
	x := exchange{
		Method:          request.Method,
		Target:          request.URL,
		RequestHeaders:  request.Headers,
		RequestBody:     requestBody,
		Status:          response.Status,
		ResponseHeaders: response.Headers,
		ResponseBody:    responseBody,
		ResponseOK:      responseOK,
	}
	if _, port, err := net.SplitHostPort(request.Host); err == nil {
		x.Port, _ = strconv.Atoi(port)
	}
	return x, true
}

// eventText returns a captured body as text, decoding base64 captures and a
// gzip or deflate Content-Encoding. It is not usable when it was truncated,
// could not be read or decoded, or is binary.
func eventText(body mockhttp.EventBody, contentEncoding string) (string, bool) {
	if body.Truncated || body.Error != "" {
		return "", false
	}
	data := []byte(body.Text)
	if body.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(body.Text)
		if err != nil {
			return "", false
		}
		data = decoded
	}
	if len(data) > 0 {
		var reader io.ReadCloser
		var err error
		switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
		case "", "identity":
		case "gzip", "x-gzip":
			reader, err = gzip.NewReader(bytes.NewReader(data))
		case "deflate":
			reader, err = zlib.NewReader(bytes.NewReader(data))
		default:
			return "", false
		}
		if err != nil {
			return "", false
		}
		if reader != nil {
			data, err = io.ReadAll(io.LimitReader(reader, 16<<20))
			reader.Close()
			if err != nil {
				return "", false
			}
		}
	}
	text := string(data)
	return text, utf8.ValidString(text) && !strings.ContainsRune(text, 0)
}
