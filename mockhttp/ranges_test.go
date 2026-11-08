package mockhttp

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestFileBodiesSupportRangeRequests(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "video.bin"), "0123456789")
	writeFile(t, filepath.Join(dir, "page.txt"), "hello {{$path.id}}")
	source := filepath.Join(dir, "api.http")
	writeFile(t, source, `### Video
GET /video

< video.bin

### Templated
GET /page/:id

< page.txt

### Created
POST /upload

< video.bin

### [201] Other status
GET /status

< video.bin
`)
	methods, err := restclient.Load([]string{source})
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	serve := func(method, target string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}

	rec := serve("GET", "/video", nil)
	if rec.Code != 200 || rec.Body.String() != "0123456789" || rec.Header().Get("Accept-Ranges") != "bytes" || rec.Header().Get("Last-Modified") == "" {
		t.Fatalf("full = %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	lastModified := rec.Header().Get("Last-Modified")

	rec = serve("GET", "/video", map[string]string{"Range": "bytes=2-5"})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Errorf("range = %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	rec = serve("GET", "/video", map[string]string{"Range": "bytes=20-"})
	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("unsatisfiable = %d", rec.Code)
	}
	rec = serve("GET", "/video", map[string]string{"Range": "bytes=0-1", "If-Range": "Mon, 02 Jan 2006 15:04:05 GMT"})
	if rec.Code != 200 || rec.Body.Len() != 10 {
		t.Errorf("stale If-Range = %d %q", rec.Code, rec.Body.String())
	}
	rec = serve("GET", "/video", map[string]string{"If-Modified-Since": lastModified})
	if rec.Code != http.StatusNotModified {
		t.Errorf("If-Modified-Since = %d", rec.Code)
	}
	rec = serve("HEAD", "/video", map[string]string{"Range": "bytes=0-1"})
	if rec.Code != http.StatusPartialContent || rec.Body.Len() != 0 {
		t.Errorf("HEAD range = %d %q", rec.Code, rec.Body.String())
	}

	// Templated files, other methods and other statuses ignore Range.
	for _, tt := range []struct{ method, target string }{{"GET", "/page/7"}, {"POST", "/upload"}, {"GET", "/status"}} {
		rec := serve(tt.method, tt.target, map[string]string{"Range": "bytes=0-1"})
		if rec.Code == http.StatusPartialContent || rec.Header().Get("Accept-Ranges") != "" {
			t.Errorf("%s %s = %d %v", tt.method, tt.target, rec.Code, rec.Header())
		}
	}

	events := s.Requests(RequestFilter{})
	if len(events) == 0 {
		t.Fatal("no events")
	}
}
