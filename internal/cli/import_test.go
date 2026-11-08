package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sspencer/mock/restclient"
)

const testHAR = `{"log":{"version":"1.2","entries":[
 {"request":{"method":"GET","url":"https://api.example.com/users?page=1","headers":[{"name":":authority","value":"api.example.com"},{"name":"Authorization","value":"Bearer t"}]},
  "response":{"status":200,"headers":[{"name":"content-type","value":"application/json"}],"content":{"size":9,"mimeType":"application/json","text":"[{\"id\":1}]"}}},
 {"request":{"method":"OPTIONS","url":"https://api.example.com/users","headers":[{"name":"Access-Control-Request-Method","value":"POST"}]},
  "response":{"status":204,"headers":[],"content":{"size":0}}},
 {"request":{"method":"POST","url":"https://api.example.com/users","headers":[],"postData":{"mimeType":"application/json","text":"{\"name\":\"a\"}"}},
  "response":{"status":201,"headers":[],"content":{"size":12,"mimeType":"application/json","text":"eyJpZCI6Mn0=","encoding":"base64"}}},
 {"request":{"method":"GET","url":"https://cdn.example.com/logo.png","headers":[]},
  "response":{"status":200,"headers":[],"content":{"size":4,"mimeType":"image/png","text":"iVBORw==","encoding":"base64"}}},
 {"request":{"method":"GET","url":"https://api.example.com/big","headers":[]},
  "response":{"status":200,"headers":[],"content":{"size":5000,"mimeType":"application/json"}}}
]}}`

func runImportTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.har")
	if err := os.WriteFile(path, []byte(testHAR), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), append(append([]string{"import"}, args...), path), nil, &stdout, &stderr, nil)
	return stdout.String(), stderr.String(), err
}

func TestImportHAR(t *testing.T) {
	out, errOut, err := runImportTest(t, "-host", "api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := "### [200] GET /users\n@query.page = *\n@header.Authorization = *\nGET /users\nContent-Type: application/json\n\n[{\"id\":1}]\n" +
		"\n### [201] POST /users\n@json.name = *\nPOST /users\nContent-Type: application/json\n\n{\"id\":2}\n" +
		"\n### [200] GET /big\nGET /big\nContent-Type: application/json\n\n{}\n"
	if out != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	if errOut != "imported 3 route(s) from 3 request(s), skipped 2\n" {
		t.Fatalf("stderr %q", errOut)
	}
	if _, err := restclient.Parse("import.http", strings.NewReader(out)); err != nil {
		t.Fatalf("import does not load: %v", err)
	}
}

func TestImportHARBinaryAndPathFilter(t *testing.T) {
	out, _, err := runImportTest(t, "-path", "/logo")
	if err != nil {
		t.Fatal(err)
	}
	if out != "### [200] GET /logo.png\nGET /logo.png\nContent-Type: application/json\n\n{}\n" {
		t.Fatalf("got\n%s", out)
	}
	if _, _, err := runImportTest(t, "-host", "nowhere"); err == nil || ExitCode(err) != 1 {
		t.Fatalf("empty import: %v", err)
	}
}

func TestImportUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"import"}, {"import", "a", "b"}, {"import", "-bogus", "a"}} {
		err := Run(context.Background(), args, nil, &bytes.Buffer{}, &bytes.Buffer{}, nil)
		if ExitCode(err) != 2 {
			t.Errorf("%v: %v", args, err)
		}
	}
	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"import", "-"}, strings.NewReader("not json"), &stdout, &bytes.Buffer{}, nil)
	if ExitCode(err) != 1 {
		t.Errorf("bad stdin: %v", err)
	}
}
