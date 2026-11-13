package restclient

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// MatcherKind says how a request matcher compares values.
type MatcherKind int

const (
	// MatchExact requires a value equal to the matcher text.
	MatchExact MatcherKind = iota
	// MatchPresent ("*") requires a present, non-blank value.
	MatchPresent
	// MatchAbsent ("!") requires the value to be missing or blank: the
	// complement of MatchPresent.
	MatchAbsent
	// MatchRegexp ("~pattern") requires a value matched in full by pattern.
	MatchRegexp
	// MatchNotExact ("!text") requires a present, non-blank value, none of
	// whose values equals text.
	MatchNotExact
	// MatchNotRegexp ("!~pattern") requires a present, non-blank value, none
	// of whose values pattern matches in full.
	MatchNotRegexp
	// MatchCompare (">100", ">=1.5", "<0", "<=50") requires a value that is a
	// number and compares that way.
	MatchCompare
	// MatchLength ("#>0", "#3", "#<=10") compares the length of a value: the
	// elements of a JSON array, the members of a JSON object, or otherwise
	// the characters of its text.
	MatchLength
)

// Matcher is a parsed request matcher value from @header.Name, @query.name or
// @path.name. Raw is the value as written (after variable expansion).
type Matcher struct {
	Raw  string
	Kind MatcherKind
	re   *regexp.Regexp
	// Op and Operand are a comparison's operator (">", ">=", "<", "<=" or
	// "=") and number, for MatchCompare and MatchLength.
	Op      string
	Operand float64
}

// compareMatcher and lengthMatcher recognize ">100" and "#>0" values;
// anything else starting with ">", "<" or "#" is an exact matcher.
var (
	compareMatcher = regexp.MustCompile(`^(>=|<=|>|<)\s*(` + numberText + `)$`)
	lengthMatcher  = regexp.MustCompile(`^#\s*(>=|<=|>|<|=)?\s*([0-9]+)$`)
	numberPattern  = regexp.MustCompile(`^` + numberText + `$`)
)

// numberText is a decimal number such as -3, 0.25 or 1e3.
const numberText = `-?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`

// ParseMatcher parses a matcher value: "*" (present), "!" (absent),
// "~pattern" (a Go regular expression that must match the whole value),
// "!text" or "!~pattern" (present, but not that value or pattern), ">100",
// ">=", "<" or "<=" (a number compared that way), "#>0" and the like (a
// length compared that way; "#3" is exactly 3), or any other text (exact).
func ParseMatcher(raw string) (Matcher, error) {
	if m := compareMatcher.FindStringSubmatch(raw); m != nil {
		operand, err := strconv.ParseFloat(m[2], 64)
		if err == nil && !math.IsInf(operand, 0) {
			return Matcher{Raw: raw, Kind: MatchCompare, Op: m[1], Operand: operand}, nil
		}
	}
	if m := lengthMatcher.FindStringSubmatch(raw); m != nil {
		operand, err := strconv.ParseFloat(m[2], 64)
		if err == nil && !math.IsInf(operand, 0) {
			return Matcher{Raw: raw, Kind: MatchLength, Op: cmp.Or(m[1], "="), Operand: operand}, nil
		}
	}
	switch {
	case raw == "*":
		return Matcher{Raw: raw, Kind: MatchPresent}, nil
	case raw == "!":
		return Matcher{Raw: raw, Kind: MatchAbsent}, nil
	case strings.HasPrefix(raw, "!~"):
		re, err := compileMatcherRegexp(raw[2:])
		if err != nil {
			return Matcher{}, err
		}
		return Matcher{Raw: raw, Kind: MatchNotRegexp, re: re}, nil
	case strings.HasPrefix(raw, "!"):
		return Matcher{Raw: raw, Kind: MatchNotExact}, nil
	case strings.HasPrefix(raw, "~"):
		re, err := compileMatcherRegexp(raw[1:])
		if err != nil {
			return Matcher{}, err
		}
		return Matcher{Raw: raw, Kind: MatchRegexp, re: re}, nil
	}
	return Matcher{Raw: raw, Kind: MatchExact}, nil
}

// ParseBodyMatcher parses an @body matcher. It is ParseMatcher, except that "."
// in a regular expression also matches newlines, so ~.*alice.* finds alice
// anywhere in a multi-line body.
func ParseBodyMatcher(raw string) (Matcher, error) {
	m, err := ParseMatcher(bodyMatcherSource(raw))
	m.Raw = raw
	return m, err
}

