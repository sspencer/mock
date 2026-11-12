package mockhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sspencer/mock/restclient"

	"github.com/jaswdr/faker"
)

var placeholderPattern = restclient.PlaceholderPattern

// seededReferenceTime replaces "now" for generated timestamps under @seed, so
// seeded responses do not drift with the clock.
var seededReferenceTime = time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)

// errNoResponseFile reports that a templated "< path" named no usable file
// for this request; the request gets a 404.
var errNoResponseFile = errors.New("no response file for this request")

func statusAllowsBody(status int) bool {
	return status != http.StatusNoContent && status != http.StatusNotModified
}

// renderer renders one response: its file path, body and headers share the
// random source and the values kept with "| keep".
type renderer struct {
	method *restclient.Method
	values templateValues
	rng    *rand.Rand
	faker  faker.Faker
	now    time.Time
	seeded bool
	root   *renderScope
	// active holds the {{name}} variables being expanded, to stop cycles.
	active map[string]bool
	// sample, set when linting, fills placeholders nothing resolves (request
	// values, unset environment variables) with sampleValue.
	sample bool
	// fixture is the decoded @fixture file, loaded on first use.
	fixture     any
	fixtureErr  error
	fixtureDone bool
	// verbatim is set when the body is a response file sent unchanged, so it
	// can be served with range and conditional request support.
	verbatim bool
}

// sampleValue stands in for request values when a body is rendered without a
// request. It is valid JSON on its own and inside a string.
const sampleValue = "1"

// renderScope is the top level of a response or one copy of a repeat block.
type renderScope struct {
	parent   *renderScope
	index    int
	inRepeat bool
	kept     map[string]string
	// item is the current element of an each block, when hasItem is set.
	item    any
	hasItem bool
}

// currentItem returns the element of the innermost each block.
func (s *renderScope) currentItem() (any, bool) {
	for ; s != nil; s = s.parent {
		if s.hasItem {
			return s.item, true
		}
	}
	return nil, false
}

// rngGenerator adapts math/rand/v2 to faker's generator interface.
type rngGenerator struct{ rng *rand.Rand }

func (g rngGenerator) Intn(n int) int { return g.rng.IntN(n) }
func (g rngGenerator) Int() int       { return g.rng.Int() }

