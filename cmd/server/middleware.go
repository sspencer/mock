package main

import (
	"bytes"
	"fmt"
	"github.com/julienschmidt/httprouter"
	"io"
	"log"
	"net/http"
	"strings"
)

func requestLogger(next httprouter.Handle) httprouter.Handle {
	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		var request []string
		url := fmt.Sprintf("%v %v %v", r.Method, r.URL, r.Proto)
		request = append(request, url)
		request = append(request, fmt.Sprintf("Host: %v", r.Host))

		// Loop through headers
		for name, headers := range r.Header {
			name = strings.ToLower(name)
			for _, h := range headers {
				request = append(request, fmt.Sprintf("%v: %v", name, h))
			}
		}

		request = append(request, "\n")

		buf, _ := io.ReadAll(r.Body)
		rdr1 := io.NopCloser(bytes.NewBuffer(buf))
		rdr2 := io.NopCloser(bytes.NewBuffer(buf)) // create a second Buffer, since rdr1 will be read

		// copy body from rdr1 to buffer
		bb := new(bytes.Buffer)
		bb.ReadFrom(rdr1)

		request = append(request, bb.String()) // append request body

		// log request/headers/body
		msg := strings.TrimSpace(strings.Join(request, "\n"))
		log.Printf("REQUEST:\n----\n%s\n----\n", msg)

		// set body to unread buffer
		r.Body = rdr2

		next(w, r, p)
	}
}
