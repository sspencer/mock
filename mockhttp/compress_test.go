package mockhttp

import (
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestCompressedResponses(t *testing.T) {
	s := newTestServer(t, `### Gzip
@compress = gzip
GET /gzip
Content-Type: application/json

{"hello":"{{$query.name}}"}

### Auto
@compress = auto
GET /auto

plain text body

### Deflate
@compress = deflate
GET /deflate

deflated

### Preset
@compress = gzip
GET /preset
Content-Encoding: br

already encoded

### Empty
@compress = gzip
GET /empty
`)
	serve := func(target, acceptEncoding string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		if acceptEncoding != "" {
			req.Header.Set("Accept-Encoding", acceptEncoding)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	gunzip := func(data []byte) string {
		r, err := gzip.NewReader(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		out, _ := io.ReadAll(r)
		return string(out)
	}

	rec := serve("/gzip?name=ada", "")
	if rec.Header().Get("Content-Encoding") != "gzip" || gunzip(rec.Body.Bytes()) != `{"hello":"ada"}` ||
		rec.Header().Get("Content-Length") != strconv.Itoa(rec.Body.Len()) || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("gzip = %v %q", rec.Header(), rec.Body.String())
	}

	rec = serve("/auto", "br, gzip;q=0.5")
	if rec.Header().Get("Content-Encoding") != "gzip" || gunzip(rec.Body.Bytes()) != "plain text body" ||
		rec.Header().Get("Vary") != "Accept-Encoding" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("auto gzip = %v", rec.Header())
	}
	for _, accept := range []string{"", "identity", "gzip;q=0, deflate;q=0", "br"} {
		rec = serve("/auto", accept)
		if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "plain text body" || rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("auto with %q = %v %q", accept, rec.Header(), rec.Body.String())
		}
	}
	if rec = serve("/auto", "deflate"); rec.Header().Get("Content-Encoding") != "deflate" {
		t.Errorf("auto deflate = %v", rec.Header())
	}
	if rec = serve("/auto", "*;q=1, gzip;q=0"); rec.Header().Get("Content-Encoding") != "deflate" {
		t.Errorf("auto wildcard = %v", rec.Header())
	}

	rec = serve("/deflate", "")
	zr, err := zlib.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := io.ReadAll(zr); string(out) != "deflated" || rec.Header().Get("Content-Encoding") != "deflate" {
		t.Errorf("deflate = %q %v", out, rec.Header())
	}

	if rec = serve("/preset", ""); rec.Header().Get("Content-Encoding") != "br" || rec.Body.String() != "already encoded" {
		t.Errorf("preset = %v %q", rec.Header(), rec.Body.String())
	}
	if rec = serve("/empty", ""); rec.Header().Get("Content-Encoding") != "" {
		t.Errorf("empty = %v", rec.Header())
	}
}

func TestCompressSkipsStreams(t *testing.T) {
	s := newTestServer(t, "### S\n@compress = gzip\n@stream = 1ms\nGET /s\n\na\nb\n")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest("GET", "/s", nil))
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "a\nb" {
		t.Errorf("stream = %v %q", rec.Header(), rec.Body.String())
	}
}
