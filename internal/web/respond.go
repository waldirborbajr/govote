// Package web provides shared HTTP utilities: JSON responders, rate limiting,
// HTML templates and admin session resolution used by all handler packages.
package web

import (
	"encoding/json"
	"log"
	"net/http"
)

// RespondJSON writes data as a JSON response with the given status code.
func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("web: failed to encode JSON response: %v", err)
		}
	}
}

// RespondError writes a JSON error payload with the given status code.
func RespondError(w http.ResponseWriter, status int, message string) {
	RespondJSON(w, status, map[string]string{"error": message})
}
