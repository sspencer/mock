package mock

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/mattn/go-isatty"
)

var (
	green   = string([]byte{27, 91, 57, 55, 59, 52, 50, 109})
	white   = string([]byte{27, 91, 57, 48, 59, 52, 55, 109})
	yellow  = string([]byte{27, 91, 57, 48, 59, 52, 51, 109})
	red     = string([]byte{27, 91, 57, 55, 59, 52, 49, 109})
	blue    = string([]byte{27, 91, 57, 55, 59, 52, 52, 109})
	magenta = string([]byte{27, 91, 57, 55, 59, 52, 53, 109})
	cyan    = string([]byte{27, 91, 57, 55, 59, 52, 54, 109})
	reset   = string([]byte{27, 91, 48, 109})
)

type responseLogger func(int, *http.Request)

func newResponseLogger() responseLogger {
	disableColor := true

	if isatty.IsTerminal(os.Stdout.Fd()) {
		disableColor = false
	}

	return func(statusCode int, r *http.Request) {
		var statusColor, methodColor, resetColor string
		if !disableColor {
			statusColor = colorForStatus(statusCode)
			methodColor = colorForMethod(r.Method)
			resetColor = reset
		}

		log.Printf("%s %3d %s| %s %-7s %s %s\n",
			statusColor, statusCode, resetColor,
			methodColor, r.Method, resetColor,
			r.URL.Path,
		)
	}
}

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

func colorForStatus(code int) string {
	switch {
	case code >= http.StatusOK && code < http.StatusMultipleChoices:
		return green
	case code >= http.StatusMultipleChoices && code < http.StatusBadRequest:
		return white
	case code >= http.StatusBadRequest && code < http.StatusInternalServerError:
		return red
	default:
		return red
	}
}

func colorForMethod(method string) string {
	switch method {
	case http.MethodGet:
		return blue
	case http.MethodPost:
		return cyan
	case http.MethodPut:
		return yellow
	case http.MethodDelete:
		return red
	case http.MethodPatch:
		return green
	case http.MethodHead:
		return magenta
	case http.MethodOptions:
		return white
	default:
		return reset
	}
}
