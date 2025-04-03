package main

import (
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

type httpRequestLog struct {
	Header  http.Header `json:"header"`
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Details string      `json:"details"`
	Body    string      `json:"body"`
}

type httpResponseLog struct {
	Header     http.Header `json:"header"`
	Status     int         `json:"status"`
	StatusText string      `json:"statusText"`
	Time       string      `json:"time"`
	Details    string      `json:"details"`
	Body       string      `json:"body"`
}

type httpLog struct {
	Request  httpRequestLog  `json:"request"`
	Response httpResponseLog `json:"response"`
}

type loggerFunc func(log httpLog)

func newLogger() loggerFunc {
	if isatty.IsTerminal(os.Stdout.Fd()) {
		return colorlog()
	}

	return monolog()
}

func monolog() loggerFunc {
	return func(r httpLog) {
		log.Printf(" %3d | %-7s %s\n",
			r.Response.Status,
			r.Request.Method,
			r.Request.URL)
	}
}

func colorlog() loggerFunc {
	return func(r httpLog) {
		statusColor := colorForStatus(r.Response.Status)
		methodColor := colorForMethod(r.Request.Method)
		resetColor := reset
		log.Printf("%s %3d %s %s %-7s %s %s\n",
			statusColor, r.Response.Status, resetColor,
			methodColor, r.Request.Method, resetColor,
			r.Request.URL,
		)
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
