package main

import (
	"log"
	"math/rand/v2"
	"os"
	"sync"
	"text/template"
	"time"

	"github.com/jaswdr/faker"
)

var (
	funcMapOnce sync.Once
	funcMap     template.FuncMap
)

// createFuncMap initializes and returns a map of template functions for data generation.
func createFuncMap() template.FuncMap {
	funcMapOnce.Do(func() {
		fake := faker.New()

		funcMap = template.FuncMap{
			"name":         fake.Person().Name,
			"firstName":    fake.Person().FirstName,
			"lastName":     fake.Person().LastName,
			"email":        fake.Internet().Email,
			"user":         fake.Internet().User,
			"url":          fake.Internet().URL,
			"server":       fake.Internet().Domain,
			"hash":         fake.Hash().MD5,
			"phone":        fake.Phone().Number,
			"bool":         fake.Boolean().Bool,
			"uuid":         fake.UUID().V4,
			"guid":         fake.UUID().V4,
			"timestamp":    func() int64 { return fake.Time().Unix(time.Now()) },
			"isoTimestamp": func() string { return fake.Time().ISO8601(time.Now()) },
			"integer":      fake.UInt16,
			"float":        func() float32 { return fake.Float32(2, 0, 100_000) },
			"file":         func() string { return fake.File().AbsoluteFilePath(3 + rand.IntN(4)) },
			"sentence":     func() string { return fake.Lorem().Sentence(8 + rand.IntN(9)) },
			"paragraph":    func() string { return fake.Lorem().Paragraph(3 + rand.IntN(2)) },
			"article":      func() string { return fake.Lorem().Paragraph(5 + rand.IntN(3)) },
		}
	})

	return funcMap
}

// Testbed for adding variables available for response {{substitution}}.
func main() {

	const templateText = `
UUID:         {{uuid}}
GUID:         {{guid}}
First:        {{firstName}}
Last:         {{lastName}}
Email:        {{email}}
User:         {{user}}
URL:          {{url}}
Host:         {{server}}
Bool:         {{bool}}
Integer:      {{integer}}
Float:        {{float}}
File:         {{file}}
Hash:         {{hash}}
Phone:        {{phone}}
Timestamp:    {{timestamp}}
ISOTimestamp: {{isoTimestamp}}
Sentence:     {{sentence}}
Paragraph:    {{paragraph}}
Article:      {{article}}
Input:        {{printf "%q" .}}
`

	tmpl, err := template.New("titleTest").Funcs(createFuncMap()).Parse(templateText)
	if err != nil {
		log.Fatalf("parsing: %s", err)
	}

	// Run the template to verify the output.
	err = tmpl.Execute(os.Stdout, "the go programming language")
	if err != nil {
		log.Fatalf("execution: %s", err)
	}
}