func newRenderer(method *restclient.Method, values templateValues) *renderer {
	r := &renderer{method: method, values: values, now: time.Now(), root: &renderScope{}, active: make(map[string]bool)}
	if seed, ok := method.Variable("seed"); ok {
		r.seeded = true
		r.rng = rand.New(rand.NewPCG(requestSeed(seed, values.facts), 0x6d6f636b))
	} else {
		r.rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	r.faker = faker.Faker{Generator: rngGenerator{r.rng}}
	return r
}

// requestSeed mixes the @seed value with the request, so the same request
// always gets the same generated values and different requests differ.
func requestSeed(seed string, facts *requestFacts) uint64 {
	h := fnv.New64a()
	h.Write([]byte(seed))
	if facts != nil {
		h.Write([]byte{0})
		h.Write([]byte(facts.r.Method))
		h.Write([]byte{0})
		h.Write([]byte(facts.r.URL.RequestURI()))
		h.Write([]byte{0})
		h.Write(facts.body.data)
	}
	return h.Sum64()
}

// bodyFile is a route's external body file for one request.
type bodyFile struct {
	// name is the relative path, with placeholders expanded.
	name string
	// path is where it was resolved on disk.
	path     string
	template bool
}

// file expands and resolves the route's "< path", if it has one. A templated
// path that cannot name a file returns errNoResponseFile.
func (r *renderer) file() (bodyFile, bool, error) {
	if r.method.File == "" {
		return bodyFile{}, false, nil
	}
	name := r.method.File
	isTemplate := restclient.FileIsTemplate(name)
	if isTemplate {
		expanded, err := r.text(name, false)
		if err != nil {
			return bodyFile{}, true, err
		}
		if restclient.FileIsTemplate(expanded) || restclient.ValidateBodyFile(expanded) != nil {
			return bodyFile{}, true, errNoResponseFile
		}
		name = expanded
	}
	return bodyFile{name: name, path: restclient.ResolveFile(r.method.Source, name), template: isTemplate}, true, nil
}

// headers renders the route's response headers. Without a Content-Type
// header, a "< path" file is typed by its extension; failing that, a route
// with a default Content-Type (@default.header or -H) sniffs the rendered
// body and sends the default unless sniffing finds a specific type. Routes
// with neither are sniffed when the response is written.
func (r *renderer) headers(file bodyFile, body []byte) (http.Header, error) {
	headers := r.method.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	for _, values := range headers {
		for i, value := range values {
			if r.method.Template == "off" {
				continue
			}
			expanded, err := r.text(value, false)
			if err != nil {
				return nil, err
			}
			values[i] = expanded
		}
	}
	if headers.Get("Content-Type") != "" {
		return headers, nil
	}
	if file.path != "" {
		if contentType := mime.TypeByExtension(filepath.Ext(file.path)); contentType != "" {
			headers.Set("Content-Type", contentType)
			return headers, nil
		}
	}
	if r.method.DefaultContentType == "" {
		return headers, nil
	}
	if sniffed := http.DetectContentType(body); len(body) > 0 && specificContentType(sniffed) {
		headers.Set("Content-Type", sniffed)
		return headers, nil
	}
	contentType := r.method.DefaultContentType
	if r.method.Template != "off" {
		expanded, err := r.text(contentType, false)
		if err != nil {
			return nil, err
		}
		contentType = expanded
	}
	headers.Set("Content-Type", contentType)
	return headers, nil
}

// specificContentType reports whether a sniffed type says more than
// http.DetectContentType's fallbacks for text it cannot place (text/plain,
// which includes JSON) and for unknown bytes (application/octet-stream).
func specificContentType(sniffed string) bool {
	mediaType, _, _ := strings.Cut(sniffed, ";")
	return mediaType != "text/plain" && mediaType != "application/octet-stream"
}

// body renders the response body. A templated file path that names a missing
// file returns errNoResponseFile.
func (r *renderer) body(file bodyFile, hasFile bool) ([]byte, error) {
	if !hasFile {
		if r.method.Body == "" || r.method.Template == "off" {
			return []byte(r.method.Body), nil
		}
		body, err := r.text(r.method.Body, r.isJSON(""))
		return []byte(body), err
	}
	body, err := readResponseFile(r.method.Source, file.name)
	if err != nil {
		if file.template && errors.Is(err, os.ErrNotExist) {
			return nil, errNoResponseFile
		}
		return nil, fmt.Errorf("%s: %w", file.path, err)
	}
	if !fileIsTemplated(*r.method, file.path, body) {
		r.verbatim = true
		return body, nil
	}
	rendered, err := r.text(string(body), r.isJSON(file.path))
	return []byte(rendered), err
}

// fileIsTemplated reports whether a response file's placeholders are expanded:
// always with "@template = on", never with "off", and otherwise when its
// content type is textual and its content looks like text.
func fileIsTemplated(method restclient.Method, path string, body []byte) bool {
	switch method.Template {
	case "on":
		return true
	case "off":
		return false
	}
	return fileTemplatesEnabled(method, path) && isMostlyText(body)
}

func (r *renderer) isJSON(path string) bool {
	contentType := responseContentType(*r.method, path)
	return contentType == "application/json" || strings.HasSuffix(contentType, "+json")
}

// responseContentType is the route's media type before its body is read: its
// Content-Type header, else the type of the file extension, else its default
// Content-Type.
func responseContentType(method restclient.Method, path string) string {
	contentType := method.Headers.Get("Content-Type")
	if contentType == "" && path != "" {
		contentType = mime.TypeByExtension(filepath.Ext(path))
	}
	if contentType == "" {
		contentType = method.DefaultContentType
	}
	contentType, _, _ = strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(contentType))
}

// text renders a template. In JSON mode, values inserted inside JSON strings
// are escaped and repeat copies are separated by commas.
func (r *renderer) text(input string, jsonMode bool) (string, error) {
	nodes, _ := parseTemplate(input)
	out := &templateWriter{json: jsonMode}
	if err := r.render(nodes, r.root, out); err != nil {
		return "", err
	}
	return string(out.buf), nil
}

