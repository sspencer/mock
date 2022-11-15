package colorlog

import (
	"github.com/mattn/go-isatty"
	"log"
	"net/http"
	"os"
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

type ResponseLoggerFunc func(int, *http.Request)

func NewResponseLoggerFunc() ResponseLoggerFunc {
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