// MustBodyMatcher is MustMatcher for @body values.
func MustBodyMatcher(raw string) Matcher {
	m := MustMatcher(bodyMatcherSource(raw))
	m.Raw = raw
	return m
}

func bodyMatcherSource(raw string) string {
	if pattern, ok := strings.CutPrefix(raw, "!~"); ok && pattern != "" {
		return "!~(?s)" + pattern
	}
	if pattern, ok := strings.CutPrefix(raw, "~"); ok && pattern != "" {
		return "~(?s)" + pattern
	}
	return raw
}

// MustMatcher parses raw, treating an invalid regular expression as a matcher
// that never matches. Load-time validation reports such patterns; this keeps
// hand-built routes from panicking at request time.
func MustMatcher(raw string) Matcher {
	m, err := ParseMatcher(raw)
	if err != nil {
		return Matcher{Raw: raw, Kind: MatchRegexp}
	}
	return m
}

// Negated reports whether the matcher is a "!text" or "!~pattern" matcher.
func (m Matcher) Negated() bool {
	return m.Kind == MatchNotExact || m.Kind == MatchNotRegexp
}

// Pattern returns the regular expression of a "~pattern" or "!~pattern"
// matcher as written, or "" for other matchers.
func (m Matcher) Pattern() string {
	switch m.Kind {
	case MatchRegexp:
		return strings.TrimPrefix(m.Raw, "~")
	case MatchNotRegexp:
		return strings.TrimPrefix(m.Raw, "!~")
	}
	return ""
}

// negatedMatch is Match for "!text" and "!~pattern": some value is non-blank
// and no value equals text or matches pattern.
func (m Matcher) negatedMatch(values []string) bool {
	if !slices.ContainsFunc(values, func(value string) bool { return strings.TrimSpace(value) != "" }) {
		return false
	}
	text := m.Raw[1:]
	for _, value := range values {
		if m.Kind == MatchNotRegexp {
			if m.re == nil || m.re.MatchString(value) {
				return false
			}
		} else if value == text {
			return false
		}
	}
	return true
}

// Match reports whether values, every value the request carries for the
// header or query parameter (nil when it is missing), satisfy the matcher.
// Exact and regexp matchers accept any one matching value; present and absent
// matchers look at the first value. Negated matchers ("!text", "!~pattern")
// need a non-blank value and reject the request if any value is excluded.
func (m Matcher) Match(values []string) bool {
	if m.Negated() {
		return m.negatedMatch(values)
	}
	present := len(values) > 0 && strings.TrimSpace(values[0]) != ""
	switch m.Kind {
	case MatchCompare:
		return slices.ContainsFunc(values, func(value string) bool {
			n, ok := ParseNumber(value)
			return ok && m.compare(n)
		})
	case MatchLength:
		return slices.ContainsFunc(values, func(value string) bool { return m.compare(float64(TextLength(value))) })
	case MatchPresent:
		return present
	case MatchAbsent:
		return !present
	case MatchRegexp:
		if m.re == nil {
			return false
		}
		for _, value := range values {
			if m.re.MatchString(value) {
				return true
			}
		}
		return false
	}
	for _, value := range values {
		if value == m.Raw {
			return true
		}
	}
	return false
}

// AllValuesSuffix ends a @query.name[] or @header.Name[] matcher name: the
// matcher must accept every value of a repeated field, not just one.
const AllValuesSuffix = "[]"

// AllValues splits a matcher key into its field name and whether it is an
// all-values matcher (name[]).
func AllValues(key string) (string, bool) {
	return strings.CutSuffix(key, AllValuesSuffix)
}

// MatchAll is Match for an all-values matcher (@query.name[]): every value
// must satisfy it on its own, and there must be at least one. A length
// matcher ("#2", "#>0") counts the values instead, and "!" accepts a field
// with no non-blank value.
func (m Matcher) MatchAll(values []string) bool {
	switch m.Kind {
	case MatchLength:
		return m.compare(float64(len(values)))
	case MatchAbsent:
		return m.Match(values) && !slices.ContainsFunc(values, func(v string) bool { return strings.TrimSpace(v) != "" })
	}
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !m.Match([]string{value}) {
			return false
		}
	}
	return true
}

