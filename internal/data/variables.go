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

	"github.com/jaswdr/faker"
)

var funcMap template.FuncMap

func init() {
	// First we create a FuncMap with which to register the function.
	fake := faker.New()

	fakeFile := func() func() string {
		return func() string {
			return fake.File().AbsoluteFilePath(3 + rand.IntN(4))
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
			return fake.Lorem().Sentence(8 + rand.IntN(9))
		}
	}

	fakeParagraph := func() func() string {
		return func() string {
			s1 := fake.Lorem().Sentence(8 + rand.IntN(9))
			s2 := fake.Lorem().Sentence(8 + rand.IntN(9))
			s3 := fake.Lorem().Sentence(8 + rand.IntN(9))
			return fmt.Sprintf("%s %s %s", s1, s2, s3)
		}
	}

	fakeArticle := func() func() string {
		return func() string {
			p1 := fake.Lorem().Paragraph(2 + rand.IntN(3))
			p2 := fake.Lorem().Paragraph(2 + rand.IntN(3))
			p3 := fake.Lorem().Paragraph(2 + rand.IntN(3))
			return fmt.Sprintf("%s\n\n%s\n\n%s\n", p1, p2, p3)
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
		"uuid":         fake.UUID().V4,
		"guid":         fake.UUID().V4,
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

func substitute(values url.Values, body []byte) []byte {

	// convert {{$var}} to {{var}}
	body = dollarReplacerRegex.ReplaceAll(body, []byte("{{${1}}}"))

	// randomly select amongst multiple values
	paramMap := make(map[string]string)
	for name, value := range values {
		paramMap["{{"+name+"}}"] = value[rand.IntN(len(value))]
	}

	// substitute params
	body = replacerRegex.ReplaceAllFunc(body, func(k []byte) []byte {
		if val, ok := paramMap[string(k)]; ok {
			return []byte(val)
		}

		return k
	})

	// substitute {{global paramMap}} and {{funcs}}
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
