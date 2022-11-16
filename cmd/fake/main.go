package main

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/jaswdr/faker"
	"log"
	"os"
	"text/template"
	"time"
)

// Testbed for adding variables available for response {{substitution}}.
func main() {
	// First we create a FuncMap with which to register the function.
	fake := faker.New()

	fakeFile := func() func() string {
		return func() string {
			return fake.File().AbsoluteFilePath(5)
		}
	}

	fakeFloat := func() func() float32 {
		return func() float32 {
			return fake.Float32(2, -10_000, 10_000)
		}
	}

	fakeTimestamp := func() func() int64 {
		return func() int64 {
			return fake.Time().Unix(time.Now())
		}
	}
	fakeISOTimestamp := func() func() string {
		return func() string {
			return fake.Time().ISO8601(time.Now())
		}
	}

	fakeSentence := func() func() string {
		return func() string {
			return fake.Lorem().Sentence(14)
		}
	}

	fakeParagraph := func() func() string {
		return func() string {
			s1 := fake.Lorem().Sentence(12)
			s2 := fake.Lorem().Sentence(14)
			s3 := fake.Lorem().Sentence(10)
			return fmt.Sprintf("%s %s %s", s1, s2, s3)
		}
	}

	fakeArticle := func() func() string {
		return func() string {
			p1 := fake.Lorem().Paragraph(2)
			p2 := fake.Lorem().Paragraph(3)
			p3 := fake.Lorem().Paragraph(2)
			return fmt.Sprintf("%s\n\n%s\n\n%s\n", p1, p2, p3)
		}
	}

	fakeUUID := func() func() string {
		return func() string {
			return uuid.New().String()
		}
	}

	// https://pkg.go.dev/github.com/jaswdr/faker
	funcMap := template.FuncMap{
		// The name "title" is what the function will be called in the template text.
		"firstName":    fake.Person().FirstName,
		"lastName":     fake.Person().LastName,
		"email":        fake.Internet().Email,
		"user":         fake.Internet().User,
		"url":          fake.Internet().URL,
		"server":       fake.Internet().Domain,
		"hash":         fake.Hash().MD5,
		"phone":        fake.Phone().Number,
		"bool":         fake.Boolean().Bool,
		"uuid":         fakeUUID(),
		"timestamp":    fakeTimestamp(),
		"isoTimestamp": fakeISOTimestamp(),
		"integer":      fake.UInt16,
		"float":        fakeFloat(),
		"file":         fakeFile(),
		"sentence":     fakeSentence(),
		"paragraph":    fakeParagraph(),
		"article":      fakeArticle(),
	}

	const templateText = `
UUID: {{uuid}}
UUID: {{uuid}}
First: {{firstName}}
Last: {{lastName}}
Email: {{email}}
User: {{user}}
URL: {{url}}
Host: {{server}}
Bool: {{bool}}
Integer: {{integer}}
Float: {{float}}
File: {{file}}
Hash: {{hash}}
Phone: {{phone}}
UUID: {{uuid}}
Timestamp: {{timestamp}}
ISOTimestamp: {{isoTimestamp}}
Sentence: {{sentence}}
Paragraph: {{paragraph}}
Article: {{article}}
Input: {{printf "%q" .}}
`

	tmpl, err := template.New("titleTest").Funcs(funcMap).Parse(templateText)
	if err != nil {
		log.Fatalf("parsing: %s", err)
	}

	// Run the template to verify the output.
	err = tmpl.Execute(os.Stdout, "the go programming language")
	if err != nil {
		log.Fatalf("execution: %s", err)
	}
}
