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
