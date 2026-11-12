package mockhttp

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// errNotFunction reports that a $name placeholder is not one of the functions
// that take arguments.
var errNotFunction = errors.New("not a function")

// isFunction reports whether name is a $name placeholder that accepts arguments.
func isFunction(name string) bool {
	switch name {
	case "randomInt", "timestamp", "datetime", "localDatetime", "oneOf", "randomFloat", "randomDate":
		return true
	default:
		return false
	}
}

// callFunction evaluates the REST Client style generators:
//
//	{{$randomInt min max}}                      min inclusive, max exclusive
//	{{$timestamp [offset option]}}              Unix seconds, UTC
//	{{$datetime rfc1123|iso8601 [offset option]}}       UTC
//	{{$localDatetime rfc1123|iso8601 [offset option]}}  local time zone
//	{{$oneOf a b c}}                            one of the arguments
//	{{$randomFloat min max [decimals]}}         default 2 decimals
//	{{$randomDate from to}}                     dates or RFC 3339 times
//
// An offset is an integer, possibly negative, followed by an option: y, Q, M,
// w, d, h, m, s, or ms. Names that are not functions return errNotFunction.
// rng is the response's random source; nil uses the global one.
func callFunction(name string, args []string, now time.Time, rng *rand.Rand) (string, error) {
	switch name {
	case "randomInt":
		return randomInt(args, rng)
	case "timestamp":
		t, err := offsetTime(now.UTC(), args)
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(t.Unix(), 10), nil
	case "datetime":
		return formatDatetime(now.UTC(), args, http.TimeFormat, "2006-01-02T15:04:05.000Z")
	case "localDatetime":
		return formatDatetime(now.Local(), args, time.RFC1123Z, "2006-01-02T15:04:05.000-07:00")
	case "oneOf":
		if len(args) == 0 {
			return "", errors.New("expected one or more values, such as {{$oneOf pending paid refunded}}")
		}
		return args[intN(rng, len(args))], nil
	case "randomFloat":
		return randomFloat(args, rng)
	case "randomDate":
		return randomDate(args, rng)
	default:
		return "", errNotFunction
	}
}

// intN draws from rng, or from the global source when rng is nil.
func intN(rng *rand.Rand, n int) int {
	if rng == nil {
		return rand.IntN(n)
	}
	return rng.IntN(n)
}

func float64N(rng *rand.Rand) float64 {
	if rng == nil {
		return rand.Float64()
	}
	return rng.Float64()
}

func randomFloat(args []string, rng *rand.Rand) (string, error) {
	if len(args) != 2 && len(args) != 3 {
		return "", errors.New("expected min, max and optional decimals, such as {{$randomFloat 1 100 2}}")
	}
	low, errLow := strconv.ParseFloat(args[0], 64)
	high, errHigh := strconv.ParseFloat(args[1], 64)
	if errLow != nil || errHigh != nil || high <= low {
		return "", errors.New("min and max must be numbers with max greater than min")
	}
	decimals := 2
	if len(args) == 3 {
		n, err := strconv.Atoi(args[2])
		if err != nil || n < 0 || n > 15 {
			return "", errors.New("decimals must be a whole number from 0 to 15")
		}
		decimals = n
	}
	value := low + float64N(rng)*(high-low)
	return strconv.FormatFloat(value, 'f', decimals, 64), nil
}

// randomDate picks a time between two dates (2006-01-02) or RFC 3339 times,
// inclusive, and writes it in the same form as the arguments: a date when
// both are dates, else an RFC 3339 time in UTC.
func randomDate(args []string, rng *rand.Rand) (string, error) {
	if len(args) != 2 {
		return "", errors.New("expected from and to, such as {{$randomDate 2024-01-01 2024-12-31}}")
	}
	parse := func(text string) (time.Time, bool, error) {
		if t, err := time.Parse(time.DateOnly, text); err == nil {
			return t, true, nil
		}
		t, err := time.Parse(time.RFC3339, text)
		return t, false, err
	}
	from, fromDate, errFrom := parse(args[0])
	to, toDate, errTo := parse(args[1])
	if errFrom != nil || errTo != nil || to.Before(from) {
		return "", errors.New("from and to must be dates (2024-01-31) or RFC 3339 times, with to not before from")
	}
	if fromDate && toDate {
		days := int(to.Sub(from).Hours()/24) + 1
		return from.AddDate(0, 0, intN(rng, days)).Format(time.DateOnly), nil
	}
	span := to.Sub(from)
	offset := time.Duration(float64N(rng) * float64(span))
	return from.Add(offset).UTC().Truncate(time.Second).Format(time.RFC3339), nil
}

func randomInt(args []string, rng *rand.Rand) (string, error) {
	if len(args) != 2 {
		return "", errors.New("expected min and max, such as {{$randomInt 1 100}}")
	}
	low, errLow := strconv.Atoi(args[0])
	high, errHigh := strconv.Atoi(args[1])
	if errLow != nil || errHigh != nil {
		return "", errors.New("min and max must be integers")
	}
	if high <= low || high-low <= 0 {
		return "", errors.New("max must be greater than min")
	}
	if rng == nil {
		return strconv.Itoa(low + rand.IntN(high-low)), nil
	}
	return strconv.Itoa(low + rng.IntN(high-low)), nil
}

func formatDatetime(t time.Time, args []string, rfc1123, iso8601 string) (string, error) {
	if len(args) == 0 {
		return "", errors.New("expected rfc1123 or iso8601")
	}
	layout := ""
	switch strings.ToLower(args[0]) {
	case "rfc1123":
		layout = rfc1123
	case "iso8601":
		layout = iso8601
	default:
		return "", fmt.Errorf("unknown format %q: use rfc1123 or iso8601", args[0])
	}
	t, err := offsetTime(t, args[1:])
	if err != nil {
		return "", err
	}
	return t.Format(layout), nil
}

// offsetTime applies an optional "offset option" pair, such as "-1 d".
func offsetTime(t time.Time, args []string) (time.Time, error) {
	if len(args) == 0 {
		return t, nil
	}
	if len(args) != 2 {
		return t, errors.New("expected an offset and an option, such as -1 d")
	}
	n, err := strconv.Atoi(args[0])
	if err != nil {
		return t, fmt.Errorf("offset %q must be an integer", args[0])
	}
	switch args[1] {
	case "y":
		return t.AddDate(n, 0, 0), nil
	case "Q":
		return t.AddDate(0, 3*n, 0), nil
	case "M":
		return t.AddDate(0, n, 0), nil
	case "w":
		return t.AddDate(0, 0, 7*n), nil
	case "d":
		return t.AddDate(0, 0, n), nil
	case "h":
		return t.Add(time.Duration(n) * time.Hour), nil
	case "m":
		return t.Add(time.Duration(n) * time.Minute), nil
	case "s":
		return t.Add(time.Duration(n) * time.Second), nil
	case "ms":
		return t.Add(time.Duration(n) * time.Millisecond), nil
	default:
		return t, fmt.Errorf("unknown offset option %q: use y, Q, M, w, d, h, m, s, or ms", args[1])
	}
}
