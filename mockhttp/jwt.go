package mockhttp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// stringClaims are JWT claims that are always strings, even when their value
// looks like a number.
var stringClaims = map[string]bool{"sub": true, "iss": true, "aud": true, "jti": true}

// timeClaims are JWT claims holding a Unix time; a duration such as "1h" or
// "-5m" is taken relative to now.
var timeClaims = map[string]bool{"exp": true, "nbf": true, "iat": true}

// checkJWTArgs reports whether {{$jwt}} arguments are claim=value pairs.
func checkJWTArgs(args []string) error {
	for _, arg := range args {
		if name, _, ok := strings.Cut(arg, "="); !ok || name == "" {
			return errors.New(`{{$jwt}} arguments must be claim=value pairs, such as {{$jwt sub=$json.user exp=1h}}`)
		}
	}
	return nil
}

// jwt builds a JSON Web Token from claim=value arguments, such as
// {{$jwt sub=$json.user role=admin exp=1h}}. iat is set to now unless given.
// With @jwt.secret the token is signed with HS256; without it, it is
// unsigned (alg "none"). A claim whose $reference has no value is left out.
func (r *renderer) jwt(args []string, scope *renderScope) (string, bool) {
	if checkJWTArgs(args) != nil {
		return "", false
	}
	now := r.now
	if r.seeded {
		now = seededReferenceTime
	}
	claims := map[string]any{"iat": now.Unix()}
	for _, arg := range args {
		name, raw, _ := strings.Cut(arg, "=")
		value, ok := r.argValue(raw, scope)
		if !ok {
			continue
		}
		claims[name] = claimValue(name, value, now)
	}
	secret := ""
	if raw, ok := r.method.Variables["jwt.secret"]; ok {
		rendered, err := r.text(raw, false)
		if err != nil {
			return "", false
		}
		secret = rendered
	}
	alg := "none"
	if secret != "" {
		alg = "HS256"
	}
	header, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", false
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	if secret == "" {
		return unsigned + ".", true
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), true
}

// claimValue types a claim: times as Unix seconds (a duration counts from
// now), sub/iss/aud/jti as strings, and other values as JSON numbers,
// booleans, objects or arrays when they read as such, else strings.
func claimValue(name, value string, now time.Time) any {
	if timeClaims[name] {
		if d, err := time.ParseDuration(value); err == nil {
			return now.Add(d).Unix()
		}
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
		return value
	}
	if stringClaims[name] {
		return value
	}
	trimmed := strings.TrimSpace(value)
	if trimmed != "" && json.Valid([]byte(trimmed)) && !strings.HasPrefix(trimmed, `"`) {
		return json.RawMessage(trimmed)
	}
	return value
}
