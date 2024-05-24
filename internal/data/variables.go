package data

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"math/rand/v2"
	"net/url"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/jaswdr/faker"
)

var funcMap template.FuncMap

func init() {
	// First we create a FuncMap with which to register the function.
	fake := faker.New()

	fakeFile := func() func() string {
		return func() string {
			return fake.File().AbsoluteFilePath(5)
		}
	}

	fakeFloat := func() func() float32 {
		return func() float32 {
			return fake.Float32(2, 0, 100_000)
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
	funcMap = template.FuncMap{
		// The name "title" is what the function will be called in the template text.
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
}

func substituteParams(values url.Values) func([]byte) []byte {
	vars := make(map[string]string)
	for name, value := range values {
		k := fmt.Sprintf("{{%s}}", name)
		vars[k] = value[rand.IntN(len(value))]
	}

	return func(k []byte) []byte {
		if val, ok := vars[string(k)]; ok {
			return []byte(val)
		}

		return k
	}
}

func substituteVars(body []byte) []byte {
	tmpl, err := template.New("test").Funcs(funcMap).Parse(string(body))
	if err != nil {
		log.Printf("substitute var parsing: %s", err)
		return body
	}

	var b bytes.Buffer
	w := bufio.NewWriter(&b)

	err = tmpl.Execute(w, "")
	if err != nil {
		log.Printf("substitute var execution: %s", err)
		return body
	}
	w.Flush()
	return b.Bytes()
}

// convert {{$var}} to {{var}}
func replaceVarDollars(body []byte) []byte {
	return dollarReplacerRegex.ReplaceAll(body, []byte("{{${1}}}"))
}
