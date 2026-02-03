package data

import (
	"bufio"
	"bytes"
	"log"
	"math/rand/v2"
	"net/url"
	"regexp"
	"sync"
	"text/template"
	"time"

	"github.com/jaswdr/faker"
)

var (
	dollarReplacerRegex = regexp.MustCompile(`{{\s*\$([a-zA-Z_]\w*)\s*}}`)
	replacerRegex       = regexp.MustCompile(`\{\{[^}]+}}`)
	funcMapOnce         sync.Once
	funcMap             template.FuncMap
)

// createFuncMap initializes and returns a map of template functions for data generation.
func createFuncMap() template.FuncMap {
	funcMapOnce.Do(func() {
		f := faker.New()

		funcMap = template.FuncMap{
			"name":         f.Person().Name,
			"firstName":    f.Person().FirstName,
			"lastName":     f.Person().LastName,
			"email":        f.Internet().Email,
			"user":         f.Internet().User,
			"url":          f.Internet().URL,
			"server":       f.Internet().Domain,
			"hash":         f.Hash().MD5,
			"phone":        f.Phone().Number,
			"bool":         f.Boolean().Bool,
			"uuid":         f.UUID().V4,
			"guid":         f.UUID().V4,
			"timestamp":    func() int64 { return f.Time().Unix(time.Now()) },
			"isoTimestamp": func() string { return f.Time().ISO8601(time.Now()) },
			"integer":      f.UInt16,
			"float":        func() float32 { return f.Float32(2, 0, 100_000) },
			"file":         func() string { return f.File().AbsoluteFilePath(3 + rand.IntN(4)) },
			"sentence":     func() string { return f.Lorem().Sentence(8 + rand.IntN(9)) },
			"paragraph":    func() string { return f.Lorem().Paragraph(3 + rand.IntN(2)) },
			"article":      func() string { return f.Lorem().Paragraph(5 + rand.IntN(3)) },
		}
	})

	return funcMap
}

// substitute replaces placeholders in the input body with values from the given URL parameters
// and generates dynamic values using predefined functions.
func substitute(values url.Values, body []byte) []byte {
	body = dollarReplacerRegex.ReplaceAll(body, []byte("{{${1}}}"))

	// map URL parameter placeholders
	paramMap := make(map[string]string)
	for name, value := range values {
		paramMap["{{"+name+"}}"] = value[rand.IntN(len(value))]
	}

	// replace parameters in the body
	body = replacerRegex.ReplaceAllFunc(body, func(k []byte) []byte {
		if val, ok := paramMap[string(k)]; ok {
			return []byte(val)
		}
		return k
	})

	// replace global parameters and functions
	tmpl, err := template.New("substitution").Funcs(createFuncMap()).Parse(string(body))
	if err != nil {
		log.Printf("template parsing error: %s", err)
		return body
	}

	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	if err := tmpl.Execute(w, nil); err != nil {
		log.Printf("template execution error: %s", err)
		return body
	}

	if err := w.Flush(); err != nil {
		log.Printf("failed to flush template output: %v", err)
		return body
	}
	return b.Bytes()
}