// templateWriter collects output and, in JSON mode, tracks whether the
// literal text so far has left it inside a JSON string.
type templateWriter struct {
	buf      []byte
	json     bool
	inString bool
	escaped  bool
}

func (w *templateWriter) literal(text string) {
	w.buf = append(w.buf, text...)
	if !w.json {
		return
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case w.escaped:
			w.escaped = false
		case w.inString && c == '\\':
			w.escaped = true
		case c == '"':
			w.inString = !w.inString
		}
	}
}

func (w *templateWriter) value(value string) {
	if w.json && w.inString {
		encoded, _ := json.Marshal(value)
		w.buf = append(w.buf, encoded[1:len(encoded)-1]...)
		return
	}
	w.buf = append(w.buf, value...)
}

// renderCopy renders one copy of a repeat or each block. In JSON mode,
// outside a string, copies are separated by commas: a copy that renders
// only whitespace (an {{#if}} that did not hold) is dropped, and the comma
// goes before the whitespace that ends the previous copy with content.
// hadContent tracks whether an earlier copy of this block had any.
func (r *renderer) renderCopy(nodes []templateNode, scope *renderScope, out *templateWriter, hadContent *bool) error {
	mark, inString := len(out.buf), out.inString
	if err := r.render(nodes, scope, out); err != nil {
		return err
	}
	if !out.json || inString {
		return nil
	}
	if len(bytes.TrimSpace(out.buf[mark:])) == 0 {
		out.buf = out.buf[:mark]
		return nil
	}
	if *hadContent {
		end := mark
		for end > 0 && strings.IndexByte(" \t\r\n", out.buf[end-1]) >= 0 {
			end--
		}
		tail := append([]byte(nil), out.buf[end:]...)
		out.buf = append(append(out.buf[:end], ','), tail...)
	}
	*hadContent = true
	return nil
}

func (r *renderer) render(nodes []templateNode, scope *renderScope, out *templateWriter) error {
	for _, node := range nodes {
		switch node.kind {
		case textNode:
			out.literal(node.text)
		case placeholderNode:
			if value, ok := r.resolve(node, scope); ok {
				out.value(value)
			} else if r.sample {
				out.value(sampleValue)
			} else {
				out.buf = append(out.buf, node.text...)
			}
		case repeatNode:
			count := node.min
			if node.max > node.min {
				count += r.rng.IntN(node.max - node.min + 1)
			}
			hadContent := false
			for i := range count {
				copyScope := &renderScope{parent: scope, index: i, inRepeat: true}
				if err := r.renderCopy(node.children, copyScope, out, &hadContent); err != nil {
					return err
				}
			}
		case ifNode:
			children := node.elseChildren
			if r.condition(node.cond, scope) {
				children = node.children
			}
			if err := r.render(children, scope, out); err != nil {
				return err
			}
		case eachNode:
			items := r.pageItems(r.eachItems(node.source, scope), node.filters, scope)
			if len(items) == 0 && r.sample {
				// Lint one copy, with placeholders standing in for the item.
				if err := r.render(node.children, &renderScope{parent: scope, inRepeat: true}, out); err != nil {
					return err
				}
			}
			hadContent := false
			for i, item := range items {
				copyScope := &renderScope{parent: scope, index: i, inRepeat: true, item: item, hasItem: true}
				if err := r.renderCopy(node.children, copyScope, out, &hadContent); err != nil {
					return err
				}
			}
		}
		if len(out.buf) > maxRenderedBytes {
			return errRenderedTooLarge
		}
	}
	return nil
}

