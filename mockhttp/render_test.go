package mockhttp

import (
	"encoding/json"
	"github.com/sspencer/mock/restclient"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedValueSupportsDocumentedKeys(t *testing.T) {
	keys := []string{
		"name",
		"firstName",
		"lastName",
		"user",
		"email",
		"phone",
		"url",
		"server",
		"hash",
		"bool",
		"integer",
		"float",
		"uuid",
		"guid",
		"timestamp",
		"isoTimestamp",
		"file",
		"sentence",
		"paragraph",
		"article",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			if got := generatedValue(key); got == "" {
				t.Fatalf("generatedValue(%q) = empty string, want value", key)
			}
		})
	}
}

func TestStatusAllowsBody(t *testing.T) {
	if statusAllowsBody(http.StatusNoContent) || statusAllowsBody(http.StatusNotModified) {
		t.Fatal("204 and 304 must not allow a body")
	}
	if !statusAllowsBody(http.StatusOK) || !statusAllowsBody(http.StatusCreated) || !statusAllowsBody(http.StatusBadRequest) {
		t.Fatal("ordinary final statuses should allow a body")
	}
}

func TestGeneratedValueReturnsEmptyForUnknownKey(t *testing.T) {
	if got := generatedValue("missing"); got != "" {
		t.Fatalf("generatedValue(missing) = %q, want empty string", got)
	}
}

func TestTextClassificationChecksEntireBody(t *testing.T) {
	body := append([]byte(strings.Repeat("a", 1024)), 0)
	if isMostlyText(body) {
		t.Fatal("binary suffix classified as text")
	}
}
func TestPlaceholderGrammarAndUnknownPreservation(t *testing.T) {
	method := restclient.Method{Variables: map[string]string{"user-name": "Alice", "user.name": "Bob"}}
	if got := expandPlaceholders("{{$user-name}} {{$user.name}} {{$typo}}", method, nil); got != "Alice Bob {{$typo}}" {
		t.Fatal(got)
	}
}

func TestJSONInterpolationEscapesUntrustedValues(t *testing.T) {
	method := restclient.Method{Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: `{"value":"{{$id}}","number":{{$number}}}`}
	body, err := renderBody(method, map[string]string{"id": "quote\"\n\\slash", "number": "42"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("invalid JSON: %s (%v)", body, err)
	}
	if decoded["value"] != "quote\"\n\\slash" || decoded["number"] != float64(42) {
		t.Fatal(decoded)
	}
}
func TestResponseFileCannotEscapeThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "fixtures"), 0700)
	os.WriteFile(filepath.Join(dir, "secret"), []byte("secret"), 0600)
	if err := os.Symlink("../secret", filepath.Join(dir, "fixtures", "link")); err != nil {
		t.Skip(err)
	}
	method := restclient.Method{Source: filepath.Join(dir, "fixtures", "test.http"), Variables: map[string]string{"file": "link"}}
	path, ok := resolveFilePath(&method)
	if !ok {
		t.Fatal("expected lexical path to resolve")
	}
	if _, err := renderBody(method, nil, path, true); err == nil {
		t.Fatal("served file outside fixture root")
	}
}

func TestBinaryMIMEPreventsTextLikeFileExpansion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.bin")
	want := "printable binary {{$name}}"
	os.WriteFile(path, []byte(want), 0600)
	method := restclient.Method{Source: filepath.Join(dir, "test.http"), Variables: map[string]string{"file": "body.bin"}}
	body, err := renderBody(method, nil, path, true)
	if err != nil || string(body) != want {
		t.Fatalf("binary changed: %q, %v", body, err)
	}
}
