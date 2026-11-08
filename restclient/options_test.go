package restclient

import (
	"strings"
	"testing"
)

func TestOptionsVarsOverrideDefinitions(t *testing.T) {
	source := `@host = http://localhost:9000
@token = file

### Users
GET {{host}}/users
X-Token: {{token}}

ok

### Admin
@token = section
GET {{host}}/admin
X-Token: {{token}} {{extra}}

ok
`
	methods, err := Options{Vars: map[string]string{"host": "http://localhost:9100", "token": "cli", "extra": "more"}}.Parse("vars.http", strings.NewReader(source))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	for _, method := range methods {
		if method.Port != 9100 {
			t.Errorf("%s: port = %d, want 9100 from -var host", method.Name, method.Port)
		}
	}
	if got := methods[0].Variables["token"]; got != "cli" {
		t.Errorf("file variable override: token = %q, want cli", got)
	}
	if got := methods[1].Variables["token"]; got != "cli" {
		t.Errorf("section variable override: token = %q, want cli", got)
	}
	if got := methods[1].Variables["extra"]; got != "more" {
		t.Errorf("undefined variable: extra = %q, want more", got)
	}
	if line := methods[0].VariableLines["host"]; line != 1 {
		t.Errorf("override keeps the definition line: got %d, want 1", line)
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Errorf("UnusedVariables() = %+v, want none", unused)
	}
}

func TestOptionsVarsNotDefinedInFileAreNotReportedUnused(t *testing.T) {
	methods, err := Options{Vars: map[string]string{"elsewhere": "x"}}.Parse("vars.http", strings.NewReader("### A\nGET /a\n\nok\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Errorf("UnusedVariables() = %+v, want none", unused)
	}
}

func TestOptionsVarsSetControlVariables(t *testing.T) {
	methods, err := Options{Vars: map[string]string{"delay": "0s"}}.Parse("vars.http", strings.NewReader("### A\n@delay = 2s\nGET /a\n\nok\n"))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, _ := methods[0].Variable("delay"); got != "0s" {
		t.Errorf("delay = %q, want 0s", got)
	}
}

func TestCheckVariableOverride(t *testing.T) {
	for _, name := range []string{"host", "api.token", "_x-1", "delay"} {
		if err := CheckVariableOverride(name); err != nil {
			t.Errorf("CheckVariableOverride(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "1x", "a b", "header.Authorization", "query.page", "body", "json.a", "form.a", "path.id"} {
		if err := CheckVariableOverride(name); err == nil {
			t.Errorf("CheckVariableOverride(%q) = nil, want error", name)
		}
	}
}

func TestOptionsDefaultsFillUndefinedVariables(t *testing.T) {
	source := "@host = http://localhost:9000\n\n### A\nGET {{host}}/a\nX-Token: {{token}}\n\nok\n"
	methods, err := Options{
		Defaults: map[string]string{"host": "http://localhost:9500", "token": "env", "port": "9999", "query.x": "1"},
		Vars:     map[string]string{"token": "cli"},
	}.Parse("vars.http", strings.NewReader(source))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if methods[0].Port != 9000 {
		t.Errorf("port = %d, want 9000: an @name definition beats a default and @port is never defaulted", methods[0].Port)
	}
	if got := methods[0].Variables["token"]; got != "cli" {
		t.Errorf("token = %q, want cli from Vars", got)
	}
	methods, err = Options{Defaults: map[string]string{"token": "env", "unused": "x"}}.Parse("vars.http", strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if got := methods[0].Variables["token"]; got != "env" {
		t.Errorf("token = %q, want env from Defaults", got)
	}
	if unused := UnusedVariables(methods); len(unused) != 0 {
		t.Errorf("UnusedVariables() = %+v, want none", unused)
	}
}

func TestIsDefaultVariable(t *testing.T) {
	for name, want := range map[string]bool{"baseUrl": true, "api-key": true, "port": false, "delay": false, "header.X": false, "callback.url": false, "1x": false} {
		if got := IsDefaultVariable(name); got != want {
			t.Errorf("IsDefaultVariable(%q) = %v, want %v", name, got, want)
		}
	}
}
