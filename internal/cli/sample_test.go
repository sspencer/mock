package cli

import (
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func parseOne(t *testing.T, source string) restclient.Method {
	t.Helper()
	methods, err := restclient.Parse("test.http", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return methods[0]
}

func TestBuildSampleRequest(t *testing.T) {
	sample := buildSampleRequest(parseOne(t, `### Order
@path.region = eu west
@query.page = *
@query.debug = !
@header.Authorization = ~^Bearer
@header.X-Tenant = acme
@cookie.session = *
@json.customer.id = 42
@json.items.*.sku = ~^A
@json.note = !
POST /regions/:region/users/:user_id/orders/:slug?v=2
`))
	if sample.Method != "POST" {
		t.Errorf("method = %q", sample.Method)
	}
	if want := "/regions/eu%20west/users/1/orders/example?page=1&v=2"; sample.Target != want {
		t.Errorf("target = %q, want %q", sample.Target, want)
	}
	if sample.Headers.Get("Authorization") != "example" || sample.Headers.Get("X-Tenant") != "acme" ||
		sample.Headers.Get("Cookie") != "session=example" || sample.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", sample.Headers)
	}
	if want := `{"customer":{"id":42},"items":[{"sku":"example"}]}`; sample.Body != want {
		t.Errorf("body = %s, want %s", sample.Body, want)
	}
}

func TestBuildSampleRequestBodies(t *testing.T) {
	form := buildSampleRequest(parseOne(t, "### Login\n@form.user = ann\n@form.code = *\nPOST /login\n"))
	if form.Body != "code=example&user=ann" || form.Headers.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("form sample = %q %v", form.Body, form.Headers)
	}
	raw := buildSampleRequest(parseOne(t, "### Ping\n@body = ping\nPOST /ping\n"))
	if raw.Body != "ping" || raw.Headers.Get("Content-Type") != "" {
		t.Errorf("raw sample = %q %v", raw.Body, raw.Headers)
	}
	wild := buildSampleRequest(parseOne(t, "### Files\n@path.rest = a b/c\nGET /files/*rest\n"))
	if wild.Target != "/files/a%20b/c" {
		t.Errorf("wildcard target = %q", wild.Target)
	}
	if got := sampleJSONBody(map[string]string{"a": "1", "a.b": "2"}); got != "" {
		t.Errorf("conflicting paths = %q, want empty", got)
	}
	if got := sampleJSONBody(map[string]string{"0.id": "7", "1.id": "true"}); got != `[{"id":7},{"id":true}]` {
		t.Errorf("array root = %q", got)
	}
}

func TestBuildSampleRequestMethodLists(t *testing.T) {
	for source, want := range map[string]string{
		"### Health\nOPTIONS,GET /health\n": "OPTIONS",
		"### Any\n* /anything\n":            "GET",
	} {
		if got := buildSampleRequest(parseOne(t, source)).Method; got != want {
			t.Errorf("%q sample method = %q, want %q", source, got, want)
		}
	}
}
