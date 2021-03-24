package mock

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
)

type schemaParser struct {
	baseDir string
}

type tempSchema struct {
	Method      string
	Path        string
	Status      int
	ContentType string
	Body        []byte
}

// SchemaFile parses an API schema file.
func SchemaFile(fn string) (schemas []*Schema, err error) {
	f, err := os.Open(fn)
	if err != nil {
		return
	}

	defer f.Close()

	dir := path.Dir(fn)

	temps, err := SchemaReader(f, dir)
	if err != nil {
		return nil, err
	}

	// Combine duplicate Method Paths into same route that has multiple responses
	m := make(map[string]*Schema)
	for _, t := range temps {
		key := fmt.Sprintf("%s:%s", t.Method, t.Path)
		resp := Response{
			Status: t.Status,
			ContentType: t.ContentType,
			Body: t.Body,
		}

		if schema, ok := m[key]; ok {
			schema.Responses = append(schema.Responses, resp)
		} else {
			m[key] = &Schema{
				Method: t.Method,
				Path: t.Path,
				Responses: []Response{resp},
			}
		}
	}

	for _, s := range m {
		schemas = append(schemas, s)
	}

	return schemas, nil
}

// SchemaReader parses an API schema.
func SchemaReader(r io.Reader, dir string) ([]*tempSchema, error) {
	var schemas []*tempSchema
	sp := &schemaParser{dir}

	scanner := bufio.NewScanner(r)
	state := stateNone

	var body []byte
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		switch state {
		case stateNone:
			if len(line) == 0 || line[0:1] == "#" {
				continue
			}

			schema, err := sp.parse(line, lineNum)
			if err != nil {
				return nil, err
			}
			schemas = append(schemas, schema)
			if len(schema.Body) == 0 {
				state = stateBody
				body = []byte{}
			} else {
				// schema had optional @file response, no Response expected
				state = stateNone
			}

		case stateBody:
			if len(line) > 0 {
				body = append(body, line...)
			} else {
				if len(body) > 0 && len(schemas) > 0 {
					schemas[len(schemas)-1].Body = body
					body = []byte{}
				}
				state = stateNone
			}

		default:
			continue
		}
	}

	if len(body) > 0 && len(schemas) > 0 {
		schemas[len(schemas)-1].Body = body
	}

	return schemas, nil
}

func (sp *schemaParser) parse(line string, lineNum int) (*tempSchema, error) {
	tokens := strings.Split(line, " ")
	tlen := len(tokens)
	if tlen < 3 {
		return nil, fmt.Errorf("parsing line %d: %s", lineNum, line)
	}

	if !sp.isHTTPMethod(tokens[0]) {
		return nil, fmt.Errorf("invalid http method, line %d: %s", lineNum, line)
	}

	if !sp.isHTTPStatusCode(tokens[1]) {
		return nil, fmt.Errorf("invalid http status code, line %d: %s", lineNum, line)
	}

	if !sp.isHTTPPath(tokens[2]) {
		return nil, fmt.Errorf("invalid path, line %d: %s", lineNum, line)
	}

	contentType := "application/json"
	body := []byte{}

	if tlen > 3 {
		rest := strings.Join(tokens[3:tlen], "")
		rlen := len(rest)
		if rlen < 2 {
			return nil, fmt.Errorf("invalid optional, line %d: %s", lineNum, line)
		}

		first := rest[0:1]
		last := rest[rlen-1 : rlen]

		if first == "\"" && last == "\"" {
			contentType = rest[1 : rlen-2]
		}

		if first == "@" {
			var err error
			fn := path.Join(sp.baseDir, path.Clean(rest[1:rlen]))
			contentType = mime.TypeByExtension(path.Ext(fn))
			if body, err = ioutil.ReadFile(fn); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				return nil, fmt.Errorf("could not read file, line %d: %s", lineNum, line)
			}
		}
	}

	schema := &tempSchema{}
	schema.Method = strings.ToUpper(tokens[0])
	schema.Status, _ = strconv.Atoi(tokens[1])
	schema.Path = sp.cleanPath(tokens[2])
	schema.ContentType = contentType
	schema.Body = body

	return schema, nil
}

func (sp *schemaParser) isHTTPMethod(m string) bool {

	switch strings.ToUpper(m) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func (sp *schemaParser) isHTTPStatusCode(code string) bool {
	var n int
	var err error

	if n, err = strconv.Atoi(code); err != nil {
		return false
	}

	return n >= http.StatusContinue && n <= http.StatusNetworkAuthenticationRequired
}

func (sp *schemaParser) isHTTPPath(p string) bool {
	// just check for leading "/"
	// everything else will be encoded later
	return p[0:1] == "/"
}

func (sp *schemaParser) cleanPath(p string) string {
	items := strings.Split(p, "/")
	uri := []string{}
	for _, i := range items {
		uri = append(uri, url.PathEscape(i))
	}

	return strings.Join(uri, "/")
}
