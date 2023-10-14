package colorlog

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

type HTTPLog struct {
	Status int
	Method string
	Uri    string
	Body   string
}

type LoggerFunc func(log HTTPLog)

func New(displayBody bool) LoggerFunc {
	if isatty.IsTerminal(os.Stdout.Fd()) {
		return colorlog(displayBody)
	}

	return monolog(displayBody)
}

func monolog(displayBody bool) LoggerFunc {
	return func(r HTTPLog) {
		if displayBody {
			log.Printf(" %3d | %-7s %s\n%s\n",
				r.Status,
				r.Method,
				r.Uri,
				r.Body)
		} else {
			log.Printf(" %3d | %-7s %s\n%s\n",
				r.Status,
				r.Method,
				r.Uri,
				r.Body)
		}
	}
}

func colorlog(displayBody bool) LoggerFunc {
	return func(r HTTPLog) {
		statusColor := colorForStatus(r.Status)
		methodColor := colorForMethod(r.Method)
		resetColor := reset
		if displayBody {
			log.Printf("%s %3d %s %s %-7s %s %s\n%s\n",
				statusColor, r.Status, resetColor,
				methodColor, r.Method, resetColor,
				r.Uri,
				r.Body,
			)
		} else {
			log.Printf("%s %3d %s %s %-7s %s %s\n",
				statusColor, r.Status, resetColor,
				methodColor, r.Method, resetColor,
				r.Uri,
			)
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
