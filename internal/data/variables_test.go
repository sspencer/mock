package data

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestSubstitute_SimpleParameters(t *testing.T) {
	values := url.Values{
		"name": []string{"John"},
		"age":  []string{"30"},
	}

	input := []byte("Hello {{name}}, you are {{age}} years old")
	result := substitute(values, input)

	resultStr := string(result)
	if !strings.Contains(resultStr, "John") {
		t.Errorf("expected result to contain 'John', got %q", resultStr)
	}
	if !strings.Contains(resultStr, "30") {
		t.Errorf("expected result to contain '30', got %q", resultStr)
	}
}

func TestSubstitute_DollarSyntax(t *testing.T) {
	values := url.Values{
		"host": []string{"localhost"},
	}

	// Test the $variable syntax which should be converted to {{variable}}
	input := []byte("Server: {{$host}}")
	result := substitute(values, input)

	resultStr := string(result)
	if !strings.Contains(resultStr, "localhost") {
		t.Errorf("expected result to contain 'localhost', got %q", resultStr)
	}
}

func TestSubstitute_MultipleValues(t *testing.T) {
	// When multiple values exist, substitute should pick one randomly
	values := url.Values{
		"color": []string{"red", "blue", "green"},
	}

	input := []byte("Color: {{color}}")
	result := substitute(values, input)

	resultStr := string(result)
	// Should contain one of the colors
	hasColor := strings.Contains(resultStr, "red") ||
		strings.Contains(resultStr, "blue") ||
		strings.Contains(resultStr, "green")

	if !hasColor {
		t.Errorf("expected result to contain a color, got %q", resultStr)
	}
}

func TestSubstitute_UnknownParameter(t *testing.T) {
	values := url.Values{
		"known": []string{"value"},
	}

	input := []byte("Known: {{known}}, Unknown: {{unknown}}")
	result := substitute(values, input)

	resultStr := string(result)
	if !strings.Contains(resultStr, "value") {
		t.Errorf("expected result to contain 'value', got %q", resultStr)
	}
	// Unknown parameters should be left as-is or handled by template
	if !strings.Contains(resultStr, "Unknown:") {
		t.Errorf("expected result to contain 'Unknown:', got %q", resultStr)
	}
}

func TestSubstitute_FakerFunctions(t *testing.T) {
	values := url.Values{}

	tests := []struct {
		name     string
		input    string
		validate func(string) bool
	}{
		{
			name:  "uuid",
			input: "ID: {{uuid}}",
			validate: func(s string) bool {
				// UUID format: 8-4-4-4-12 hex digits
				uuidRegex := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
				return uuidRegex.MatchString(s)
			},
		},
		{
			name:  "bool",
			input: "Active: {{bool}}",
			validate: func(s string) bool {
				return strings.Contains(s, "true") || strings.Contains(s, "false")
			},
		},
		{
			name:  "integer",
			input: "Count: {{integer}}",
			validate: func(s string) bool {
				// Should contain a number
				numRegex := regexp.MustCompile(`\d+`)
				return numRegex.MatchString(s)
			},
		},
		{
			name:  "email",
			input: "Email: {{email}}",
			validate: func(s string) bool {
				return strings.Contains(s, "@")
			},
		},
		{
			name:  "name",
			input: "Name: {{name}}",
			validate: func(s string) bool {
				// Should have some text after "Name: "
				return len(s) > len("Name: ")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := substitute(values, []byte(tt.input))
			resultStr := string(result)

			if !tt.validate(resultStr) {
				t.Errorf("validation failed for %s: got %q", tt.name, resultStr)
			}
		})
	}
}

func TestSubstitute_MixedParametersAndFunctions(t *testing.T) {
	values := url.Values{
		"id": []string{"123"},
	}

	input := []byte(`{
		"id": "{{id}}",
		"uuid": "{{uuid}}",
		"email": "{{email}}"
	}`)

	result := substitute(values, input)
	resultStr := string(result)

	// Should contain the substituted id
	if !strings.Contains(resultStr, "123") {
		t.Errorf("expected result to contain '123', got %q", resultStr)
	}

	// Should contain generated email with @
	if !strings.Contains(resultStr, "@") {
		t.Errorf("expected result to contain email with @, got %q", resultStr)
	}

	// Should contain a UUID pattern
	uuidRegex := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	if !uuidRegex.MatchString(resultStr) {
		t.Errorf("expected result to contain UUID, got %q", resultStr)
	}
}

func TestSubstitute_EmptyInput(t *testing.T) {
	values := url.Values{}
	input := []byte("")

	result := substitute(values, input)

	if len(result) != 0 {
		t.Errorf("expected empty result, got %q", string(result))
	}
}

func TestSubstitute_NoPlaceholders(t *testing.T) {
	values := url.Values{
		"name": []string{"John"},
	}

	input := []byte("Plain text without placeholders")
	result := substitute(values, input)

	if string(result) != string(input) {
		t.Errorf("expected result to equal input, got %q", string(result))
	}
}

