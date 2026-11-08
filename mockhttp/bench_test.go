package mockhttp

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

// benchRoutes is the route count for matching benchmarks: matching scans
// routes in load order, so the cost grows with it.
const benchRoutes = 3000

// benchServer loads source and returns a server that logs nowhere.
func benchServer(b *testing.B, source string) *Server {
	b.Helper()
	methods, err := restclient.Parse("bench.http", strings.NewReader(source))
	if err != nil {
		b.Fatalf("Parse() error = %v", err)
	}
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// manyRoutes writes benchRoutes sections: static paths, path parameters and
// header matchers, so the scan exercises each kind of check.
func manyRoutes() string {
	var source strings.Builder
	for i := range benchRoutes {
		switch i % 3 {
		case 0:
			fmt.Fprintf(&source, "### Static %d\nGET /static/%d\n\nok\n\n", i, i)
		case 1:
			fmt.Fprintf(&source, "### Param %d\nGET /items%d/:id\n\n{\"id\": \"{{$id}}\"}\n\n", i, i)
		default:
			fmt.Fprintf(&source, "### Header %d\n@header.X-Tenant = t%d\nGET /tenant/%d\n\nok\n\n", i, i, i)
		}
	}
	return source.String()
}

func BenchmarkMatchFirstRoute(b *testing.B) {
	server := benchServer(b, manyRoutes())
	request := httptest.NewRequest(http.MethodGet, "/static/0", nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, ok := server.findMethod(request, requestBody{}); !ok {
			b.Fatal("findMethod() did not match")
		}
	}
}

func BenchmarkMatchLastRoute(b *testing.B) {
	server := benchServer(b, manyRoutes())
	last := benchRoutes - 1 // A header route: benchRoutes is a multiple of 3.
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/tenant/%d", last), nil)
	request.Header.Set("X-Tenant", fmt.Sprintf("t%d", last))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, ok := server.findMethod(request, requestBody{}); !ok {
			b.Fatal("findMethod() did not match")
		}
	}
}

// BenchmarkMatchMiss covers an unmatched request, which scans every route
// and then ranks near-miss candidates for the admin UI.
func BenchmarkMatchMiss(b *testing.B) {
	server := benchServer(b, manyRoutes())
	request := httptest.NewRequest(http.MethodGet, "/static/none", nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, ok := server.findMethod(request, requestBody{}); ok {
			b.Fatal("findMethod() matched")
		}
	}
}

func BenchmarkMatchJSONBody(b *testing.B) {
	var source strings.Builder
	for i := range benchRoutes {
		fmt.Fprintf(&source, "### Order %d\n@json.customer.id = %d\nPOST /orders\n\nok\n\n", i, i)
	}
	server := benchServer(b, source.String())
	body := requestBody{data: fmt.Appendf(nil, `{"customer": {"id": %d}, "items": [1, 2, 3]}`, benchRoutes-1)}
	request := httptest.NewRequest(http.MethodPost, "/orders", nil)
	request.Header.Set("Content-Type", "application/json")
	b.ReportAllocs()
	for b.Loop() {
		if _, _, _, ok := server.findMethod(request, body); !ok {
			b.Fatal("findMethod() did not match")
		}
	}
}

// benchRender renders the first route's body for a request to target.
func benchRender(b *testing.B, source, target string) {
	b.Helper()
	server := benchServer(b, source)
	request := httptest.NewRequest(http.MethodGet, target, nil)
	method, values, _, ok := server.findMethod(request, requestBody{})
	if !ok {
		b.Fatal("findMethod() did not match")
	}
	b.ReportAllocs()
	for b.Loop() {
		render := newRenderer(method, values)
		file, hasFile, err := render.file()
		if err == nil {
			_, err = render.body(file, hasFile)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRenderPlaceholders(b *testing.B) {
	benchRender(b, `### User
GET /users/:id
Content-Type: application/json

{"id": {{$id}}, "name": "{{$name}}", "email": "{{$email}}", "city": "{{$city}}", "uuid": "{{$uuid}}", "q": "{{$query.q | upper}}"}
`, "/users/7?q=search")
}

func BenchmarkRenderRepeat(b *testing.B) {
	benchRender(b, `### Users
GET /users
Content-Type: application/json

[
  {{#repeat 500}}
  {"id": {{$index | add 1}}, "name": "{{$name}}", "email": "{{$email}}"}
  {{/repeat}}
]
`, "/users")
}

func BenchmarkRenderLiteral(b *testing.B) {
	benchRender(b, "### Static\nGET /static\nContent-Type: application/json\n\n"+strings.Repeat(`{"key": "value"},`, 1000)+"\n", "/static")
}

// BenchmarkServeHTTP covers the whole request path for a templated route
// near the end of the route list: match, render, write and log.
func BenchmarkServeHTTP(b *testing.B) {
	server := benchServer(b, manyRoutes())
	target := fmt.Sprintf("/items%d/42", benchRoutes-2)
	b.ReportAllocs()
	for b.Loop() {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusOK {
			b.Fatalf("status = %d", response.Code)
		}
	}
}
