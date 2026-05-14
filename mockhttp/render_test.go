package mockhttp

import "testing"

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
