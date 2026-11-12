package mockhttp

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/sspencer/mock/restclient"
)

// A response template is text with placeholders ({{name}}, {{%name}},
// {{$name args | filter ...}}), repeat blocks ({{#repeat n}} ...
// {{/repeat}}), each blocks ({{#each $json.items}} ... {{/each}}) and if
// blocks ({{#if $query.expand}} ... {{else}} ... {{/if}}). It is parsed into
// nodes once per render.

const (
	// maxRepeat caps one {{#repeat}} block's count.
	maxRepeat = 1000
	// maxRenderedBytes caps a rendered body, so nested repeats cannot run
	// away with memory.
	maxRenderedBytes = 16 << 20
)

var errRenderedTooLarge = fmt.Errorf("rendered body is larger than %d MiB", maxRenderedBytes>>20)

// templateTagPattern matches block tags and placeholders. Submatches 1 and 2
// are an opening tag's block name and argument, submatch 3 is a closing tag's
// block name, submatch 4 is an {{else}} tag, and submatches 5-7 are
// restclient.PlaceholderPattern's prefix, name and arguments.
var templateTagPattern = regexp.MustCompile(`\{\{#(repeat|each|if)(?:[ \t]+([^{}]*?))?[ \t]*}}|\{\{/(repeat|each|if)[ \t]*}}|(\{\{else[ \t]*}})|` + restclient.PlaceholderPattern.String())

// refPattern matches a reference in an {{#if}} condition or a filter
// argument: $name (a request value, fixture field or generator) or a plain
// variable name.
var refPattern = regexp.MustCompile(`^(\$?)(` + restclient.PlaceholderName + `)$`)

type nodeKind int

const (
	textNode nodeKind = iota
	placeholderNode
	repeatNode
	// eachNode renders its children once per element of the array at source.
	eachNode
	// ifNode renders its children when cond holds, else its elseChildren.
	ifNode
)

// templateCond is an {{#if}} condition: a presence test of left, or left
// compared with right ("==" or "!=").
type templateCond struct {
	left  templateNode
	op    string
	right string
}

type templateNode struct {
	kind nodeKind
	// text is literal text, or a placeholder as written.
	text string

	prefix  string
	name    string
	args    []string
	filters []templateFilter

	min, max int
	// source is an each block's array, such as "json.items" or
	// "fixture.users". Its filters are offset and limit.
	source       string
	children     []templateNode
	cond         *templateCond
	elseChildren []templateNode
}

