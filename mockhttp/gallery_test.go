package mockhttp

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

// galleryServer serves one file from examples/gallery, as
// `mock examples/gallery/NAME` would.
func galleryServer(t *testing.T, name string) *Server {
	t.Helper()
	methods, err := restclient.Load([]string{filepath.Join("..", "examples", "gallery", name)})
	if err != nil {
		t.Fatal(err)
	}
	s := New(methods, slog.New(slog.DiscardHandler))
	t.Cleanup(s.Close)
	return s
}

// galleryCase is one request to a gallery example: what to send and the
// route, status and body text that should answer it.
type galleryCase struct {
	method, target string
	headers        map[string]string
	body           string
	status         int
	route          string
	contains       []string
}

func runGallery(t *testing.T, s *Server, cases []galleryCase) map[string]string {
	t.Helper()
	bodies := make(map[string]string)
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.target, strings.NewReader(c.body))
		for name, value := range c.headers {
			req.Header.Set(name, value)
		}
		if length := c.headers["Content-Length"]; length != "" {
			req.ContentLength, _ = strconv.ParseInt(length, 10, 64)
		}
		var routeName string
		s.SetRequestHook(func(e RequestEvent) {
			if e.Match.Route != nil {
				routeName = e.Match.Route.Name
			}
		})
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		body := rec.Body.String()
		label := c.method + " " + c.target
		if rec.Code != c.status || routeName != c.route {
			t.Errorf("%s = %d by %q, want %d by %q\n%s", label, rec.Code, routeName, c.status, c.route, body)
			continue
		}
		for _, want := range c.contains {
			if !strings.Contains(body, want) {
				t.Errorf("%s body lacks %q:\n%s", label, want, body)
			}
		}
		if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") && body != "" && !json.Valid([]byte(body)) {
			t.Errorf("%s body is not valid JSON:\n%s", label, body)
		}
		bodies[c.route] = body
	}
	return bodies
}

var formHeaders = map[string]string{"Content-Type": "application/x-www-form-urlencoded"}

func TestGalleryOAuth(t *testing.T) {
	s := galleryServer(t, "oauth.http")
	bodies := runGallery(t, s, []galleryCase{
		{"POST", "/oauth/token", formHeaders, "grant_type=client_credentials&client_id=demo-app&client_secret=wrong", 401, "Token request with the wrong client secret", []string{"invalid_client"}},
		{"POST", "/oauth/token", formHeaders, "grant_type=client_credentials&client_id=demo-app&client_secret=demo-secret", 200, "Client credentials grant", []string{`"token_type": "Bearer"`, `"scope": "read:orders"`}},
		{"POST", "/oauth/token", formHeaders, "grant_type=password&username=ada.l&password=x", 200, "Password grant, captured username", []string{`"refresh_token": "rt-ada.l-`}},
		{"POST", "/oauth/token", formHeaders, "grant_type=refresh_token&refresh_token=rt-ada.l-123", 200, "Refresh token grant", []string{"access_token"}},
		{"POST", "/oauth/token", formHeaders, "grant_type=refresh_token&refresh_token=nope", 400, "Expired or unknown refresh token", []string{"invalid_grant"}},
		{"POST", "/oauth/token", formHeaders, "grant_type=implicit", 400, "Unsupported grant type", []string{"unsupported_grant_type"}},
		{"POST", "/oauth/introspect", formHeaders, "token=eyJabc", 200, "Introspect an active token", []string{`"active": true`}},
		{"POST", "/oauth/introspect", formHeaders, "token=junk", 200, "Introspect anything else", []string{`"active": false`}},
		{"GET", "/userinfo", map[string]string{"Authorization": "Bearer eyJabc"}, "", 200, "User info for a bearer token", []string{`"email"`}},
		{"GET", "/userinfo", nil, "", 401, "User info without a token", []string{"invalid_token"}},
	})
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(bodies["Password grant, captured username"]), &token); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token.AccessToken, ".")
	if len(parts) != 3 || parts[2] == "" {
		t.Fatalf("access token %q is not a signed JWT", token.AccessToken)
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil || claims["sub"] != "ada.l" || claims["iss"] != "http://localhost:8080" || claims["exp"] == nil {
		t.Errorf("claims = %s", payload)
	}
}

