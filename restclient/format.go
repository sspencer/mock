package restclient

import (
	"bytes"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Format returns a request file's text in the canonical layout that
// mock fmt writes:
//
//   - one blank line between sections, none before the first and a single
//     newline at the end; runs of blank lines elsewhere become one;
//   - "### name" with one space, "@name = value" definitions, and response
//     headers as "Name: value";
//   - request lines with single spaces and an upper-case method;
//   - in each blank-separated group of a section's directives, variables
//     first, then request matchers, then the directives that shape the
//     response, each keeping the comment lines directly above it;
//   - LF line endings and no trailing spaces outside bodies.
//
// Bodies are kept as written, apart from the blank lines after them, which
// are not part of the body; a section with "@trim = off" keeps its body
// byte for byte. Format fails when the text does not parse, and when the
// result would parse to routes that differ from the original in more than
// line numbers and comment order, so a formatting mistake cannot change
// what the mock serves.
func Format(source string, src []byte) ([]byte, error) {
	before, err := Parse(source, bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	lines := splitFormatLines(string(src))
	out := formatLines(lines, before)
	after, err := Parse(source, strings.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("%s: formatting would break the file, so it is left as is: %w", source, err)
	}
	if !reflect.DeepEqual(formatKey(before), formatKey(after)) {
		return nil, fmt.Errorf("%s: formatting would change the routes, so the file is left as is (please report this as a mock fmt bug)", source)
	}
	return []byte(out), nil
}

// formatLine is one line of a request file and its terminator ("\n",
// "\r\n", or "" for a last line without one).
type formatLine struct{ text, ending string }

func splitFormatLines(text string) []formatLine {
	var lines []formatLine
	for text != "" {
		line, rest, found := strings.Cut(text, "\n")
		ending := ""
		if found {
			ending = "\n"
		}
		if trimmed, ok := strings.CutSuffix(line, "\r"); ok && found {
			line, ending = trimmed, "\r\n"
		}
		lines = append(lines, formatLine{line, ending})
		text = rest
	}
	return lines
}

// formatBlock is a formatted part of a file: the definitions before the
// first section, or one section. A raw block ends in a body that must be
// kept exactly, so no blank line may follow it.
type formatBlock struct {
	text string
	raw  bool
}

func formatLines(lines []formatLine, methods []Method) string {
	first := slices.IndexFunc(lines, func(l formatLine) bool { return strings.HasPrefix(l.text, "###") })
	if first < 0 {
		first = len(lines)
	}
	var blocks []formatBlock
	next := 0 // the next method, in file order
	takeMethod := func() *Method {
		if next < len(methods) {
			next++
			return &methods[next-1]
		}
		return nil
	}

	// Before the first ###: file definitions and comments, then possibly a
	// request (REST Client style) with the comment lines directly above it.
	preamble := lines[:first]
	request := -1
	leading := 0
	for i, line := range preamble {
		trimmed := strings.TrimSpace(line.text)
		switch {
		case trimmed == "", strings.HasPrefix(trimmed, "@"):
			leading = i + 1
		case isComment(trimmed):
		default:
			request = i
		}
		if request >= 0 {
			break
		}
	}
	header := preamble
	if request >= 0 {
		header = preamble[:leading]
	}
	if text := formatDirectives(header, false); text != "" {
		blocks = append(blocks, formatBlock{text: text})
	}
	if request >= 0 {
		blocks = append(blocks, formatSection("", preamble[leading:], takeMethod()))
	}

	for start := first; start < len(lines); {
		end := start + 1
		for end < len(lines) && !strings.HasPrefix(lines[end].text, "###") {
			end++
		}
		name := strings.TrimSpace(strings.TrimPrefix(lines[start].text, "###"))
		heading := "###"
		if name != "" {
			heading += " " + name
		}
		body := lines[start+1 : end]
		if sectionHasRequest(body) {
			blocks = append(blocks, formatSection(heading, body, takeMethod()))
		} else {
			// Only definitions: file-scoped variables, kept in order.
			text := heading + "\n" + formatDirectives(body, false)
			blocks = append(blocks, formatBlock{text: strings.TrimSuffix(text, "\n") + "\n"})
		}
		start = end
	}

	var out strings.Builder
	for i, block := range blocks {
		if i > 0 && !blocks[i-1].raw {
			out.WriteString("\n")
		}
		out.WriteString(block.text)
	}
	return out.String()
}

// sectionHasRequest reports whether a section's lines (after its ### line)
// hold more than directives, comments and blank lines.
func sectionHasRequest(lines []formatLine) bool {
	return slices.ContainsFunc(lines, func(l formatLine) bool { return directiveKind(l.text) == directiveEnd })
}

const (
	directiveBlank = iota
	directiveComment
	directiveDefinition
	directiveEnd // the request line, or anything else after the directives
)

func directiveKind(text string) int {
	trimmed := strings.TrimSpace(text)
	switch {
	case trimmed == "":
		return directiveBlank
	case strings.HasPrefix(trimmed, "@"):
		return directiveDefinition
	case isComment(trimmed):
		return directiveComment
	}
	return directiveEnd
}

// formatSection formats one section: heading is its "### name" line, or ""
// for a request before the first ###; method is what the section parsed to.
func formatSection(heading string, lines []formatLine, method *Method) formatBlock {
	var out strings.Builder
	if heading != "" {
		out.WriteString(heading + "\n")
	}
	i := slices.IndexFunc(lines, func(l formatLine) bool { return directiveKind(l.text) == directiveEnd })
	out.WriteString(formatDirectives(lines[:i], true))
	out.WriteString(formatRequestLine(lines[i].text) + "\n")
	i++
	for ; i < len(lines) && strings.TrimSpace(lines[i].text) != ""; i++ {
		name, value, _ := strings.Cut(lines[i].text, ":")
		out.WriteString(strings.TrimRight(strings.TrimSpace(name)+": "+strings.TrimSpace(value), " ") + "\n")
	}
	if i == len(lines) {
		return formatBlock{text: out.String()}
	}
	i++ // the blank line that ends the headers
	body := lines[i:]
	if value, ok := method.Variable("trim"); ok && strings.TrimSpace(value) == "off" {
		out.WriteString("\n")
		for _, line := range body {
			out.WriteString(line.text + line.ending)
		}
		return formatBlock{text: out.String(), raw: true}
	}
	end := len(body)
	for end > 0 && strings.TrimSpace(body[end-1].text) == "" {
		end--
	}
	if end > 0 {
		out.WriteString("\n")
		for _, line := range body[:end] {
			out.WriteString(line.text + "\n")
		}
	}
	return formatBlock{text: out.String()}
}

// formatRequestLine joins the request line's fields with single spaces and
// upper-cases its method.
func formatRequestLine(text string) string {
	fields := strings.Fields(text)
	if len(fields) > 1 && isMethodToken(fields[0]) {
		fields[0] = strings.ToUpper(fields[0])
	}
	return strings.Join(fields, " ")
}

// formatDirectives formats a run of blank, comment and definition lines:
// blank lines at the ends are dropped and inner runs become one. With
// reorder, each blank-separated group is sorted by directiveRank, below
// any comment lines that open the first group.
func formatDirectives(lines []formatLine, reorder bool) string {
	var groups [][]string
	var group []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line.text)
		switch directiveKind(trimmed) {
		case directiveBlank:
			if len(group) > 0 {
				groups, group = append(groups, group), nil
			}
		case directiveDefinition:
			group = append(group, formatDefinition(trimmed))
		default:
			group = append(group, trimmed)
		}
	}
	if len(group) > 0 {
		groups = append(groups, group)
	}
	var out strings.Builder
	for i, group := range groups {
		if i > 0 {
			out.WriteString("\n")
		}
		if reorder {
			// Comments at the top of a section describe the section, not the
			// directive under them, so they stay first.
			top := 0
			if i == 0 {
				top = slices.IndexFunc(group, func(line string) bool { return strings.HasPrefix(line, "@") })
				if top < 0 {
					top = len(group)
				}
			}
			group = append(group[:top:top], sortDirectives(group[top:])...)
		}
		for _, line := range group {
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}

// formatDefinition writes "@name = value" with one space around "=".
func formatDefinition(line string) string {
	name, value, ok := cutDefinition(line[1:])
	if !ok {
		return line
	}
	return strings.TrimRight("@"+strings.TrimSpace(name)+" = "+strings.TrimSpace(value), " ")
}

// sortDirectives orders a group of directive lines by directiveRank, keeping
// each definition's comment lines above it and comments after the last
// definition at the end. The sort is stable, so directives of one kind keep
// their order, and with it the meaning of repeated names.
func sortDirectives(group []string) []string {
	type item struct {
		lines []string
		rank  int
	}
	var items []item
	var pending []string
	for _, line := range group {
		if !strings.HasPrefix(line, "@") {
			pending = append(pending, line)
			continue
		}
		name, _, _ := cutDefinition(line[1:])
		items = append(items, item{lines: append(pending, line), rank: directiveRank(strings.TrimSpace(name))})
		pending = nil
	}
	slices.SortStableFunc(items, func(a, b item) int { return a.rank - b.rank })
	var out []string
	for _, it := range items {
		out = append(out, it.lines...)
	}
	return append(out, pending...)
}

// directiveRank sorts a section's directives: variables (0), then request
// matchers (1), then directives that shape the response (2).
func directiveRank(name string) int {
	_, control := controlVariables[name]
	switch {
	case isMatcherName(name) || strings.HasPrefix(name, "match."):
		return 1
	case control || strings.HasPrefix(name, "callback.") || strings.HasPrefix(name, defaultHeaderPrefix):
		return 2
	}
	return 0
}

// formatKey returns copies of methods without what formatting may change:
// line numbers and the order of comments.
func formatKey(methods []Method) []Method {
	out := make([]Method, len(methods))
	for i, m := range methods {
		m.Line = 0
		lines := make(map[string]int, len(m.VariableLines))
		for name := range m.VariableLines {
			lines[name] = 0
		}
		m.VariableLines = lines
		m.Comments = slices.Sorted(slices.Values(m.Comments))
		callbacks := make([]*Callback, len(m.Callbacks))
		for j, c := range m.Callbacks {
			copied := *c
			copied.line = 0
			callbacks[j] = &copied
		}
		m.Callbacks = callbacks
		out[i] = m
	}
	return out
}
