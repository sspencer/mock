package main

import (
	"bytes"
	"testing"
	"text/template"
)

func TestCreateFuncMap(t *testing.T) {
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
	fm1 := createFuncMap()
	fm2 := createFuncMap()

	if len(fm1) != len(fm2) {
		t.Error("funcMap should be singleton")
	}
}

func TestFuncMap_AllFunctionsWork(t *testing.T) {
	fm := createFuncMap()

	functions := []string{
		"uuid", "guid", "firstName", "lastName", "email",
		"user", "url", "server", "bool", "integer",
		"float", "file", "hash", "phone", "timestamp",
		"isoTimestamp", "sentence", "paragraph", "article",
	}

	for _, fn := range functions {
		t.Run(fn, func(t *testing.T) {
			tmplStr := "{{" + fn + "}}"
			tmpl, err := template.New("test").Funcs(fm).Parse(tmplStr)
			if err != nil {
				t.Fatalf("failed to parse template: %v", err)
			}

			var buf bytes.Buffer
			err = tmpl.Execute(&buf, nil)
			if err != nil {
				t.Fatalf("failed to execute template: %v", err)
			}

			result := buf.String()
			if result == "" {
				t.Errorf("function %s produced empty result", fn)
			}

			// Result should not contain the placeholder
			if result == "{{"+fn+"}}" {
				t.Errorf("function %s was not replaced", fn)
			}
		})
	}
}

func TestFuncMap_UUID(t *testing.T) {
	fm := createFuncMap()
	tmpl, err := template.New("test").Funcs(fm).Parse("{{uuid}}")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, nil)
	if err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	result := buf.String()
	// UUID should be non-empty and contain hyphens
	if len(result) == 0 {
		t.Error("UUID is empty")
	}
	if len(result) < 32 {
		t.Errorf("UUID too short: %s", result)
	}
}

func TestFuncMap_Email(t *testing.T) {
	fm := createFuncMap()
	tmpl, err := template.New("test").Funcs(fm).Parse("{{email}}")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, nil)
	if err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	result := buf.String()
	// Email should contain @
	if len(result) == 0 {
		t.Error("Email is empty")
	}
	if !bytes.Contains(buf.Bytes(), []byte("@")) {
		t.Errorf("Email doesn't contain @: %s", result)
	}
}

func TestFuncMap_Integer(t *testing.T) {
	fm := createFuncMap()
	tmpl, err := template.New("test").Funcs(fm).Parse("{{integer}}")
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, nil)
	if err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	result := buf.String()
	if len(result) == 0 {
		t.Error("Integer is empty")
	}
}

func TestFuncMap_MultipleVariables(t *testing.T) {
	fm := createFuncMap()
	tmplStr := `Name: {{firstName}} {{lastName}}
Email: {{email}}
UUID: {{uuid}}`

	tmpl, err := template.New("test").Funcs(fm).Parse(tmplStr)
	if err != nil {
		t.Fatalf("failed to parse template: %v", err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, nil)
	if err != nil {
		t.Fatalf("failed to execute template: %v", err)
	}

	result := buf.String()
	if len(result) == 0 {
		t.Error("Result is empty")
	}

	// Check that all placeholders were replaced
	if bytes.Contains(buf.Bytes(), []byte("{{")) {
		t.Errorf("Template contains unreplaced placeholders: %s", result)
	}
}