func TestGalleryPagination(t *testing.T) {
	s := galleryServer(t, "pagination.http")
	bodies := runGallery(t, s, []galleryCase{
		{"GET", "/products?offset=20&limit=3", nil, "", 200, "Products page", []string{`"total": 25`, `"limit": 3`, `"next": "/products?offset=23&limit=3"`, `"sku": "P-021"`, `"sku": "P-023"`}},
		{"GET", "/products/7", nil, "", 200, "Product by id", []string{`"name":"Strainer"`}},
		{"GET", "/products/99", nil, "", 404, "No such product", []string{"no product 99"}},
		{"GET", "/categories/storage/products", nil, "", 200, "Products in one category", []string{`"name": "Mug"`}},
		{"GET", "/categories/toys/products", nil, "", 404, "", nil},
	})
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal([]byte(bodies["Products page"]), &page) != nil || len(page.Items) != 3 {
		t.Errorf("page = %s", bodies["Products page"])
	}
	_, all := serveRequest(t, s, "GET", "/products", "", "")
	if strings.Contains(all, `"limit"`) || strings.Count(all, `"sku"`) != 25 {
		t.Errorf("unpaged list = %s", all)
	}
	var category struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal([]byte(bodies["Products in one category"]), &category) != nil || len(category.Items) != 8 {
		t.Errorf("storage category = %s", bodies["Products in one category"])
	}
}

func multipartBody(t *testing.T, fields map[string]string, filename string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, value := range fields {
		_ = w.WriteField(name, value)
	}
	if filename != "" {
		part, _ := w.CreateFormFile("file", filename)
		_, _ = part.Write([]byte("not really an image"))
	}
	_ = w.Close()
	return w.FormDataContentType(), buf.String()
}

func TestGalleryUploads(t *testing.T) {
	s := galleryServer(t, "uploads.http")
	pngType, pngBody := multipartBody(t, map[string]string{"description": "Holiday"}, "Beach Day.PNG")
	pdfType, pdfBody := multipartBody(t, nil, "report.pdf")
	noneType, noneBody := multipartBody(t, map[string]string{"description": "x"}, "")
	runGallery(t, s, []galleryCase{
		{"POST", "/uploads", map[string]string{"Content-Type": pngType}, pngBody, 201, "Image upload", []string{`"filename": "Beach Day.PNG"`, `"description": "Holiday"`, `/Beach%20Day.PNG"`}},
		{"POST", "/uploads", map[string]string{"Content-Type": pdfType}, pdfBody, 415, "Upload of another file type", []string{"unsupported_media_type"}},
		{"POST", "/uploads", map[string]string{"Content-Type": noneType}, noneBody, 400, "Upload without a file part", []string{"missing_file"}},
		{"POST", "/uploads", map[string]string{"Content-Type": pngType, "Content-Length": "9000000"}, pngBody, 413, "Upload over 5 MB", []string{"file_too_large"}},
		{"PUT", "/uploads/logo.png", map[string]string{"Content-Type": "image/png", "Content-Length": "4"}, "\x89PNG", 201, "Raw upload", []string{`"name": "logo.png"`, `"bytes": 4`}},
		{"PUT", "/uploads/notes.txt", map[string]string{"Content-Type": "text/plain"}, "hi", 415, "Raw upload without a usable Content-Type", nil},
	})
}