// eachItems returns the elements an each block iterates over: the elements of
// the array at source, or the one value there when it is not an array.
// Missing values, null, and bodies that are not JSON give none.
func (r *renderer) eachItems(source string, scope *renderScope) []any {
	source, ok := r.expandSelectors(source, scope)
	if !ok {
		return nil
	}
	var root any
	var path string
	if p, ok := strings.CutPrefix(source, "json."); ok {
		if r.values.facts == nil {
			return nil
		}
		doc, err := r.values.facts.jsonDocument()
		if err != nil {
			return nil
		}
		root, path = doc, p
	} else if p, ok := fixturePath(source); ok {
		doc, found := r.fixtureDocument()
		if !found {
			return nil
		}
		root, path = doc, p
	} else if p, ok := itemPath(source); ok {
		item, found := scope.currentItem()
		if !found {
			return nil
		}
		root, path = item, p
	} else {
		return nil
	}
	nodes := selectJSONNodes(root, path)
	if len(nodes) == 1 {
		if array, ok := nodes[0].([]any); ok {
			return array
		}
	}
	items := nodes[:0:0]
	for _, node := range nodes {
		if node != nil {
			items = append(items, node)
		}
	}
	return items
}

// resolve evaluates a placeholder and its filters. ok is false when it stays
// literal: nothing supplies its value and it has no default.
func (r *renderer) resolve(node templateNode, scope *renderScope) (string, bool) {
	filters := node.filters
	value, ok := "", false
	// A kept value skips evaluation and the filters before its keep.
	for i, filter := range filters {
		if filter.name != "keep" {
			continue
		}
		if kept, found := scope.lookupKept(keepKey(node, filter)); found {
			value, ok, filters = kept, true, filters[i+1:]
		}
		break
	}
	if !ok {
		value, ok = r.evaluate(node, scope)
	}
	if !ok {
		// A default supplies the value; the filters after it still apply.
		for i, filter := range filters {
			if filter.name == "default" {
				var found bool
				if value, found = r.argValue(filter.args[0], scope); !found && r.sample {
					value = sampleValue
				}
				ok, filters = true, filters[i+1:]
				break
			}
		}
		if !ok {
			return "", false
		}
	}
	for _, filter := range filters {
		switch filter.name {
		case "default":
		case "keep":
			key := keepKey(node, filter)
			if _, found := scope.lookupKept(key); !found {
				if scope.kept == nil {
					scope.kept = make(map[string]string)
				}
				scope.kept[key] = value
			}
		default:
			value = applyFilter(r.resolveArgs(filter, scope), value)
		}
	}
	return value, true
}

// argValue resolves a filter, block or condition argument: a $reference
// gives its value (ok is false when it has none), anything else is literal.
func (r *renderer) argValue(arg string, scope *renderScope) (string, bool) {
	if !isRef(arg) {
		return arg, true
	}
	node, _ := refNode(arg)
	return r.resolve(node, scope)
}

// resolveArgs returns filter with its $reference arguments resolved. One
// that has no value becomes "".
func (r *renderer) resolveArgs(filter templateFilter, scope *renderScope) templateFilter {
	if !slices.ContainsFunc(filter.args, isRef) {
		return filter
	}
	args := make([]string, len(filter.args))
	for i, arg := range filter.args {
		args[i], _ = r.argValue(arg, scope)
	}
	return templateFilter{name: filter.name, args: args}
}

// condition evaluates an {{#if}} condition. A presence test holds when the
// reference has a value other than "", "false", "0" or "null"; == and !=
// compare text, and a reference with no value equals nothing.
func (r *renderer) condition(cond *templateCond, scope *renderScope) bool {
	value, ok := r.resolve(cond.left, scope)
	if cond.op == "" {
		return ok && truthy(value)
	}
	right, rightOK := r.argValue(cond.right, scope)
	equal := ok && rightOK && value == right
	return equal == (cond.op == "==")
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "false", "0", "null":
		return false
	}
	return true
}

// pageItems applies an each block's offset and limit. An argument that is
// not a whole number is ignored; negative values count as 0.
func (r *renderer) pageItems(items []any, filters []templateFilter, scope *renderScope) []any {
	for _, filter := range filters {
		text, _ := r.argValue(filter.args[0], scope)
		n, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil {
			continue
		}
		n = max(n, 0)
		switch filter.name {
		case "offset":
			items = items[min(n, len(items)):]
		case "limit":
			items = items[:min(n, len(items))]
		}
	}
	return items
}

