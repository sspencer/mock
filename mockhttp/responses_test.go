package mockhttp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

// newDirServer writes files into a temporary directory and serves the
// request file text as if it were api.http in that directory.
func newDirServer(t *testing.T, files map[string]string, input string) *Server {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	methods, err := restclient.Parse(filepath.Join(dir, "api.http"), strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	return New(methods, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestStatusFromPlaceholder(t *testing.T) {
	s := newTestServer(t, `### [{{$query.status | default 200}}] Echo status
GET /status

status

### Created
@status = {{$json.code}}
POST /things

made
`)
	for target, want := range map[string]int{"/status?status=418": 418, "/status": 200, "/status?status=abc": 500, "/status?status=99": 500} {
		if code, _ := serveRequest(t, s, "GET", target, "", ""); code != want {
			t.Errorf("GET %s = %d, want %d", target, code, want)
		}
	}
	if code, body := serveRequest(t, s, "POST", "/things", "application/json", `{"code":201}`); code != 201 || body != "made" {
		t.Errorf("POST /things = %d %q", code, body)
	}
}

func TestIfBlocks(t *testing.T) {
	s := newTestServer(t, `### User
GET /users/:id
Content-Type: application/json

{
  "id": "{{$path.id}}",
{{#if $query.expand}}
  "profile": {"bio": "hi"},
{{/if}}
{{#if $query.role == "site admin"}}
  "admin": true,
{{else}}
  "admin": false,
{{/if}}
{{#if $path.id != 1}}
  "guest": true,
{{/if}}
  "end": 1
}
`)
	decode := func(target string) map[string]any {
		t.Helper()
		_, body := serveRequest(t, s, "GET", target, "", "")
		var out map[string]any
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("GET %s: %v\n%s", target, err, body)
		}
		return out
	}
	plain := decode("/users/1")
	if _, ok := plain["profile"]; ok || plain["admin"] != false || plain["guest"] != nil {
		t.Errorf("plain = %v", plain)
	}
	for _, off := range []string{"false", "0", ""} {
		if got := decode("/users/1?expand=" + off); got["profile"] != nil {
			t.Errorf("expand=%q rendered the block: %v", off, got)
		}
	}
	full := decode("/users/2?expand=yes&role=site%20admin")
	if full["profile"] == nil || full["admin"] != true || full["guest"] != true {
		t.Errorf("full = %v", full)
	}
}

func TestIfBlockProblemsStayLiteral(t *testing.T) {
	for input, want := range map[string]string{
		"a {{else}} b":                         "{{else}} is only allowed once",
		"{{#if $query.x}} a":                   "{{#if}} has no matching {{/if}}",
		"{{#if $query.x ~ y}}a{{/if}}":         "{{#if}} needs a reference",
		"{{#if}}a{{/if}}":                      "{{#if}} needs a reference",
		"{{#if $q}}a{{else}}b{{else}}c{{/if}}": "{{else}} is only allowed once",
	} {
		_, problems := parseTemplate(input)
		if len(problems) == 0 || !strings.Contains(problems[0].message, want) {
			t.Errorf("parseTemplate(%q) problems = %+v, want %q", input, problems, want)
		}
	}
	nodes, _ := parseTemplate("{{#if $query.x}}yes{{else}}no")
	r := newRenderer(&restclient.Method{}, templateValues{})
	out := &templateWriter{}
	if err := r.render(nodes, r.root, out); err != nil || string(out.buf) != "{{#if $query.x}}yes{{else}}no" {
		t.Errorf("unclosed if rendered %q, %v", out.buf, err)
	}
}

const usersFixture = `{"users": [{"id": 1, "name": "Ada"}, {"id": 2, "name": "Bob"}, {"id": 3, "name": "Cy"}, {"id": 4, "name": "Di"}]}`

func TestFixtureLookupByKeyAndRequire(t *testing.T) {
	s := newDirServer(t, map[string]string{"users.json": usersFixture}, `@fixture = users.json

### User
@require = $fixture.users[id=$path.id]
GET /users/:id
Content-Type: application/json

{"name": "{{$fixture.users[id=$path.id].name}}"}

### [404] No such user
GET /users/:id

{"error": "not found"}

### Order line
POST /orders
Content-Type: application/json

{"qty": {{$json.items[sku=A1].qty}}}
`)
	if code, body := serveRequest(t, s, "GET", "/users/2", "", ""); code != 200 || body != `{"name": "Bob"}` {
		t.Errorf("GET /users/2 = %d %s", code, body)
	}
	if code, body := serveRequest(t, s, "GET", "/users/9", "", ""); code != 404 || body != `{"error": "not found"}` {
		t.Errorf("GET /users/9 = %d %s", code, body)
	}
	if _, body := serveRequest(t, s, "POST", "/orders", "application/json", `{"items":[{"sku":"B2","qty":1},{"sku":"A1","qty":5}]}`); body != `{"qty": 5}` {
		t.Errorf("POST /orders body = %s", body)
	}
}

func TestRequireNearMissAndMatcherSelector(t *testing.T) {
	s := newDirServer(t, map[string]string{"users.json": usersFixture}, `@fixture = users.json

### User
@require = {{$fixture.users[id=$path.id]}}
GET /users/:id

ok

### Two
@json.items[sku=A1].qty = 2
POST /cart

two
`)
	var event RequestEvent
	s.SetRequestHook(func(e RequestEvent) { event = e })
	if code, _ := serveRequest(t, s, "GET", "/users/42", "", ""); code != 404 {
		t.Fatalf("status = %d", code)
	}
	if len(event.Match.Candidates) == 0 || !strings.Contains(event.Match.Candidates[0].Reasons[0], "Required $fixture.users[id=$path.id] has no value") {
		t.Errorf("candidates = %+v", event.Match.Candidates)
	}
	if code, body := serveRequest(t, s, "POST", "/cart", "application/json", `{"items":[{"sku":"A1","qty":2}]}`); code != 200 || body != "two" {
		t.Errorf("POST /cart = %d %s", code, body)
	}
	if code, _ := serveRequest(t, s, "POST", "/cart", "application/json", `{"items":[{"sku":"A1","qty":3}]}`); code != 404 {
		t.Errorf("POST /cart with qty 3 = %d", code)
	}
}

func TestEachPagingAndLength(t *testing.T) {
	s := newDirServer(t, map[string]string{"users.json": usersFixture}, `### Users
@fixture = users.json
GET /users
Content-Type: application/json

{"total": {{$fixture.users | length}}, "names": [{{#each $fixture.users | offset $query.offset | limit $query.limit}}"{{$item.name}}"{{/each}}]}
`)
	for target, want := range map[string]string{
		"/users":                   `{"total": 4, "names": ["Ada","Bob","Cy","Di"]}`,
		"/users?offset=1&limit=2":  `{"total": 4, "names": ["Bob","Cy"]}`,
		"/users?offset=3&limit=10": `{"total": 4, "names": ["Di"]}`,
		"/users?offset=9":          `{"total": 4, "names": []}`,
		"/users?limit=x":           `{"total": 4, "names": ["Ada","Bob","Cy","Di"]}`,
		"/users?offset=-2&limit=1": `{"total": 4, "names": ["Ada"]}`,
	} {
		if _, body := serveRequest(t, s, "GET", target, "", ""); body != want {
			t.Errorf("GET %s = %s, want %s", target, body, want)
		}
	}
	for _, tag := range []string{"{{#each $fixture.users | skip 1}}", "{{#each $fixture.users | limit}}", "{{#each $fixture.users | limit many}}"} {
		if _, problems := parseTemplate(tag + "x{{/each}}"); len(problems) == 0 {
			t.Errorf("%s parsed without a problem", tag)
		}
	}
}

func TestNewFilters(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("k"))
	mac.Write([]byte("abc"))
	for text, want := range map[string]string{
		`{{$query.v | length}}`:                 "5",
		`{{$query.a | length}}`:                 "3",
		`{{$query.v | slice 1 3}}`:              "él",
		`{{$query.v | slice -2}}`:               "lo",
		`{{$query.v | slice 9}}`:                "",
		`{{$query.v | replace l L}}`:            "héLLo",
		`{{$query.b | base64decode}}`:           "hi there",
		`{{$query.s | sha256}}`:                 "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		`{{$query.s | hmac k}}`:                 hexString(mac.Sum(nil)),
		`{{$query.s | hmac $query.key}}`:        hexString(mac.Sum(nil)),
		`{{$query.n | int}}`:                    "-3",
		`{{$query.n | round}}`:                  "-4",
		`{{$query.n | round 1}}`:                "-3.7",
		`{{$query.p | round 2}}`:                "2.00",
		`{{$query.i | add 3}}`:                  "10",
		`{{$query.i | sub 10}}`:                 "-3",
		`{{$query.i | mul $query.i}}`:           "49",
		`{{$query.i | mul 1.5}}`:                "10.5",
		`{{$query.s | add 1}}`:                  "abc",
		`{{$query.t | date date}}`:              "2024-03-05",
		`{{$query.t | date unix}}`:              "1709640000",
		`{{$query.u | date iso8601}}`:           "2024-03-05T12:00:00Z",
		`{{$query.u | date rfc1123}}`:           "Tue, 05 Mar 2024 12:00:00 GMT",
		`{{$query.u | date "Jan 2, 2006"}}`:     "Mar 5, 2024",
		`{{$query.missing | default $query.s}}`: "abc",
	} {
		s := newTestServer(t, "### F\nGET /f\n\n"+text+"\n")
		target := "/f?v=h%C3%A9llo&a=%5B1%2C2%2C3%5D&b=aGkgdGhlcmU%3D&s=abc&key=k&n=-3.7&p=2&i=7&t=2024-03-05T12:00:00Z&u=1709640000"
		if _, body := serveRequest(t, s, "GET", target, "", ""); body != want {
			t.Errorf("%s = %q, want %q", text, body, want)
		}
	}
	for _, text := range []string{"{{$x | sub}}", "{{$x | mul two}}", "{{$x | round 1.5}}", "{{$x | slice a}}", "{{$x | replace a}}", "{{$x | nope}}"} {
		if _, problems := parseTemplate(text); len(problems) == 0 {
			t.Errorf("%s parsed without a problem", text)
		}
	}
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

func TestNewGenerators(t *testing.T) {
	s := newTestServer(t, `### G
GET /g
Content-Type: application/json

{"one": "{{$oneOf pending paid "on hold"}}", "f": {{$randomFloat 1 2 3}}, "d": "{{$randomDate 2024-01-01 2024-01-03}}", "t": "{{$randomDate 2024-01-01T00:00:00Z 2024-01-01T01:00:00Z}}",
 "company": "{{$company}}", "city": "{{$city}}", "country": "{{$country}}", "address": "{{$address}}", "ip": "{{$ip}}", "word": "{{$word}}", "price": "{{$price}}", "ulid": "{{$ulid}}"}
`)
	ulid := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	for range 20 {
		_, body := serveRequest(t, s, "GET", "/g", "", "")
		var out map[string]any
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("%v\n%s", err, body)
		}
		if one := out["one"]; one != "pending" && one != "paid" && one != "on hold" {
			t.Errorf("oneOf = %v", one)
		}
		if f := out["f"].(float64); f < 1 || f > 2 {
			t.Errorf("randomFloat = %v", f)
		}
		if d := out["d"]; d != "2024-01-01" && d != "2024-01-02" && d != "2024-01-03" {
			t.Errorf("randomDate = %v", d)
		}
		if ts, err := time.Parse(time.RFC3339, out["t"].(string)); err != nil || ts.Hour() != 0 {
			t.Errorf("randomDate time = %v (%v)", out["t"], err)
		}
		for _, key := range []string{"company", "city", "country", "address", "ip", "word"} {
			if out[key] == "" {
				t.Errorf("%s is empty", key)
			}
		}
		if price, err := strconv.ParseFloat(out["price"].(string), 64); err != nil || price < 1 || price >= 1000 || !strings.Contains(out["price"].(string), ".") {
			t.Errorf("price = %v", out["price"])
		}
		if !ulid.MatchString(out["ulid"].(string)) {
			t.Errorf("ulid = %v", out["ulid"])
		}
	}
	seeded := newTestServer(t, "### U\n@seed = 1\nGET /u\n\n{{$ulid}} {{$oneOf a b c d e f}}\n")
	_, first := serveRequest(t, seeded, "GET", "/u", "", "")
	_, second := serveRequest(t, seeded, "GET", "/u", "", "")
	if first != second || !strings.HasPrefix(first, "01JGFJJZ00") {
		t.Errorf("seeded ulid and oneOf differ or drift with the clock: %q, %q", first, second)
	}
	for _, text := range []string{"{{$oneOf}}", "{{$randomFloat 2 1}}", "{{$randomDate 2024-02-01 2024-01-01}}"} {
		var logs strings.Builder
		methods, _ := restclient.Parse("t.http", strings.NewReader("### X\nGET /x\n\n"+text+"\n"))
		New(methods, slog.New(slog.NewTextHandler(&logs, nil)))
		if !strings.Contains(logs.String(), "invalid placeholder arguments") {
			t.Errorf("%s: no load warning:\n%s", text, logs.String())
		}
	}
}

func decodeJWT(t *testing.T, token string) (header, claims map[string]any, signature string) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token %q has %d parts", token, len(parts))
	}
	for i, out := range []*map[string]any{&header, &claims} {
		data, err := base64.RawURLEncoding.DecodeString(parts[i])
		if err != nil || json.Unmarshal(data, out) != nil {
			t.Fatalf("part %d of %q does not decode", i, token)
		}
	}
	return header, claims, parts[2]
}

