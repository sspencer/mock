package restclient

import (
	"strings"
	"testing"
)

func TestRejectInvalidConstraints(t *testing.T) {
	for _, input := range []string{"### q\nGET /x?a=%ZZ", "### h\nGET /x\nBad Header: yes", "### h\nGET /x\nX-Test: bad\x00value", "### f\n# $file=../secret\nGET /x"} {
		if _, err := Parse("test.http", strings.NewReader(input)); err == nil {
			t.Errorf("accepted invalid input %q", input)
		}
	}
}
func FuzzParse(f *testing.F) {
	f.Add("### route\nGET /x?q=a\n\n{{$name}}")
	f.Fuzz(func(t *testing.T, input string) { Parse("fuzz.http", strings.NewReader(input)) })
}

func TestRejectInvalidDirectives(t *testing.T) {
	for _, directive := range []string{"delay=-1s", "delay=zero"} {
		if _, err := Parse("test.http", strings.NewReader("### route\n# $"+directive+"\nGET /x")); err == nil {
			t.Errorf("accepted %s", directive)
		}
	}
	for _, method := range []string{"GET/103", "GET/0", "GET/oops", "GET/-1", "GET/1000"} {
		if _, err := Parse("test.http", strings.NewReader("### route\n"+method+" /x")); err == nil {
			t.Errorf("accepted %s", method)
		}
	}
}

func TestRejectStatusVariable(t *testing.T) {
	_, err := Parse("test.http", strings.NewReader("### route\n# $status=201\nPOST /x"))
	if err == nil {
		t.Fatal("accepted $status variable")
	}
	for _, want := range []string{"test.http:2:", "$status is no longer supported", "DELETE/204"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want to contain %q", err, want)
		}
	}
}
