package main

import (
	"fmt"
	"net/http"
)

func methodNotFound(w http.ResponseWriter, _ *http.Request) {
	message := "The requested resource could not be found"
	http.Error(w, message, http.StatusNotFound)
}

func methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	message := fmt.Sprintf("The %s method is not supported for this resource", r.Method)
	http.Error(w, message, http.StatusMethodNotAllowed)
}
