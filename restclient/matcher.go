package restclient

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
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
)

// Matcher is a parsed request matcher value from @header.Name, @query.name or
// @path.name. Raw is the value as written (after variable expansion).
type Matcher struct {
	Raw  string
	Kind MatcherKind
	re   *regexp.Regexp
}

// ParseMatcher parses a matcher value: "*" (present), "!" (absent),
// "~pattern" (a Go regular expression that must match the whole value), or
// any other text (exact).
func ParseMatcher(raw string) (Matcher, error) {
	switch {
	case raw == "*":
		return Matcher{Raw: raw, Kind: MatchPresent}, nil
	case raw == "!":
		return Matcher{Raw: raw, Kind: MatchAbsent}, nil
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

// Match reports whether values, every value the request carries for the
// header or query parameter (nil when it is missing), satisfy the matcher.
// Exact and regexp matchers accept any one matching value; present and absent
// matchers look at the first value.
func (m Matcher) Match(values []string) bool {
	present := len(values) > 0 && strings.TrimSpace(values[0]) != ""
	switch m.Kind {
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
