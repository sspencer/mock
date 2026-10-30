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
	case "randomInt", "timestamp", "datetime", "localDatetime":
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
//
// An offset is an integer, possibly negative, followed by an option: y, Q, M,
// w, d, h, m, s, or ms. Names that are not functions return errNotFunction.
func callFunction(name string, args []string, now time.Time) (string, error) {
	switch name {
	case "randomInt":
		return randomInt(args)
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
	default:
		return "", errNotFunction
	}
}

func randomInt(args []string) (string, error) {
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
	return strconv.Itoa(low + rand.IntN(high-low)), nil
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