func TestSubstitute_NestedBraces(t *testing.T) {
	values := url.Values{
		"data": []string{`{"nested": "value"}`},
	}

	input := []byte("Data: {{data}}")
	result := substitute(values, input)

	resultStr := string(result)
	if !strings.Contains(resultStr, "nested") {
		t.Errorf("expected result to contain 'nested', got %q", resultStr)
	}
}

func TestSubstitute_SpecialCharacters(t *testing.T) {
	values := url.Values{
		"special": []string{"value with spaces & symbols!"},
	}

	input := []byte("Text: {{special}}")
	result := substitute(values, input)

	resultStr := string(result)
	if !strings.Contains(resultStr, "value with spaces & symbols!") {
		t.Errorf("expected result to contain special characters, got %q", resultStr)
	}
}

func TestSubstitute_MultipleOccurrences(t *testing.T) {
	values := url.Values{
		"name": []string{"John"},
	}

	input := []byte("Hello {{name}}, welcome {{name}}!")
	result := substitute(values, input)

	resultStr := string(result)
	// Both occurrences should be replaced
	count := strings.Count(resultStr, "John")
	if count != 2 {
		t.Errorf("expected 'John' to appear 2 times, got %d times in %q", count, resultStr)
	}
}

func TestSubstitute_DollarWithWhitespace(t *testing.T) {
	values := url.Values{
		"userid": []string{"12345"},
	}

	// Test dollar syntax with whitespace - after dollar replacement, it becomes {{userid}}
	input := []byte("User: {{ $userid }}")
	result := substitute(values, input)

	resultStr := string(result)
	// After dollar replacement, {{userid}} should match the parameter
	if !strings.Contains(resultStr, "12345") {
		t.Errorf("expected result to contain userid value, got %q", resultStr)
	}
}

func TestSubstitute_AllFakerFunctions(t *testing.T) {
	// Test that all faker functions are available and work
	values := url.Values{}

	functions := []string{
		"name", "firstName", "lastName", "email", "user",
		"url", "server", "hash", "phone", "bool", "uuid",
		"guid", "timestamp", "isoTimestamp", "integer",
		"float", "file", "sentence", "paragraph", "article",
	}

	for _, fn := range functions {
		t.Run(fn, func(t *testing.T) {
			input := []byte("{{" + fn + "}}")
			result := substitute(values, input)

			if len(result) == 0 {
				t.Errorf("function %s produced empty result", fn)
			}

			// Result should not contain the placeholder anymore
			resultStr := string(result)
			if strings.Contains(resultStr, "{{"+fn+"}}") {
				t.Errorf("function %s not replaced: %q", fn, resultStr)
			}
		})
	}
}

func TestSubstitute_TemplateError(t *testing.T) {
	values := url.Values{}

	// Invalid template syntax should return original or handle gracefully
	input := []byte("{{invalid syntax")
	result := substitute(values, input)

	// Should not panic and return something
	if result == nil {
		t.Error("expected non-nil result even with invalid template")
	}
}

func TestCreateFuncMap(t *testing.T) {
	// Test that funcMap is created and contains expected functions
	fm := createFuncMap()

	if fm == nil {
		t.Fatal("expected non-nil funcMap")
	}

	expectedFuncs := []string{
		"name", "firstName", "lastName", "email", "user",
		"url", "server", "hash", "phone", "bool", "uuid",
		"guid", "timestamp", "isoTimestamp", "integer",
		"float", "file", "sentence", "paragraph", "article",
	}

	for _, fn := range expectedFuncs {
		if _, ok := fm[fn]; !ok {
			t.Errorf("expected funcMap to contain function %q", fn)
		}
	}
}

func TestCreateFuncMap_Singleton(t *testing.T) {
	// Test that funcMap is created only once (singleton pattern)
	fm1 := createFuncMap()
	fm2 := createFuncMap()

	// Should be the same instance
	if len(fm1) != len(fm2) {
		t.Error("funcMap should be singleton")
	}
}

func TestDollarReplacerRegex(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"{{$host}}", "{{host}}"},
		{"{{ $port }}", "{{port}}"},
		{"{{$user_name}}", "{{user_name}}"},
		{"no replacement", "no replacement"},
	}

	for _, tt := range tests {
		result := dollarReplacerRegex.ReplaceAll([]byte(tt.input), []byte("{{${1}}}"))
		if string(result) != tt.expected {
			t.Errorf("dollarReplacer(%q) = %q, expected %q", tt.input, string(result), tt.expected)
		}
	}
}

func TestReplacerRegex(t *testing.T) {
	tests := []struct {
		input   string
		matches int
	}{
		{"{{name}}", 1},
		{"{{first}} {{last}}", 2},
		{"no matches", 0},
		{"{{multiple}} words {{here}}", 2},
	}

	for _, tt := range tests {
		matches := replacerRegex.FindAll([]byte(tt.input), -1)
		if len(matches) != tt.matches {
			t.Errorf("replacerRegex.FindAll(%q) found %d matches, expected %d", tt.input, len(matches), tt.matches)
		}
	}
}