// selectorRefPattern matches a [field=$reference] selector in a name.
var selectorRefPattern = regexp.MustCompile(`\[([A-Za-z0-9_-]+)=(\$[^\]\s{}"]+)\]`)

// expandSelectors replaces the $references in a name's [field=$ref]
// selectors with their values, as in fixture.users[id=$path.id]. ok is false
// when a reference has no value or a value a selector cannot hold.
func (r *renderer) expandSelectors(name string, scope *renderScope) (string, bool) {
	if !strings.Contains(name, "=$") {
		return name, true
	}
	ok := true
	expanded := selectorRefPattern.ReplaceAllStringFunc(name, func(selector string) string {
		parts := selectorRefPattern.FindStringSubmatch(selector)
		value, found := r.argValue(parts[2], scope)
		if !found || value == "" || strings.ContainsAny(value, "]\" \t\r\n{}") {
			ok = false
			return selector
		}
		return "[" + parts[1] + "=" + value + "]"
	})
	return expanded, ok
}

func keepKey(node templateNode, keep templateFilter) string {
	label := ""
	if len(keep.args) > 0 {
		label = keep.args[0]
	}
	return node.prefix + node.name + "\x00" + strings.Join(node.args, " ") + "\x00" + label
}

func (s *renderScope) lookupKept(key string) (string, bool) {
	for ; s != nil; s = s.parent {
		if value, ok := s.kept[key]; ok {
			return value, true
		}
	}
	return "", false
}

func (r *renderer) evaluate(node templateNode, scope *renderScope) (string, bool) {
	if node.prefix != "$" {
		raw, ok := r.method.Variables[node.name]
		if !ok || len(node.args) > 0 || r.active[node.name] {
			return "", false
		}
		r.active[node.name] = true
		nodes, _ := parseTemplate(raw)
		out := &templateWriter{}
		err := r.render(nodes, scope, out)
		delete(r.active, node.name)
		if err != nil {
			return "", false
		}
		value := string(out.buf)
		if node.prefix == "%" {
			value = strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
		}
		return value, true
	}
	name, ok := r.expandSelectors(node.name, scope)
	if !ok {
		return "", false
	}
	node.name = name
	if node.name == "jwt" {
		return r.jwt(node.args, scope)
	}
	if len(node.args) > 0 {
		value, err := callFunction(node.name, node.args, r.now, r.rng)
		return value, err == nil
	}
	if node.name == "index" {
		for s := scope; s != nil; s = s.parent {
			if s.inRepeat {
				return strconv.Itoa(s.index), true
			}
		}
	}
	if path, ok := fixturePath(node.name); ok {
		return r.fixtureValue(path)
	}
	if path, ok := requestEchoPath(node.name); ok && r.values.facts != nil {
		return r.values.facts.requestValue(path)
	}
	if path, ok := itemPath(node.name); ok {
		if item, found := scope.currentItem(); found {
			for _, value := range selectJSONNodes(item, path) {
				if text, ok := jsonText(value); ok {
					return text, true
				}
			}
			return "", false
		}
	}
	if value, ok := r.values.lookup(node.name); ok {
		return value, true
	}
	if value, err := callFunction(node.name, nil, r.now, r.rng); err == nil {
		return value, true
	}
	if value := r.generatedValue(node.name); value != "" {
		return value, true
	}
	return "", false
}

func isMostlyText(body []byte) bool {
	if len(body) == 0 {
		return true
	}
	if !utf8.Valid(body) {
		return false
	}
	for _, b := range body {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' || b == 127 {
			return false
		}
	}
	return true
}

