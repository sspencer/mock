package data

import (
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

var (
	dollarReplacerRegex = regexp.MustCompile(`{{\s*\$([a-zA-Z_]\w*)\s*}}`)
)

// Handle returns a HTTP handler method for the given endpoint.
func (e *Endpoint) Handle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		queryParams := r.URL.Query()

		// first look at GET params for a match
		for key, values := range queryParams {
			value := values[rand.IntN(len(values))]
			getVar := getVarKey(key, value)
			if m, ok := e.localVars[getVar]; ok {
				e.writeHTTPResponse(w, r, e.Path, m, getVar)
				return
			}
		}

		// otherwise, cycle through the responses
		e.writeHTTPResponse(w, r, e.Path, e.getNextResponse(), "")
	}
}

func (e *Endpoint) getNextResponse() mockResponse {
	e.Lock()
	defer e.Unlock()

	index := e.index % len(e.responses)
	response := e.responses[index]
	e.index++

	return response
}

func (e *Endpoint) writeHTTPResponse(w http.ResponseWriter, r *http.Request, path string, resp mockResponse, getVar string) {
	if resp.delay > 0 {
		time.Sleep(resp.delay)
	}

	// replace {{params}} and {{variables}} in body
	subVars := make(url.Values)

	// 1. add global localVars
	for name, val := range e.globalVars {
		subVars[name] = []string{val}
	}

	// 2. add localVars from path
	items := strings.Split(path, "/")
	for _, item := range items {
		if strings.HasPrefix(item, "{") && strings.HasSuffix(item, "}") {
			// Remove curly braces and print the item
			key := item[1 : len(item)-1]
			value := chi.URLParam(r, key)
			subVars[key] = []string{value}
		}
	}

	// 3. add endpoint's delay value
	subVars["delay"] = []string{resp.delay.String()}

	// 4. add HTTP GET params as substitution variables
	for k, v := range r.URL.Query() {
		arr := strings.SplitN(getVar, "=", 2)

		// if response was chosen based on GET param, add that
		// param as var instead of potential array of choices
		if len(arr) == 2 && arr[0] == k {
			subVars[k] = []string{arr[1]}
		} else {
			subVars[k] = v
		}
	}

	// replace Header values with global variables
	for hdrName, hdrValue := range resp.header {
		w.Header().Add(hdrName, string(substitute(subVars, []byte(hdrValue))))
	}

	w.WriteHeader(resp.status)

	_, err := w.Write(substitute(subVars, resp.body))

	if err != nil {
		log.Println(err.Error())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