func TestJWT(t *testing.T) {
	t.Setenv("MOCK_TEST_JWT_SECRET", "s3cret")
	s := newTestServer(t, `### Login
@jwt.secret = {{$env.MOCK_TEST_JWT_SECRET}}
POST /login

{{$jwt sub=$json.user uid=$json.id admin=true name="Ada Lovelace" exp=1h missing=$json.nope}}

### Anonymous
GET /anon

{{$jwt sub=42}}
`)
	_, token := serveRequest(t, s, "POST", "/login", "application/json", `{"user":"7","id":7}`)
	header, claims, signature := decodeJWT(t, token)
	if header["alg"] != "HS256" || header["typ"] != "JWT" {
		t.Errorf("header = %v", header)
	}
	iat, _ := claims["iat"].(float64)
	if claims["sub"] != "7" || claims["uid"] != float64(7) || claims["admin"] != true || claims["name"] != "Ada Lovelace" || claims["exp"] != iat+3600 || claims["missing"] != nil {
		t.Errorf("claims = %v", claims)
	}
	if time.Since(time.Unix(int64(iat), 0)) > time.Minute {
		t.Errorf("iat = %v", iat)
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(token[:strings.LastIndex(token, ".")]))
	if signature != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("signature does not verify")
	}
	_, unsigned := serveRequest(t, s, "GET", "/anon", "", "")
	header, claims, signature = decodeJWT(t, unsigned)
	if header["alg"] != "none" || signature != "" || claims["sub"] != "42" {
		t.Errorf("unsigned token %q: header %v, claims %v", unsigned, header, claims)
	}
}