func (r *renderer) generatedValue(key string) string {
	f := r.faker
	reference := r.now
	if r.seeded {
		reference = seededReferenceTime
	}
	switch key {
	case "integer":
		return fmt.Sprint(f.UInt16())
	case "float":
		return fmt.Sprint(f.Float32(2, 0, 100_000))
	case "bool":
		return fmt.Sprint(f.Boolean().Bool())
	case "uuid", "guid":
		return r.uuid()
	case "timestamp":
		return fmt.Sprint(f.Time().Unix(reference))
	case "isoTimestamp":
		return f.Time().ISO8601(reference)
	case "name":
		return f.Person().Name()
	case "firstName":
		return f.Person().FirstName()
	case "lastName":
		return f.Person().LastName()
	case "phone":
		return f.Phone().Number()
	case "user":
		return f.Internet().User()
	case "email":
		return f.Internet().Email()
	case "url":
		return f.Internet().URL()
	case "server":
		return f.Internet().Domain()
	case "hash":
		return f.Hash().MD5()
	case "file":
		return f.File().AbsoluteFilePath(3 + r.rng.IntN(4))
	case "sentence":
		return f.Lorem().Sentence(8 + r.rng.IntN(9))
	case "paragraph":
		return f.Lorem().Paragraph(3 + r.rng.IntN(2))
	case "article":
		return f.Lorem().Paragraph(5 + r.rng.IntN(3))
	case "company":
		return f.Company().Name()
	case "city":
		return f.Address().City()
	case "country":
		return f.Address().Country()
	case "address":
		return f.Address().Address()
	case "ip":
		return f.Internet().Ipv4()
	case "word":
		return f.Lorem().Word()
	case "price":
		return strconv.FormatFloat(1+r.rng.Float64()*998.99, 'f', 2, 64)
	case "ulid":
		return r.ulid(reference)
	default:
		return ""
	}
}

// uuid returns a version 4 UUID from the response's random source, so it
// is reproducible under @seed.
func (r *renderer) uuid() string {
	var b [16]byte
	for i := 0; i < len(b); i += 8 {
		v := r.rng.Uint64()
		for j := range 8 {
			b[i+j] = byte(v >> (8 * j))
		}
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// crockford is the base32 alphabet of ULIDs.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ulid returns a ULID: 48 bits of milliseconds since the Unix epoch at
// reference, then 80 random bits from the response's random source, in
// Crockford base32 (26 characters, sortable by time).
func (r *renderer) ulid(reference time.Time) string {
	var b [16]byte
	ms := uint64(reference.UnixMilli())
	for i := range 6 {
		b[i] = byte(ms >> (8 * (5 - i)))
	}
	hi, lo := r.rng.Uint64(), r.rng.Uint64()
	for i := 6; i < 16; i++ {
		if i < 14 {
			b[i] = byte(hi >> (8 * (i - 6)))
		} else {
			b[i] = byte(lo >> (8 * (i - 14)))
		}
	}
	// 128 bits as 26 five-bit groups, the first holding the top 3 bits.
	out := make([]byte, 26)
	var acc uint64
	bits, n := 2, 0 // 2 leading zero bits pad 128 to 130.
	for _, v := range b {
		acc = acc<<8 | uint64(v)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[n] = crockford[(acc>>bits)&31]
			n++
		}
	}
	return string(out)
}

func isGeneratedKey(key string) bool {
	switch key {
	case "integer", "float", "bool", "uuid", "guid", "timestamp", "isoTimestamp", "name", "firstName", "lastName", "phone", "user", "email", "url", "server", "hash", "file", "sentence", "paragraph", "article",
		"company", "city", "country", "address", "ip", "word", "price", "ulid":
		return true
	default:
		return false
	}
}

// readResponseFile uses a rooted filesystem to prevent symlinks from escaping
// the root the file is found in (the .http file's directory or the workspace
// root), including when a dependency is replaced during reload.
func readResponseFile(source, name string) ([]byte, error) {
	var firstErr error
	for _, dir := range restclient.BodyFileRoots(source) {
		body, err := readRooted(dir, name)
		if err == nil {
			return body, nil
		}
		if firstErr == nil || os.IsNotExist(firstErr) && !os.IsNotExist(err) {
			firstErr = err
		}
	}
	return nil, firstErr
}

func readRooted(dir, name string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(filepath.Clean(name))
}

func fileTemplatesEnabled(method restclient.Method, path string) bool {
	contentType := responseContentType(method, path)
	return strings.HasPrefix(contentType, "text/") || contentType == "application/json" || strings.HasSuffix(contentType, "+json") || contentType == "application/xml" || strings.HasSuffix(contentType, "+xml") || contentType == "application/javascript"
}