func TestGalleryPayments(t *testing.T) {
	s := galleryServer(t, "payments.http")
	auth := func(extra map[string]string) map[string]string {
		headers := map[string]string{"Authorization": "Bearer sk_test_1", "Idempotency-Key": "k1", "Content-Type": "application/json"}
		for k, v := range extra {
			headers[k] = v
		}
		return headers
	}
	charge := func(number string) string {
		return `{"amount": 2500, "currency": "USD", "card": {"number": "` + number + `"}}`
	}
	runGallery(t, s, []galleryCase{
		{"POST", "/v1/payments", map[string]string{"Content-Type": "application/json"}, charge("4242424242424242"), 401, "Any request without an API key", []string{"authentication_error"}},
		{"GET", "/v1/payments/pay_1001", map[string]string{"Authorization": "Basic x"}, "", 401, "Any request with another kind of key", nil},
		{"POST", "/v1/payments", map[string]string{"Authorization": "Bearer sk_test_1"}, charge("4242424242424242"), 400, "Charge without an Idempotency-Key", nil},
		{"POST", "/v1/payments", auth(nil), charge("4242424242424242"), 201, "Successful charge", []string{`"amount": 2500`, `"currency": "usd"`, `"last4": "4242"`}},
		{"POST", "/v1/payments", auth(nil), charge("4000000000000002"), 402, "Declined card", []string{"card_declined"}},
		{"POST", "/v1/payments", auth(nil), charge("4000000000009995"), 402, "Insufficient funds", []string{"insufficient_funds"}},
		{"POST", "/v1/payments", auth(nil), charge("4000000000003220"), 200, "Card needs 3-D Secure", []string{"requires_action"}},
		{"POST", "/v1/payments", auth(nil), charge("4000000000000119"), 500, "Processing error", []string{"processing_error"}},
		{"POST", "/v1/payments", auth(nil), `{"amount": 0, "card": {"number": "4242424242424242"}}`, 400, "Charge with a bad amount", []string{`"param": "amount"`}},
		{"POST", "/v1/payments", auth(nil), charge("12"), 400, "Charge with a bad card number", []string{"invalid_number"}},
		{"GET", "/v1/payments/pay_1002", auth(nil), "", 200, "Payment by id", []string{`"status":"refunded"`}},
		{"GET", "/v1/payments/pay_9", auth(nil), "", 404, "No such payment", []string{"no payment pay_9"}},
		{"POST", "/v1/payments/pay_1001/refunds", auth(nil), "", 201, "Refund a payment", []string{`"amount": 2500`, `"payment": "pay_1001"`}},
		{"POST", "/v1/payments/pay_1001/refunds", auth(nil), `{"amount": 100}`, 201, "Refund a payment", []string{`"amount": 100`}},
		{"POST", "/v1/payments/pay_9/refunds", auth(nil), "", 404, "No such payment", nil},
	})

	// The 21st charge in a second from one API key is rate limited; another
	// key is not.
	var last *httptest.ResponseRecorder
	for range 20 {
		req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(charge("4242424242424242")))
		for k, v := range auth(map[string]string{"Authorization": "Bearer sk_test_burst"}) {
			req.Header.Set(k, v)
		}
		last = httptest.NewRecorder()
		s.ServeHTTP(last, req)
	}
	if last.Code != 201 || last.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("20th charge = %d, remaining %q", last.Code, last.Header().Get("X-RateLimit-Remaining"))
	}
	req := httptest.NewRequest("POST", "/v1/payments", strings.NewReader(charge("4242424242424242")))
	for k, v := range auth(map[string]string{"Authorization": "Bearer sk_test_burst"}) {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 429 {
		t.Errorf("21st charge = %d, want 429", rec.Code)
	}

	// With chaos on, about one charge in ten fails with 503.
	failures := 0
	for range 300 {
		code, _ := serveRequest(t, s, "POST", "/v1/payments", "application/json", charge("4242424242424242"),
			"Authorization", "Bearer sk_test_chaos"+strings.Repeat("x", failures%7), "Idempotency-Key", "k", "X-Mock-Chaos", "on")
		if code == 503 {
			failures++
		}
	}
	if failures < 10 || failures > 60 {
		t.Errorf("chaos failed %d of 300 charges", failures)
	}
}

func TestGalleryWebhooks(t *testing.T) {
	old := callbackBackoff
	callbackBackoff = 10 * time.Millisecond
	t.Cleanup(func() { callbackBackoff = old })

	type delivery struct{ event, signature, body string }
	var mu sync.Mutex
	var got []delivery
	failFirst := true
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		got = append(got, delivery{r.Header.Get("X-Event"), r.Header.Get("X-Signature"), string(body)})
		if failFirst {
			failFirst = false
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	s := galleryServer(t, "webhooks.http")
	runGallery(t, s, []galleryCase{
		{"POST", "/webhook_endpoints", map[string]string{"Content-Type": "application/json"}, `{"url": "https://example.com/hooks"}`, 201, "Register a webhook endpoint", []string{`"secret": "whsec_test_secret"`}},
		{"POST", "/webhook_endpoints", map[string]string{"Content-Type": "application/json"}, `{}`, 422, "Register an endpoint without a URL", nil},
		{"POST", "/orders", map[string]string{"Content-Type": "application/json"}, `{"id": "ord_1"}`, 422, "Create an order without a webhook URL", nil},
		{"POST", "/orders", map[string]string{"Content-Type": "application/json"}, `{"id": "ord_1", "webhook_url": "` + receiver.URL + `/hooks"}`, 202, "Create an order, then send its webhooks", []string{`"status": "pending"`}},
	})
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("deliveries = %+v", got)
	}
	signature := "sha256=" + applyFilter(templateFilter{name: "hmac", args: []string{"whsec_test_secret"}}, "ord_1")
	for i, want := range []string{"order.created", "order.created", "order.shipped"} {
		if got[i].event != want || got[i].signature != signature || !json.Valid([]byte(got[i].body)) || !strings.Contains(got[i].body, `"id": "ord_1"`) {
			t.Errorf("delivery %d = %+v, want event %s signed %s", i, got[i], want, signature)
		}
	}
}
