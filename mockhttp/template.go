package mockhttp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// A response template is text with placeholders ({{name}}, {{%name}},
// {{$name args | filter ...}}) and repeat blocks ({{#repeat n}} ...
// {{/repeat}}). It is parsed into nodes once per render.

const (
	// maxRepeat caps one {{#repeat}} block's count.
	maxRepeat = 1000
	// maxRenderedBytes caps a rendered body, so nested repeats cannot run
	// away with memory.
	maxRenderedBytes = 16 << 20
)

var errRenderedTooLarge = fmt.Errorf("rendered body is larger than %d MiB", maxRenderedBytes>>20)

// templateTagPattern matches repeat tags and placeholders. Submatch 1 is the
// repeat count, when the match is an opening tag; submatches 2-4 are
// restclient.PlaceholderPattern's prefix, name and arguments.
var templateTagPattern = regexp.MustCompile(`\{\{#repeat(?:[ \t]+([^{}]*?))?[ \t]*}}|\{\{/repeat[ \t]*}}|` + restclient.PlaceholderPattern.String())

type nodeKind int

const (
	textNode nodeKind = iota
	placeholderNode
	repeatNode
)

type templateNode struct {
	kind nodeKind
	// text is literal text, or a placeholder as written.
	text string

	prefix  string
	name    string
	args    []string
	filters []templateFilter

	min, max int
	children []templateNode
}

type templateFilter struct {
	name string
	args []string
}

// templateProblem is a template mistake reported as a load warning. The text
// it concerns is kept literally.
type templateProblem struct {
	text    string
	message string
}

// parseTemplate splits input into nodes. It never fails: a malformed repeat
// tag or placeholder stays literal text, and is reported in problems.
func parseTemplate(input string) (nodes []templateNode, problems []templateProblem) {
	type frame struct {
		node   templateNode
		opener string
		parent []templateNode
	}
	var stack []frame
	current := []templateNode(nil)
	appendText := func(text string) {
		if text == "" {
			return
		}
		if n := len(current); n > 0 && current[n-1].kind == textNode {
			current[n-1].text += text
			return
		}
		current = append(current, templateNode{kind: textNode, text: text})
	}

	previous := 0
	for _, loc := range templateTagPattern.FindAllStringSubmatchIndex(input, -1) {
		start, end := loc[0], loc[1]
		tag := input[start:end]
		isOpen, isClose := strings.HasPrefix(tag, "{{#repeat"), strings.HasPrefix(tag, "{{/repeat")
		if isOpen || isClose {
			// A tag alone on its line takes the whole line with it, so block
			// tags do not leave blank lines in the output.
			start, end = standaloneLine(input, start, end, previous)
		}
		appendText(input[previous:start])
		previous = end
		switch {
		case isOpen:
			countText := ""
			if loc[2] >= 0 {
				countText = input[loc[2]:loc[3]]
			}
			low, high, err := parseRepeatCount(countText)
			if err != nil {
				problems = append(problems, templateProblem{tag, err.Error()})
				appendText(input[start:end])
				continue
			}
			stack = append(stack, frame{node: templateNode{kind: repeatNode, text: tag, min: low, max: high}, opener: input[start:end], parent: current})
			current = nil
		case isClose:
			if len(stack) == 0 {
				problems = append(problems, templateProblem{tag, "{{/repeat}} has no matching {{#repeat}}"})
				appendText(input[start:end])
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			top.node.children = current
			current = append(top.parent, top.node)
		default:
			node, err := parsePlaceholder(tag, input[loc[4]:loc[5]], input[loc[6]:loc[7]], submatch(input, loc, 8))
			if err != nil {
				problems = append(problems, templateProblem{tag, err.Error()})
			}
			current = append(current, node)
		}
	}
	appendText(input[previous:])
	// Unclosed blocks become literal text around their contents.
	for len(stack) > 0 {
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		problems = append(problems, templateProblem{top.node.text, "{{#repeat}} has no matching {{/repeat}}"})
		children := current
		current = top.parent
		appendText(top.opener)
		for _, child := range children {
			if child.kind == textNode {
				appendText(child.text)
			} else {
				current = append(current, child)
			}
		}
	}
	return current, problems
}

func submatch(input string, loc []int, group int) string {
	if loc[group] < 0 {
		return ""
	}
	return input[loc[group]:loc[group+1]]
}

// standaloneLine widens a block tag's span to its whole line, including the
// line break, when nothing else is on that line. It never reaches back past
// floor, the end of the previous tag.
func standaloneLine(input string, start, end, floor int) (int, int) {
	lineStart := start
	for lineStart > floor && (input[lineStart-1] == ' ' || input[lineStart-1] == '\t') {
		lineStart--
	}
	if lineStart > 0 && input[lineStart-1] != '\n' {
		return start, end
	}
	lineEnd := end
	for lineEnd < len(input) && (input[lineEnd] == ' ' || input[lineEnd] == '\t' || input[lineEnd] == '\r') {
		lineEnd++
	}
	switch {
	case lineEnd == len(input):
		return lineStart, lineEnd
	case input[lineEnd] == '\n':
		return lineStart, lineEnd + 1
	default:
		return start, end
	}
}

// parseRepeatCount parses "n" or "min max" (both inclusive).
func parseRepeatCount(text string) (int, int, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, 0, errors.New(`{{#repeat}} needs a count, such as {{#repeat 3}} or {{#repeat 1 5}}`)
	}
	values := make([]int, len(fields))
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 || n > maxRepeat {
			return 0, 0, fmt.Errorf("{{#repeat}} counts must be whole numbers from 0 to %d", maxRepeat)
		}
		values[i] = n
	}
	if len(values) == 1 {
		return values[0], values[0], nil
	}
	if values[1] < values[0] {
		return 0, 0, errors.New("{{#repeat min max}} needs max at least min")
	}
	return values[0], values[1], nil
}

