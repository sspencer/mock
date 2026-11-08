package mockhttp

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

// exampleScenario is one request from examples/matching/requests.sh with the
// route that should answer it.
type exampleScenario struct {
	description string
	method      string
	path        string
	headers     map[string]string
	body        string
	status      int
	route       string
}

const exampleSignature = "sha256=0000000000000000000000000000000000000000000000000000000000000000"

var (
	jsonHeader = map[string]string{"Content-Type": "application/json"}
	formHeader = map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	signedJSON = map[string]string{"Content-Type": "application/json", "X-Signature": exampleSignature}
)

// matchingScenarios mirrors the show lines in examples/matching/requests.sh;
// TestMatchingExampleScenariosCoverScript keeps the two lists in step.
var matchingScenarios = []exampleScenario{
	{"Bearer token, captured into the body", "GET", "/account/me", map[string]string{"Authorization": "Bearer abc123"}, "", 200, "Profile for a bearer token"},
	{"No Authorization header", "GET", "/account/me", nil, "", 401, "Profile without credentials"},
	{"Basic auth instead of Bearer", "GET", "/account/me", map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}, "", 403, "Profile with another auth scheme"},
	{"Accept: text/csv (case-insensitive)", "GET", "/account/report", map[string]string{"Accept": "TEXT/CSV"}, "", 200, "Report as CSV"},
	{"Default Accept", "GET", "/account/report", nil, "", 200, "Report as JSON (default)"},
	{"X-Api-Version: 2", "GET", "/account/settings", map[string]string{"X-Api-Version": "2"}, "", 200, "Settings, API version 2"},
	{"No version header", "GET", "/account/settings", nil, "", 200, "Settings, no version header (defaults to version 1)"},
	{"X-Api-Version: 9", "GET", "/account/settings", map[string]string{"X-Api-Version": "9"}, "", 400, "Settings, unknown API version"},
	{"X-Tenant: acme-eu", "GET", "/account/usage", map[string]string{"X-Tenant": "acme-eu"}, "", 200, "Usage for a tenant"},
	{"X-Tenant: Not Valid", "GET", "/account/usage", map[string]string{"X-Tenant": "Not Valid"}, "", 400, "Usage without a tenant"},

	{"Express shipping, echoes customer name", "POST", "/orders", jsonHeader, `{"customer":{"name":"Avery"},"shipping":{"method":"express"},"items":[{"sku":"TEA-001","qty":2}]}`, 201, "Express order"},
	{"VIP customer (boolean true)", "POST", "/orders", jsonHeader, `{"customer":{"vip":true},"items":[{"sku":"TEA-002","qty":1}]}`, 201, "VIP order"},
	{"Gift card in any item, amount captured", "POST", "/orders", jsonHeader, `{"items":[{"sku":"TEA-001","qty":1},{"sku":"GIFT-50","qty":1}]}`, 201, "Gift card order"},
	{"Standard order, items copied back", "POST", "/orders", jsonHeader, `{"items":[{"sku":"TEA-003","qty":3}]}`, 201, "Standard order"},
	{"No items", "POST", "/orders", jsonHeader, `{"customer":{"name":"Avery"}}`, 422, "Order without items"},
	{"Not JSON", "POST", "/orders", jsonHeader, `items=TEA-001`, 400, "Order that is not valid JSON"},
	{"Numeric order id", "GET", "/orders/1001", nil, "", 200, "Order status by numeric id"},
	{"Non-numeric order id", "GET", "/orders/latest", nil, "", 404, "Order status for anything else"},
	{"qty 3", "PATCH", "/orders/1001", jsonHeader, `{"qty":3}`, 200, "Update quantity, with a numeric check"},
	{"qty 3.5", "PATCH", "/orders/1001", jsonHeader, `{"qty":3.5}`, 422, "Update with a bad quantity"},

	{"client_credentials, client captured", "POST", "/auth/token", formHeader, "grant_type=client_credentials&client_id=acme-web&scope=read+write", 200, "Token for client credentials"},
	{"password grant", "POST", "/auth/token", formHeader, "grant_type=password&username=avery&password=hunter2", 200, "Token for a password grant"},
	{"refresh_token grant", "POST", "/auth/token", formHeader, "grant_type=refresh_token&refresh_token=rt_9f8e7d6c5b", 200, "Token refresh"},
	{"No grant_type", "POST", "/auth/token", formHeader, "client_id=acme-web", 400, "Token request without a grant type"},
	{"Unsupported grant", "POST", "/auth/token", formHeader, "grant_type=device_code", 400, "Token request with an unsupported grant"},
	{"Contact form from a person", "POST", "/auth/contact", formHeader, "name=Avery&email=avery@example.test&message=Hello&website=", 303, "Contact form from a person"},
	{"Contact form from a bot (honeypot)", "POST", "/auth/contact", formHeader, "name=Bot&email=bot@spam.test&message=Buy&website=spam.test", 200, "Contact form from a bot"},
	{"Contact form without an email", "POST", "/auth/contact", formHeader, "name=Avery&message=Hello", 422, "Contact form with missing fields"},

	{"Numeric product id", "GET", "/catalog/products/7", nil, "", 200, "Product by numeric id"},
	{"Product slug, parts captured", "GET", "/catalog/products/green-tea-250g", nil, "", 200, "Product by slug"},
	{"Unknown product id", "GET", "/catalog/products/Green_Tea", nil, "", 404, "Product that matches neither form"},
	{"Search with q and page", "GET", "/catalog/search?q=green&page=2", nil, "", 200, "Search results"},
	{"Search with q only", "GET", "/catalog/search?q=green", nil, "", 200, "Search results, first page"},
	{"Search without q", "GET", "/catalog/search", nil, "", 200, "Search page without a query"},
	{"Search with a bad page", "GET", "/catalog/search?q=green&page=zero", nil, "", 400, "Search with a bad page number"},
	{"Image under a nested path", "GET", "/catalog/images/teas/green/hero.webp", nil, "", 200, "Product image"},
	{"Non-image file", "GET", "/catalog/images/teas/notes.txt", nil, "", 415, "Image path that is not an image"},
	{"Anything else under /catalog", "GET", "/catalog/brands/acme", nil, "", 404, "Anything else under /catalog"},

	{"Signed payment.succeeded", "POST", "/hooks/payments", signedJSON, `{"type":"payment.succeeded","data":{"id":"pay_123"}}`, 200, "Payment succeeded"},
	{"Signed payment.canceled", "POST", "/hooks/payments", signedJSON, `{"type":"payment.canceled","data":{"id":"pay_124","reason":"customer request"}}`, 200, "Payment failed"},
	{"Signed, other event type", "POST", "/hooks/payments", signedJSON, `{"type":"payout.created","data":{"id":"po_1"}}`, 200, "Other signed payment events"},
	{"Unsigned", "POST", "/hooks/payments", jsonHeader, `{"type":"payment.succeeded"}`, 401, "Payment webhook without a valid signature"},
	{"Malformed signature", "POST", "/hooks/payments", map[string]string{"Content-Type": "application/json", "X-Signature": "md5=abc"}, `{"type":"payment.succeeded"}`, 400, "Payment webhook with a malformed signature"},
	{"Text containing PING on a later line", "POST", "/hooks/text", map[string]string{"Content-Type": "text/plain"}, "hello\nPING\nbye", 200, "Plain-text ping"},
	{"XML status, value captured", "POST", "/hooks/text", map[string]string{"Content-Type": "application/xml"}, "<report><status>healthy</status></report>", 200, "XML status report"},
	{"Empty body", "POST", "/hooks/text", nil, "", 204, "Empty keep-alive"},
	{"Anything else", "POST", "/hooks/text", map[string]string{"Content-Type": "text/plain"}, "ping", 422, "Unrecognized text"},
}

