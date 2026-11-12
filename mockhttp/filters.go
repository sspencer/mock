package mockhttp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// applyFilter transforms value. Its arguments are already resolved; default
// and keep are handled by the caller. A filter that cannot apply, such as
// add on text that is not a number, returns value unchanged.
func applyFilter(filter templateFilter, value string) string {
	args := filter.args
	switch filter.name {
	case "upper":
		return strings.ToUpper(value)
	case "lower":
		return strings.ToLower(value)
	case "trim":
		return strings.TrimSpace(value)
	case "urlencode":
		return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
	case "base64":
		return base64.StdEncoding.EncodeToString([]byte(value))
	case "base64decode":
		return base64Decode(value)
	case "json":
		encoded, _ := json.Marshal(value)
		return string(encoded)
	case "length":
		return strconv.Itoa(valueLength(value))
	case "sha256":
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	case "hmac":
		mac := hmac.New(sha256.New, []byte(args[0]))
		mac.Write([]byte(value))
		return hex.EncodeToString(mac.Sum(nil))
	case "replace":
		return strings.ReplaceAll(value, args[0], args[1])
	case "slice":
		return sliceRunes(value, args)
	case "int":
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return value
		}
		return strconv.FormatFloat(math.Trunc(n), 'f', 0, 64)
	case "round":
		places := 0
		if len(args) > 0 {
			places, _ = strconv.Atoi(args[0])
		}
		places = max(0, min(places, 15))
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return value
		}
		scale := math.Pow(10, float64(places))
		return strconv.FormatFloat(math.Round(n*scale)/scale, 'f', places, 64)
	case "add", "sub", "mul":
		return arithmetic(filter.name, value, args[0])
	case "date":
		return reformatDate(value, args[0])
	default:
		return value
	}
}

// base64Decode decodes standard or URL-safe base64, padded or not.
func base64Decode(value string) string {
	text := strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(text); err == nil {
			return string(decoded)
		}
	}
	return value
}

// valueLength is the number of elements of a JSON array, the number of
// members of a JSON object, or else the number of characters.
func valueLength(value string) int {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var decoded any
		if json.Unmarshal([]byte(trimmed), &decoded) == nil {
			switch decoded := decoded.(type) {
			case []any:
				return len(decoded)
			case map[string]any:
				return len(decoded)
			}
		}
	}
	return utf8.RuneCountInString(value)
}

// sliceRunes returns the characters from args[0] up to args[1] (or the end).
// Negative positions count from the end; positions past either end are
// clamped.
func sliceRunes(value string, args []string) string {
	runes := []rune(value)
	position := func(arg string) int {
		n, err := strconv.Atoi(arg)
		if err != nil {
			return 0
		}
		if n < 0 {
			n += len(runes)
		}
		return max(0, min(n, len(runes)))
	}
	start, end := position(args[0]), len(runes)
	if len(args) > 1 {
		end = position(args[1])
	}
	if end <= start {
		return ""
	}
	return string(runes[start:end])
}

// arithmetic adds, subtracts or multiplies: in whole numbers when both
// operands are whole, else in floating point.
func arithmetic(op, value, arg string) string {
	a, errA := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	b, errB := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if errA == nil && errB == nil {
		switch op {
		case "add":
			return strconv.FormatInt(a+b, 10)
		case "sub":
			return strconv.FormatInt(a-b, 10)
		default:
			return strconv.FormatInt(a*b, 10)
		}
	}
	x, errX := strconv.ParseFloat(strings.TrimSpace(value), 64)
	y, errY := strconv.ParseFloat(strings.TrimSpace(arg), 64)
	if errX != nil || errY != nil {
		return value
	}
	var result float64
	switch op {
	case "add":
		result = x + y
	case "sub":
		result = x - y
	default:
		result = x * y
	}
	return strconv.FormatFloat(result, 'f', -1, 64)
}

// dateLayouts are the named layouts of the date filter.
var dateLayouts = map[string]string{
	"iso8601": time.RFC3339,
	"rfc3339": time.RFC3339,
	"rfc1123": http.TimeFormat,
	"date":    time.DateOnly,
	"time":    time.TimeOnly,
}

// dateInputs are the timestamp forms the date filter reads, besides Unix
// seconds or milliseconds.
var dateInputs = []string{time.RFC3339Nano, http.TimeFormat, time.RFC1123Z, time.RFC1123, "2006-01-02T15:04:05", time.DateTime, time.DateOnly}

// reformatDate reads a timestamp (Unix seconds or milliseconds, RFC 3339,
// RFC 1123, or a plain date) and writes it in layout: unix, unixms, a named
// layout (iso8601, rfc3339, rfc1123, date, time) or a Go time layout.
func reformatDate(value, layout string) string {
	text := strings.TrimSpace(value)
	var t time.Time
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		if n > 1e12 || n < -1e12 {
			t = time.UnixMilli(n).UTC()
		} else {
			t = time.Unix(n, 0).UTC()
		}
	} else {
		parsed := false
		for _, input := range dateInputs {
			if t, err = time.Parse(input, text); err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			return value
		}
	}
	switch layout {
	case "unix":
		return strconv.FormatInt(t.Unix(), 10)
	case "unixms":
		return strconv.FormatInt(t.UnixMilli(), 10)
	case "rfc1123":
		return t.UTC().Format(http.TimeFormat)
	}
	if named, ok := dateLayouts[layout]; ok {
		layout = named
	}
	return t.Format(layout)
}
