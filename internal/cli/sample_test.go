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
	if got := sampleJSONBody(map[string]string{"a": "1", "a.b": "2"}, ""); got != "" {
		t.Errorf("conflicting paths = %q, want empty", got)
	}
	if got := sampleJSONBody(map[string]string{"0.id": "7", "1.id": "true"}, ""); got != `[{"id":7},{"id":true}]` {
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

func TestSampleValuesSatisfyComparisons(t *testing.T) {
	for value, want := range map[string]string{
		">100": "101", ">=100": "100", "<0.5": "0", "<=50": "50", ">1.5": "2",
		"#>2": "xxx", "#3": "xxx", "#<5": "x", "#<=0": "", "exact": "exact",
	} {
		if got, ok := sampleFor(value); !ok || got != want {
			t.Errorf("sampleFor(%q) = %q, %v; want %q", value, got, ok, want)
		}
	}
	if _, ok := sampleFor("~x+"); ok {
		t.Error("sampleFor(regexp): want no sample")
	}
	if got := sampleJSONBody(map[string]string{"amount": ">100", "items": "#>1", "code": "#fff"}, ""); got != `{"amount":101,"code":"#fff","items":["example","example"]}` {
		t.Errorf("sampleJSONBody = %s", got)
	}
	m := parseOne(t, "### A\n@query.limit = <=50\n@header.X-Count = >=2\nGET /a\n")
	sample := buildSampleRequest(m)
	if sample.Target != "/a?limit=50" || sample.Headers.Get("X-Count") != "2" {
		t.Errorf("sample = %+v", sample)
	}
}

func TestSampleJSONBodyFromDocument(t *testing.T) {
	if got := sampleJSONBody(map[string]string{"meta.id": "7"}, `{"type":"card","meta":{"x":1}}`); got != `{"meta":{"id":7,"x":1},"type":"card"}` {
		t.Fatalf("got %q", got)
	}
	if got := sampleJSONBody(nil, `[1,2]`); got != `[1,2]` {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSampleRequestHost(t *testing.T) {
	if host := buildSampleRequest(parseOne(t, "### A\n@match.host = billing.example.test\nGET /x\n")).Host; host != "billing.example.test" {
		t.Errorf("host = %q", host)
	}
	if host := buildSampleRequest(parseOne(t, "### A\n@match.host = ~.*\\.test\nGET /x\n")).Host; host != "example.test" {
		t.Errorf("regex host = %q, want example.test", host)
	}
	if host := buildSampleRequest(parseOne(t, "### A\nGET /x\n")).Host; host != "" {
		t.Errorf("no matcher host = %q, want empty", host)
	}
}