var knownFilters = map[string]struct{ minArgs, maxArgs int }{
	"upper":     {0, 0},
	"lower":     {0, 0},
	"trim":      {0, 0},
	"urlencode": {0, 0},
	"base64":    {0, 0},
	"json":      {0, 0},
	"default":   {1, 1},
	"add":       {1, 1},
	"keep":      {0, 1},
}

// parsePlaceholder splits a placeholder's arguments at "|" into the function
// arguments and the filters. A placeholder with an unknown filter or wrong
// filter arguments is kept as literal text.
func parsePlaceholder(text, prefix, name, rawArgs string) (templateNode, error) {
	node := templateNode{kind: placeholderNode, text: text, prefix: prefix, name: name}
	segments := splitPipes(rawArgs)
	node.args = splitArgs(segments[0])
	for _, segment := range segments[1:] {
		fields := splitArgs(segment)
		if len(fields) == 0 {
			return templateNode{kind: textNode, text: text}, errors.New("empty filter after |")
		}
		filter := templateFilter{name: fields[0], args: fields[1:]}
		spec, ok := knownFilters[filter.name]
		if !ok {
			return templateNode{kind: textNode, text: text}, fmt.Errorf("unknown filter %q (use upper, lower, trim, urlencode, base64, json, default, add or keep)", filter.name)
		}
		if len(filter.args) < spec.minArgs || len(filter.args) > spec.maxArgs {
			return templateNode{kind: textNode, text: text}, fmt.Errorf("filter %q takes %s", filter.name, filterArgsHint(spec.minArgs, spec.maxArgs))
		}
		if filter.name == "add" {
			if _, err := strconv.ParseInt(filter.args[0], 10, 64); err != nil {
				return templateNode{kind: textNode, text: text}, fmt.Errorf("filter add needs an integer, got %q", filter.args[0])
			}
		}
		node.filters = append(node.filters, filter)
	}
	return node, nil
}

func filterArgsHint(low, high int) string {
	switch {
	case high == 0:
		return "no arguments"
	case low == 0:
		return "an optional argument"
	default:
		return "one argument"
	}
}

// splitPipes splits s at "|" outside double quotes. It always returns at
// least one segment.
func splitPipes(s string) []string {
	var segments []string
	quoted, start := false, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case '|':
			if !quoted {
				segments = append(segments, s[start:i])
				start = i + 1
			}
		}
	}
	return append(segments, s[start:])
}

// splitArgs splits s at spaces and tabs. A double-quoted argument may hold
// spaces and "|"; \" and \\ escape inside it.
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inArg, quoted := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted && c == '\\' && i+1 < len(s):
			i++
			current.WriteByte(s[i])
		case c == '"':
			quoted, inArg = !quoted, true
		case !quoted && (c == ' ' || c == '\t'):
			if inArg {
				args = append(args, current.String())
				current.Reset()
				inArg = false
			}
		default:
			current.WriteByte(c)
			inArg = true
		}
	}
	if inArg {
		args = append(args, current.String())
	}
	return args
}

// applyFilter transforms value. keep is handled by the caller.
func applyFilter(filter templateFilter, value string) string {
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
	case "json":
		encoded, _ := json.Marshal(value)
		return string(encoded)
	case "add":
		n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return value
		}
		delta, _ := strconv.ParseInt(filter.args[0], 10, 64)
		return strconv.FormatInt(n+delta, 10)
	default:
		return value
	}
}
