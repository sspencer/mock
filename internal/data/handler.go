package data

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Handle returns a HTTP handler method for the given endpoint.
func (e *Endpoint) Handle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		queryParams := r.URL.Query()

		// first look at GET params for a match
		for key, values := range queryParams {
			for _, value := range values {
				vk := getVarKey(key, value)
				if m, ok := e.varmap[vk]; ok {
					e.writeResponse(w, r, e.Path, m)
					return
				}
			}
		}

		// otherwise, cycle through the responses
		e.writeResponse(w, r, e.Path, e.getNextResponse())
	}
}

func (e *Endpoint) getNextResponse() mockResponse {
	e.RLock()
	index := e.index
	e.RUnlock()

	if index >= len(e.responses) {
		index = 0
	}

	resp := e.responses[index]

	e.Lock()
	e.index = index + 1
	e.Unlock()

	return resp
}

func (e *Endpoint) writeResponse(w http.ResponseWriter, r *http.Request, path string, resp mockResponse) {
	for n, v := range resp.header {
		w.Header().Add(n, v)
	}
	w.WriteHeader(resp.status)

	if resp.delay > 0 {
		time.Sleep(resp.delay)
	}

	// replace {{params}} and {{variables}} in body
	items := strings.Split(path, "/")
	values := make(url.Values)
	for _, item := range items {
		if strings.HasPrefix(item, "{") && strings.HasSuffix(item, "}") {
			// Remove curly braces and print the item
			key := item[1 : len(item)-1]
			value := chi.URLParam(r, key)
			values[key] = []string{value}
		}
	}

	out := substituteVars(replacerRegex.ReplaceAllFunc(resp.body, substituteParams(values)))
	w.Write(out)
}