// MatchLengths is Match for a MatchLength matcher given the lengths of the
// values directly, such as JSON array sizes: any one may satisfy it.
func (m Matcher) MatchLengths(lengths []int) bool {
	return m.Kind == MatchLength && slices.ContainsFunc(lengths, func(n int) bool { return m.compare(float64(n)) })
}

func (m Matcher) compare(n float64) bool {
	switch m.Op {
	case ">":
		return n > m.Operand
	case ">=":
		return n >= m.Operand
	case "<":
		return n < m.Operand
	case "<=":
		return n <= m.Operand
	}
	return n == m.Operand
}

// ParseNumber parses a decimal number such as "42", "-0.5" or "1e3",
// ignoring surrounding spaces. Hex, infinities and NaN are not numbers.
func ParseNumber(text string) (float64, bool) {
	text = strings.TrimSpace(text)
	if !numberPattern.MatchString(text) {
		return 0, false
	}
	n, err := strconv.ParseFloat(text, 64)
	return n, err == nil && !math.IsInf(n, 0)
}

// TextLength is the length MatchLength compares for a value given as text:
// the elements of a JSON array or members of a JSON object, else the
// characters of the text.
func TextLength(text string) int {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var node any
		if json.Unmarshal([]byte(trimmed), &node) == nil {
			switch node := node.(type) {
			case []any:
				return len(node)
			case map[string]any:
				return len(node)
			}
		}
	}
	return utf8.RuneCountInString(text)
}

// Describe is a short phrase for a comparison or length matcher, such as
// "a number > 100" or "a length of at least 1", and "" for other matchers.
func (m Matcher) Describe() string {
	words := map[string]string{">": "above", ">=": "at least", "<": "below", "<=": "at most", "=": "exactly"}
	operand := strconv.FormatFloat(m.Operand, 'f', -1, 64)
	switch m.Kind {
	case MatchCompare:
		return "a number " + words[m.Op] + " " + operand
	case MatchLength:
		words[">"], words["<"] = "more than", "less than"
		return "a length of " + words[m.Op] + " " + operand
	}
	return ""
}

// MatchAny is Match for a set of values that are alternatives, such as every
// element a @json path with a "*" segment selects: present and absent
// matchers look at all of them, so "*" accepts any non-blank value and "!"
// requires that there is none. Exact and regexp matchers behave as in Match.
func (m Matcher) MatchAny(values []string) bool {
	switch m.Kind {
	case MatchPresent, MatchAbsent:
		present := slices.ContainsFunc(values, func(value string) bool { return strings.TrimSpace(value) != "" })
		return present == (m.Kind == MatchPresent)
	}
	return m.Match(values)
}

// Captures returns the named groups, such as id in ~(?P<id>[0-9]+), of the
// first value a regexp matcher matches. It returns nil for other matchers,
// for patterns without named groups, and when nothing matches.
func (m Matcher) Captures(values []string) map[string]string {
	if m.Kind != MatchRegexp || m.re == nil || !slices.ContainsFunc(m.re.SubexpNames(), func(name string) bool { return name != "" }) {
		return nil
	}
	for _, value := range values {
		groups := m.re.FindStringSubmatch(value)
		if groups == nil {
			continue
		}
		captures := make(map[string]string)
		for i, name := range m.re.SubexpNames() {
			if name != "" {
				captures[name] = groups[i]
			}
		}
		return captures
	}
	return nil
}

// CaptureNames returns the named groups of a regexp matcher value, or nil.
func CaptureNames(raw string) []string {
	m := MustMatcher(raw)
	if m.re == nil {
		return nil
	}
	var names []string
	for _, name := range m.re.SubexpNames() {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

var matcherRegexps sync.Map // pattern -> *regexp.Regexp

// compileMatcherRegexp anchors pattern so it must match the whole value, and
// caches the result so routes rebuilt per request do not recompile it.
func compileMatcherRegexp(pattern string) (*regexp.Regexp, error) {
	if cached, ok := matcherRegexps.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	if pattern == "" {
		return nil, fmt.Errorf("regular expression after ~ is empty")
	}
	re, err := regexp.Compile(`^(?:` + pattern + `)$`)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression %q: %v", pattern, err)
	}
	matcherRegexps.Store(pattern, re)
	return re, nil
}