// TestMatchingExampleScenarios sends every request from
// examples/matching/requests.sh to an in-process server loaded with
// examples/matching/*.http and checks the status and answering route.
func TestMatchingExampleScenarios(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "examples", "matching", "*.http"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no matching examples found: %v", err)
	}
	methods, err := restclient.Load(files)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	server := New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, sc := range matchingScenarios {
		t.Run(sc.description, func(t *testing.T) {
			var body io.Reader
			if sc.body != "" {
				body = strings.NewReader(sc.body)
			}
			request := httptest.NewRequest(sc.method, sc.path, body)
			for name, value := range sc.headers {
				request.Header.Set(name, value)
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)

			if response.Code != sc.status {
				t.Errorf("status = %d, want %d; body %q", response.Code, sc.status, response.Body.String())
			}
			event := lastEvent(t, server)
			if event.Match.Route == nil {
				t.Fatalf("request was not matched, want route %q", sc.route)
			}
			if event.Match.Route.Name != sc.route {
				t.Errorf("route = %q, want %q", event.Match.Route.Name, sc.route)
			}
		})
	}
}

// TestMatchingExampleScenariosCoverScript fails when requests.sh gains,
// loses or renames a scenario without the table above following.
func TestMatchingExampleScenariosCoverScript(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "examples", "matching", "requests.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var inScript []string
	for _, m := range regexp.MustCompile(`(?m)^show '([^']+)'`).FindAllStringSubmatch(string(script), -1) {
		inScript = append(inScript, m[1])
	}
	var inTable []string
	for _, sc := range matchingScenarios {
		inTable = append(inTable, sc.description)
	}
	if !slices.Equal(inScript, inTable) {
		t.Fatalf("requests.sh scenarios differ from matchingScenarios:\nscript: %q\ntable:  %q", inScript, inTable)
	}
}
