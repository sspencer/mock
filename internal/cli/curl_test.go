package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

func TestShellWords(t *testing.T) {
	words, err := shellWords("curl -H 'A: b c' \"x\\\"y\" $'a\\nb' plain\\ word \\\n  next # comment\nsecond")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range words {
		got = append(got, w.text)
	}
	want := []string{"curl", "-H", "A: b c", `x"y`, "a\nb", "plain word", "next", "second"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !words[0].first || words[1].first || !words[len(words)-1].first {
		t.Fatal("command starts are not marked")
	}
	if _, err := shellWords("curl 'open"); err == nil {
		t.Fatal("an unterminated quote should fail")
	}
}

func TestCurlExchange(t *testing.T) {
	tests := []struct {
		args                 []string
		method, target, body string
		header, headerValue  string
	}{
		{[]string{"https://api.example.com/users?page=2"}, "GET", "/users?page=2", "", "", ""},
		{[]string{"-XPUT", "-H", "Content-Type: application/json", "-d", `{"a":1}`, "http://h/x"}, "PUT", "/x", `{"a":1}`, "Content-Type", "application/json"},
		{[]string{"-d", "a=1", "-d", "b=2", "h:8080/form"}, "POST", "/form", "a=1&b=2", "Content-Type", "application/x-www-form-urlencoded"},
		{[]string{"-G", "-d", "q=x", "--data-urlencode", "s=a b", "http://h/search"}, "GET", "/search?q=x&s=a+b", "", "", ""},
		{[]string{"--json", `{"n":1}`, "http://h/j"}, "POST", "/j", `{"n":1}`, "Accept", "application/json"},
		{[]string{"-s", "-o", "/dev/null", "-u", "me:pw", "-I", "--url", "http://h/"}, "HEAD", "/", "", "Authorization", "Basic bWU6cHc="},
	}
	for _, tt := range tests {
		x, err := curlExchange(tt.args)
		if err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if x.Method != tt.method || x.Target != tt.target || x.RequestBody != tt.body {
			t.Errorf("%v: got %s %s %q", tt.args, x.Method, x.Target, x.RequestBody)
		}
		if tt.header != "" && x.RequestHeaders.Get(tt.header) != tt.headerValue {
			t.Errorf("%v: %s = %q, want %q", tt.args, tt.header, x.RequestHeaders.Get(tt.header), tt.headerValue)
		}
	}
	for _, args := range [][]string{{"-s"}, {"-H"}, {"ftp://h/x"}} {
		if _, err := curlExchange(args); err == nil {
			t.Errorf("%v should fail", args)
		}
	}
}

func TestImportCurl(t *testing.T) {
	input := `$ curl -s 'https://api.example.com/users/1' -H 'Authorization: Bearer t'
curl -X POST https://api.example.com/users \
  -H 'Content-Type: application/json' \
  --data-raw '{"name":"Ann"}'
curl https://other.example.com/skip
`
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"import", "-curl", "-host", "api.example.com", "-"}, strings.NewReader(input), &stdout, &stderr, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"GET /users/1\n", "@header.Authorization = *\n", "POST /users\n", "@json.name = *\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("import output lacks %q:\n%s", want, out)
		}
	}
	if stderr.String() != "imported 2 route(s) from 2 request(s), skipped 1\n" {
		t.Fatalf("stderr %q", stderr.String())
	}
	if _, err := restclient.Parse("import.http", strings.NewReader(out)); err != nil {
		t.Fatalf("import does not load: %v", err)
	}
}