// blockName is the tag name of a block node.
func (n templateNode) blockName() string {
	switch n.kind {
	case eachNode:
		return "each"
	case ifNode:
		return "if"
	}
	return "repeat"
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
		// elseTag is the {{else}} seen in an if block; its children so far
		// are then in node.children.
		elseTag string
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
		isOpen, isClose, isElse := loc[2] >= 0, loc[6] >= 0, loc[8] >= 0
		if isOpen || isClose || isElse {
			// A tag alone on its line takes the whole line with it, so block
			// tags do not leave blank lines in the output.
			start, end = standaloneLine(input, start, end, previous)
		}
		appendText(input[previous:start])
		previous = end
		switch {
		case isOpen:
			node, err := parseBlockTag(tag, input[loc[2]:loc[3]], submatch(input, loc, 4))
			if err != nil {
				problems = append(problems, templateProblem{tag, err.Error()})
				appendText(input[start:end])
				continue
			}
			stack = append(stack, frame{node: node, opener: input[start:end], parent: current})
			current = nil
		case isElse:
			if len(stack) == 0 || stack[len(stack)-1].node.kind != ifNode || stack[len(stack)-1].elseTag != "" {
				problems = append(problems, templateProblem{tag, "{{else}} is only allowed once inside {{#if}} ... {{/if}}"})
				appendText(input[start:end])
				continue
			}
			top := &stack[len(stack)-1]
			top.node.children, top.elseTag = current, input[start:end]
			current = nil
		case isClose:
			name := input[loc[6]:loc[7]]
			if len(stack) == 0 || stack[len(stack)-1].node.blockName() != name {
				problems = append(problems, templateProblem{tag, "{{/" + name + "}} has no matching {{#" + name + "}}"})
				appendText(input[start:end])
				continue
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if top.elseTag != "" {
				top.node.elseChildren = current
			} else {
				top.node.children = current
			}
			current = append(top.parent, top.node)
		default:
			node, err := parsePlaceholder(tag, input[loc[10]:loc[11]], input[loc[12]:loc[13]], submatch(input, loc, 14))
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
		name := top.node.blockName()
		problems = append(problems, templateProblem{top.node.text, "{{#" + name + "}} has no matching {{/" + name + "}}"})
		children := current
		if top.elseTag != "" {
			children = slices.Concat(top.node.children, []templateNode{{kind: textNode, text: top.elseTag}}, current)
		}
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

// parseBlockTag parses an opening {{#repeat count}}, {{#each $source}} or
// {{#if condition}} tag.
func parseBlockTag(tag, name, arg string) (templateNode, error) {
	switch name {
	case "repeat":
		low, high, err := parseRepeatCount(arg)
		if err != nil {
			return templateNode{}, err
		}
		return templateNode{kind: repeatNode, text: tag, min: low, max: high}, nil
	case "if":
		cond, err := parseCondition(arg)
		if err != nil {
			return templateNode{}, err
		}
		return templateNode{kind: ifNode, text: tag, cond: &cond}, nil
	}
	segments := splitPipes(arg)
	var filters []templateFilter
	for _, segment := range segments[1:] {
		fields := splitArgs(segment)
		if len(fields) != 2 || fields[0] != "offset" && fields[0] != "limit" || !isIntegerOrRef(fields[1]) {
			return templateNode{}, errors.New(`{{#each}} takes only "| offset n" and "| limit n", with a whole number or a $reference such as $query.limit`)
		}
		filters = append(filters, templateFilter{name: fields[0], args: fields[1:]})
	}
	node, err := parseEachSource(tag, segments[0])
	node.filters = filters
	return node, err
}

// parseCondition parses an {{#if}} condition: "$ref" (present and not
// empty, false, 0 or null), "$ref == value" or "$ref != value". The value is
// literal text, quoted when it holds spaces, or another $reference.
func parseCondition(arg string) (templateCond, error) {
	const usage = `{{#if}} needs a reference, optionally compared with == or !=, such as {{#if $query.expand}} or {{#if $json.type == card}}`
	fields := splitArgs(arg)
	if len(fields) != 1 && (len(fields) != 3 || fields[1] != "==" && fields[1] != "!=") {
		return templateCond{}, errors.New(usage)
	}
	left, ok := refNode(fields[0])
	if !ok {
		return templateCond{}, errors.New(usage)
	}
	cond := templateCond{left: left}
	if len(fields) == 3 {
		cond.op, cond.right = fields[1], fields[2]
	}
	return cond, nil
}

// refNode turns "$name" or "name" into a placeholder node.
func refNode(text string) (templateNode, bool) {
	match := refPattern.FindStringSubmatch(text)
	if match == nil {
		return templateNode{}, false
	}
	return templateNode{kind: placeholderNode, text: "{{" + text + "}}", prefix: match[1], name: match[2]}, true
}

// isRef reports whether a filter or block argument is a $reference, which is
// resolved when the response is rendered.
func isRef(arg string) bool {
	match := refPattern.FindStringSubmatch(arg)
	return match != nil && match[1] == "$"
}

func isIntegerOrRef(arg string) bool {
	_, err := strconv.Atoi(arg)
	return err == nil || isRef(arg)
}

func isNumberOrRef(arg string) bool {
	_, err := strconv.ParseFloat(arg, 64)
	return err == nil || isRef(arg)
}

// parseEachSource parses the array of an {{#each $source}} tag.
func parseEachSource(tag, arg string) (templateNode, error) {
	source, ok := strings.CutPrefix(strings.TrimSpace(arg), "$")
	if ok && !strings.ContainsAny(source, " \t") {
		if path, isJSON := strings.CutPrefix(source, "json."); isJSON && path != "" {
			if _, err := restclient.ParseJSONPath(path); err == nil {
				return templateNode{kind: eachNode, text: tag, source: source}, nil
			}
		} else if path, isFixture := fixturePath(source); isFixture {
			if path == "" {
				return templateNode{kind: eachNode, text: tag, source: source}, nil
			}
			if _, err := restclient.ParseJSONPath(path); err == nil {
				return templateNode{kind: eachNode, text: tag, source: source}, nil
			}
		} else if path, isItem := itemPath(source); isItem {
			if path == "" {
				return templateNode{kind: eachNode, text: tag, source: source}, nil
			}
			if _, err := restclient.ParseJSONPath(path); err == nil {
				return templateNode{kind: eachNode, text: tag, source: source}, nil
			}
		}
	}
	return templateNode{}, errors.New(`{{#each}} needs a JSON array, such as {{#each $json.items}}, {{#each $fixture.users}}, {{#each $fixture}} or {{#each $item.tags}}`)
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

// filterSpec is a filter's argument count and, for numeric filters, the
// check each argument must pass (a $reference always passes).
type filterSpec struct {
	minArgs, maxArgs int
	arg              func(string) bool
	hint             string
}

var knownFilters = map[string]filterSpec{
	"upper":        {0, 0, nil, ""},
	"lower":        {0, 0, nil, ""},
	"trim":         {0, 0, nil, ""},
	"urlencode":    {0, 0, nil, ""},
	"base64":       {0, 0, nil, ""},
	"base64decode": {0, 0, nil, ""},
	"json":         {0, 0, nil, ""},
	"length":       {0, 0, nil, ""},
	"sha256":       {0, 0, nil, ""},
	"int":          {0, 0, nil, ""},
	"default":      {1, 1, nil, ""},
	"keep":         {0, 1, nil, ""},
	"replace":      {2, 2, nil, ""},
	"hmac":         {1, 1, nil, ""},
	"date":         {1, 1, nil, ""},
	"add":          {1, 1, isNumberOrRef, "a number"},
	"sub":          {1, 1, isNumberOrRef, "a number"},
	"mul":          {1, 1, isNumberOrRef, "a number"},
	"round":        {0, 1, isIntegerOrRef, "a whole number of decimal places"},
	"slice":        {1, 2, isIntegerOrRef, "whole numbers"},
}

// filterNames lists the filters for error messages.
const filterNames = "upper, lower, trim, urlencode, base64, base64decode, json, length, sha256, int, default, keep, replace, hmac, date, add, sub, mul, round or slice"

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
			return templateNode{kind: textNode, text: text}, fmt.Errorf("unknown filter %q (use %s)", filter.name, filterNames)
		}
		if len(filter.args) < spec.minArgs || len(filter.args) > spec.maxArgs {
			return templateNode{kind: textNode, text: text}, fmt.Errorf("filter %q takes %s", filter.name, filterArgsHint(spec.minArgs, spec.maxArgs))
		}
		for _, arg := range filter.args {
			if spec.arg != nil && !spec.arg(arg) {
				return templateNode{kind: textNode, text: text}, fmt.Errorf("filter %s needs %s, got %q", filter.name, spec.hint, arg)
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
	case low == high && low == 1:
		return "one argument"
	case low == high:
		return fmt.Sprintf("%d arguments", low)
	default:
		return fmt.Sprintf("%d or %d arguments", low, high)
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
