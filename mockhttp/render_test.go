package mockhttp

import (
	"github.com/sspencer/mock/restclient"
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
