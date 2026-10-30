package mockhttp

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sspencer/mock/restclient"
)

func TestCallFunction(t *testing.T) {
	now := time.Date(2024, time.March, 15, 10, 30, 45, 123_000_000, time.UTC)
	tests := []struct {
		name string
		args string
		want string
	}{
		{"timestamp", "", "1710498645"},
		{"timestamp", "-1 d", "1710412245"},
		{"timestamp", "1 h", "1710502245"},
		{"timestamp", "30 m", "1710500445"},
		{"timestamp", "-45 s", "1710498600"},
		{"timestamp", "1000 ms", "1710498646"},
		{"timestamp", "1 w", "1711103445"},
		{"timestamp", "1 M", "1713177045"},
		{"timestamp", "1 Q", "1718447445"},
		{"timestamp", "-1 y", "1678876245"},
		{"datetime", "rfc1123", "Fri, 15 Mar 2024 10:30:45 GMT"},
		{"datetime", "RFC1123 1 d", "Sat, 16 Mar 2024 10:30:45 GMT"},
		{"datetime", "iso8601", "2024-03-15T10:30:45.123Z"},
		{"datetime", "iso8601 -2 h", "2024-03-15T08:30:45.123Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" "+tt.args, func(t *testing.T) {
			got, err := callFunction(tt.name, strings.Fields(tt.args), now)
			if err != nil || got != tt.want {
				t.Fatalf("callFunction(%q, %q) = %q, %v; want %q", tt.name, tt.args, got, err, tt.want)
			}
		})
	}
}

func TestCallFunctionLocalDatetime(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct{ format, layout string }{
		{"rfc1123", time.RFC1123Z},
		{"iso8601", "2006-01-02T15:04:05.000-07:00"},
	} {
		got, err := callFunction("localDatetime", []string{tt.format}, now)
		if err != nil {
			t.Fatalf("localDatetime %s: %v", tt.format, err)
		}
		parsed, err := time.Parse(tt.layout, got)
		if err != nil {
			t.Fatalf("localDatetime %s = %q does not parse as %q: %v", tt.format, got, tt.layout, err)
		}
		if _, offset := parsed.Zone(); func() int { _, o := now.Zone(); return o }() != offset {
			t.Fatalf("localDatetime %s = %q, want the local zone offset", tt.format, got)
		}
	}
}

func TestCallFunctionRandomInt(t *testing.T) {
	seen := map[int]bool{}
	for range 200 {
		got, err := callFunction("randomInt", []string{"5", "8"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		n, _ := strconv.Atoi(got)
		if n < 5 || n >= 8 {
			t.Fatalf("randomInt 5 8 = %d, want 5 <= n < 8", n)
		}
		seen[n] = true
	}
	if len(seen) != 3 {
		t.Fatalf("randomInt 5 8 produced %v, want all of 5, 6, 7", seen)
	}
	if got, err := callFunction("randomInt", []string{"-3", "-2"}, time.Now()); err != nil || got != "-3" {
		t.Fatalf("randomInt -3 -2 = %q, %v; want -3", got, err)
	}
}

func TestCallFunctionErrors(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct{ name, args string }{
		{"randomInt", ""},
		{"randomInt", "1"},
		{"randomInt", "1 2 3"},
		{"randomInt", "a 5"},
		{"randomInt", "5 5"},
		{"randomInt", "9 2"},
		{"timestamp", "1"},
		{"timestamp", "x d"},
		{"timestamp", "1 fortnight"},
		{"timestamp", "1 d extra"},
		{"datetime", ""},
		{"datetime", "unix"},
		{"datetime", "iso8601 1"},
		{"localDatetime", "rfc1123 1 nope"},
	} {
		if got, err := callFunction(tt.name, strings.Fields(tt.args), now); err == nil {
			t.Errorf("callFunction(%q, %q) = %q, want error", tt.name, tt.args, got)
		}
	}
	if _, err := callFunction("name", nil, now); !errors.Is(err, errNotFunction) {
		t.Fatalf("callFunction(name) error = %v, want errNotFunction", err)
	}
}

func TestPlaceholdersWithFunctionArguments(t *testing.T) {
	method := restclient.Method{
		Headers:   map[string][]string{"Content-Type": {"application/json"}},
		Variables: map[string]string{"n": "7"},
	}
	// "when" is a request parameter: with no arguments it wins, with arguments the function runs.
	values := map[string]string{"datetime": "from-request"}
	got := expandBodyPlaceholders(`{"r":{{$randomInt 3 4}},"t":{{$timestamp 0 s}},"p":"{{$datetime}}","d":"{{$datetime iso8601}}","bad":"{{$randomInt x}}","n":{{n}}}`, method, values)
	pattern := regexp.MustCompile(`^\{"r":3,"t":\d+,"p":"from-request","d":"\d{4}-\d\d-\d\dT[\d:.]+Z","bad":"\{\{\$randomInt x\}\}","n":7\}$`)
	if !pattern.MatchString(got) {
		t.Fatalf("expanded body = %s", got)
	}
}
