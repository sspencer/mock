package colorlog

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"

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

type HTTPLog struct {
	Status          int         `json:"status"`
	StatusText      string      `json:"statusText"`
	Time            string      `json:"time"`
	Method          string      `json:"method"`
	Uri             string      `json:"uri"`
	RequestDetails  string      `json:"requestDetails"`
	RequestBody     string      `json:"requestBody"`
	ResponseHeader  http.Header `json:"responseHeader"`
	ResponseBody    string      `json:"responseBody"`
	ResponseDetails string      `json:"responseDetails"`
}

type LoggerFunc func(log HTTPLog)

func New(displayRequestBody, displayResponse bool) LoggerFunc {
	if isatty.IsTerminal(os.Stdout.Fd()) {
		return colorlog(displayRequestBody, displayResponse)
	}

	return monolog(displayRequestBody, displayResponse)
}

func logResponse(r HTTPLog) string {
	var buffer bytes.Buffer

	hdrs := 0
	for k, v := range r.ResponseHeader {
		hdrs++
		val := ""
		if len(v) > 0 {
			val = v[0]
		}
		buffer.WriteString(fmt.Sprintf("%s: %s\n", k, val))
	}

	if hdrs > 0 {
		buffer.WriteString("\n")
	}

	buffer.WriteString(r.ResponseBody)
	return buffer.String()
}

func monolog(displayRequestBody, displayResponse bool) LoggerFunc {
	return func(r HTTPLog) {
		if displayRequestBody {
			log.Printf(" %3d | %-7s %s\n%s\n",
				r.Status,
				r.Method,
				r.Uri,
				r.RequestDetails)
		} else {
			log.Printf(" %3d | %-7s %s\n%s\n",
				r.Status,
				r.Method,
				r.Uri,
				r.RequestDetails)
		}

		if displayResponse {
			response := logResponse(r)
			fmt.Printf("ResponseBody:\n%s", response)
		}
	}
}

func colorlog(displayRequestBody, displayResponse bool) LoggerFunc {
	return func(r HTTPLog) {
		statusColor := colorForStatus(r.Status)
		methodColor := colorForMethod(r.Method)
		resetColor := reset
		if displayRequestBody {
			log.Printf("%s %3d %s %s %-7s %s %s\n%s\n",
				statusColor, r.Status, resetColor,
				methodColor, r.Method, resetColor,
				r.Uri,
				r.RequestDetails,
			)
		} else {
			log.Printf("%s %3d %s %s %-7s %s %s\n",
				statusColor, r.Status, resetColor,
				methodColor, r.Method, resetColor,
				r.Uri,
			)
		}

		if displayResponse {
			response := logResponse(r)
			log.Printf("%s ResponseBody: %s\n%s\n", blue, resetColor, response)
		}
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
