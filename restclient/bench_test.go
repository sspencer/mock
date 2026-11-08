package restclient

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParse(b *testing.B) {
	var source strings.Builder
	source.WriteString("@base = /api\n\n")
	for i := range 3000 {
		fmt.Fprintf(&source, "### Route %d\n@header.X-Tenant = ~^t[0-9]+$\n@json.user.id = %d\nPOST {{base}}/items/%d/:sub?x=1\nContent-Type: application/json\n\n{\"id\": %d, \"name\": \"{{$name}}\"}\n\n", i, i, i, i)
	}
	text := source.String()
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Parse("bench.http", strings.NewReader(text)); err != nil {
			b.Fatal(err)
		}
	}
}