func TestCSVAndNDJSONFixtures(t *testing.T) {
	s := newDirServer(t, map[string]string{
		"users.csv":    "\xef\xbb\xbfid,name,city\n1,Ada,London\n2,\"Bob, Jr\",Paris\n3,Cy\n",
		"events.jsonl": "{\"type\":\"a\",\"n\":1}\n\n{\"type\":\"b\",\"n\":2}\n",
	}, `### CSV
@fixture = users.csv
GET /csv

{{#each $fixture}}{{$item.id}}:{{$item.name}}:{{$item.city | default -}};{{/each}} {{$fixture[id=2].name}}

### NDJSON
@fixture = events.jsonl
GET /ndjson
Content-Type: application/json

[{{#each $fixture}}{{$item.n}}{{/each}}]
`)
	if _, body := serveRequest(t, s, "GET", "/csv", "", ""); body != "1:Ada:London;2:Bob, Jr:Paris;3:Cy:-; Bob, Jr" {
		t.Errorf("CSV = %q", body)
	}
	if _, body := serveRequest(t, s, "GET", "/ndjson", "", ""); body != "[1,2]" {
		t.Errorf("NDJSON = %q", body)
	}
	if _, err := decodeCSV([]byte("a,b\n1,2,3\n")); err == nil {
		t.Error("a CSV row longer than the header decoded")
	}
	if _, err := decodeNDJSON([]byte("{}\n{oops}\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("bad NDJSON error = %v", err)
	}
	for _, method := range s.Methods() {
		rec := httptest.NewRecorder()
		s.ServeFixture(rec, httptest.NewRequest("GET", "/fixture?route="+describeRoute(method, 0, 1).ID, nil))
		_ = rec
	}
	rec := httptest.NewRecorder()
	s.ServeFixture(rec, httptest.NewRequest("GET", "/fixture?route=1:0", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("GET /fixture for a CSV fixture = %d, want 409", rec.Code)
	}
}

func TestETag(t *testing.T) {
	s := newDirServer(t, map[string]string{"page.html": "<p>static</p>"}, `@etag = auto

### Inline
GET /inline

hello {{$query.name}}

### File
GET /page

< page.html

### Own
GET /own
ETag: "v1"

mine

### Post
POST /post

posted
`)
	get := func(target, ifNoneMatch string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		if ifNoneMatch != "" {
			req.Header.Set("If-None-Match", ifNoneMatch)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return rec
	}
	for _, target := range []string{"/inline?name=a", "/page"} {
		first := get(target, "")
		etag := first.Header().Get("ETag")
		if first.Code != 200 || !regexp.MustCompile(`^"[0-9a-f]{20}"$`).MatchString(etag) {
			t.Fatalf("GET %s = %d, ETag %q", target, first.Code, etag)
		}
		for _, header := range []string{etag, `"x", ` + etag, "W/" + etag, "*"} {
			if again := get(target, header); again.Code != 304 || again.Body.Len() != 0 {
				t.Errorf("GET %s If-None-Match %s = %d %q", target, header, again.Code, again.Body.String())
			}
		}
		if other := get(target, `"other"`); other.Code != 200 {
			t.Errorf("GET %s with a stale tag = %d", target, other.Code)
		}
	}
	if a, b := get("/inline?name=a", "").Header().Get("ETag"), get("/inline?name=b", "").Header().Get("ETag"); a == b {
		t.Error("different bodies share an ETag")
	}
	if got := get("/own", `"v1"`); got.Header().Get("ETag") != `"v1"` || got.Code != 200 {
		t.Errorf("own ETag: %d %q", got.Code, got.Header().Get("ETag"))
	}
	req := httptest.NewRequest("POST", "/post", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	etag := rec.Header().Get("ETag")
	req = httptest.NewRequest("POST", "/post", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("POST with If-None-Match = %d, want 200", rec.Code)
	}
}

func TestMissingResponseFiles(t *testing.T) {
	s := newDirServer(t, map[string]string{"orders/1.json": `{"id":1}`}, `### Order
@missing = next
GET /orders/:id

< orders/{{$path.id}}.json

### [404] No order
GET /orders/:id

{"error": "no order {{$path.id}}"}

### Gone
@missing = 410
GET /gone

< gone.json
`)
	if code, body := serveRequest(t, s, "GET", "/orders/1", "", ""); code != 200 || body != `{"id":1}` {
		t.Errorf("GET /orders/1 = %d %s", code, body)
	}
	if code, body := serveRequest(t, s, "GET", "/orders/2", "", ""); code != 404 || body != `{"error": "no order 2"}` {
		t.Errorf("GET /orders/2 = %d %s", code, body)
	}
	if code, _ := serveRequest(t, s, "GET", "/orders/..", "", ""); code != 404 {
		t.Errorf("GET /orders/.. = %d", code)
	}
	if code, _ := serveRequest(t, s, "GET", "/gone", "", ""); code != 410 {
		t.Errorf("GET /gone = %d, want 410", code)
	}
}
