package mockhttp

import (
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestXMLValues(t *testing.T) {
	body := `<?xml version="1.0"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <order id="o-7" status="new">
      <customer><name>Alice</name></customer>
      <item sku="A1">pen</item>
      <item sku="B2">ink</item>
    </order>
  </soap:Body>
</soap:Envelope>`
	facts := newRequestFacts(httptest.NewRequest("POST", "/", nil), requestBody{data: []byte(body)})
	tests := []struct {
		path string
		want []string
	}{
		{"Envelope.Body.order.customer.name", []string{"Alice"}},
		{"Envelope.Body.order.customer", []string{"Alice"}},
		{`Envelope.Body.order["@id"]`, []string{"o-7"}},
		{`Envelope.Body.order.item["@sku"]`, []string{"A1", "B2"}},
		{"Envelope.Body.order.item", []string{"pen", "ink"}},
		{"Envelope.*.order.*.name", []string{"Alice"}},
		{"Envelope.Body.missing", []string{}},
		{"order", []string{}}, // the first segment names the root element
		{`Envelope["@xmlns"]`, nil},
	}
	for _, tt := range tests {
		got, err := facts.xmlValues(tt.path)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("xmlValues(%q) = %q, %v; want %q", tt.path, got, err, tt.want)
		}
	}
	for _, bad := range []string{`{"a":1}`, "<a><b></a>", "<a/><b/>", "text <a/>"} {
		f := newRequestFacts(httptest.NewRequest("POST", "/", nil), requestBody{data: []byte(bad)})
		if _, err := f.xmlValues("a"); err == nil {
			t.Errorf("xmlValues on %q: error = nil", bad)
		}
	}
	blank := newRequestFacts(httptest.NewRequest("POST", "/", nil), requestBody{data: []byte("  ")})
	if values, err := blank.xmlValues("a"); err != nil || len(values) != 0 {
		t.Errorf("blank body: %q, %v", values, err)
	}
}

func TestXMLMatchersRouteRequests(t *testing.T) {
	s := newTestServer(t, `### Get quote
@xml.Envelope.Body.GetQuote.symbol = ~(?P<symbol>[A-Z]+)
POST /soap
Content-Type: text/xml

<quote symbol="{{$symbol}}" for="{{$xml.Envelope.Body.GetQuote["@account"]}}"/>

### Express order
@xml.order["@priority"] = express
@xml.order.item = pen
POST /orders

express

### No XML order
@xml.order = !
POST /orders

none

### Fallback
POST /orders

fallback
`)
	tests := []struct{ target, body, want string }{
		{"/soap", `<s:Envelope xmlns:s="urn:s"><s:Body><GetQuote account="acme"><symbol>MSFT</symbol></GetQuote></s:Body></s:Envelope>`, `<quote symbol="MSFT" for="acme"/>`},
		{"/orders", `<order priority="express"><item>ink</item><item>pen</item></order>`, "express"},
		{"/orders", `<order priority="slow"><item>pen</item></order>`, "fallback"},
		{"/orders", ``, "none"},
		{"/orders", `not xml`, "fallback"},
	}
	for _, tt := range tests {
		if _, got := serveBody(t, s, tt.target, tt.body); got != tt.want {
			t.Errorf("%s %s: got %q, want %q", tt.target, tt.body, got, tt.want)
		}
	}
	s = newTestServer(t, "### Express order\n@xml.order[\"@priority\"] = express\nPOST /orders\n\nexpress\n")
	req := httptest.NewRequest("POST", "/orders", strings.NewReader(`<order priority="slow"/>`))
	_, _, info, _ := s.findMethod(req, requestBody{data: []byte(`<order priority="slow"/>`)})
	found := false
	for _, c := range info.Candidates {
		found = found || slices.ContainsFunc(c.Reasons, func(r string) bool { return strings.Contains(r, `XML order["@priority"]: expected "express"`) })
	}
	if !found {
		t.Errorf("candidates = %+v", info.Candidates)
	}
}
