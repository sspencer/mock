package restclient

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "spacing and blank lines",
			in:   "\n\n@host=localhost   \n\n\n###Users  \n@delay=10ms\n\n\nget   /users   HTTP/1.1\nContent-Type:application/json  \n\n[1]\n\n\n\n###  [204]  Delete\nDELETE /users/:id",
			want: "@host = localhost\n\n### Users\n@delay = 10ms\nGET /users HTTP/1.1\nContent-Type: application/json\n\n[1]\n\n### [204]  Delete\nDELETE /users/:id\n",
		},
		{
			name: "directive order keeps comments with their directive",
			in:   "### Users\n# Lists users.\n@status = 201\n# Only for admins.\n@header.X-Role = admin\n@limit = 5\n# trailing note\nGET /users\n",
			want: "### Users\n# Lists users.\n@limit = 5\n# Only for admins.\n@header.X-Role = admin\n@status = 201\n# trailing note\nGET /users\n",
		},
		{
			name: "groups are sorted separately",
			in:   "### Users\n@delay = 1s\n@query.page = *\n\n@fault = reset\n@json.id = 7\nPOST /users\n",
			want: "### Users\n@query.page = *\n@delay = 1s\n\n@json.id = 7\n@fault = reset\nPOST /users\n",
		},
		{
			name: "crlf and trailing spaces outside bodies",
			in:   "### A \r\nGET /a\r\n\r\n{\"a\": 1}  \r\n",
			want: "### A\nGET /a\n\n{\"a\": 1}  \n",
		},
		{
			name: "request before the first section",
			in:   "@host = http://x\n// first\nget {{host}}/a\n\nbody\n###\nGET /b\n",
			want: "@host = http://x\n\n// first\nGET {{host}}/a\n\nbody\n\n###\nGET /b\n",
		},
		{
			name: "definition-only section keeps its order",
			in:   "### Vars\n@b=2\n@a=1\n\n### Use\nGET /{{a}}{{b}}\n",
			want: "### Vars\n@b = 2\n@a = 1\n\n### Use\nGET /{{a}}{{b}}\n",
		},
		{
			name: "trim off keeps the body byte for byte",
			in:   "### Raw\n@trim = off\nGET /raw\n\nline one  \r\n\r\n\r\n### Next\nGET /next\n",
			want: "### Raw\n@trim = off\nGET /raw\n\nline one  \r\n\r\n\r\n### Next\nGET /next\n",
		},
		{
			name: "external body",
			in:   "### File\nGET /f\n\n< ./users.json\n\n",
			want: "### File\nGET /f\n\n< ./users.json\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Format("test.http", []byte(test.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, test.want)
			}
			again, err := Format("test.http", got)
			if err != nil || string(again) != string(got) {
				t.Fatalf("not idempotent: %q, %v", again, err)
			}
		})
	}
}

func TestFormatRejectsParseErrors(t *testing.T) {
	_, err := Format("bad.http", []byte("### A\n@delay = soon\nGET /a\n"))
	if err == nil || !strings.Contains(err.Error(), "bad.http:2") {
		t.Fatalf("err = %v, want a parse error at bad.http:2", err)
	}
}

// TestFormatRepositoryFiles formats every valid request file in the
// repository: the result must parse to the same routes (Format checks that)
// and format to itself.
func TestFormatRepositoryFiles(t *testing.T) {
	count := 0
	for _, root := range []string{"../examples", "../mockhttp/testdata", "../internal/cli/starter"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || filepath.Ext(path) != ".http" {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if _, err := Parse(path, strings.NewReader(string(src))); err != nil {
				return nil // a deliberately broken test file
			}
			count++
			out, err := Format(path, src)
			if err != nil {
				t.Errorf("%s: %v", path, err)
				return nil
			}
			if again, err := Format(path, out); err != nil || string(again) != string(out) {
				t.Errorf("%s: formatting twice gives a different result (%v)", path, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if count == 0 {
		t.Fatal("no request files found")
	}
}
