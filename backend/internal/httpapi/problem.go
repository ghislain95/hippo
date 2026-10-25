package httpapi

import (
	"encoding/json"
	"net/http"
)

// Problem types (RFC 9457). Business errors use urn:hippo:problem:<slug> (api/openapi.yaml).
const (
	problemTypeBlank = "about:blank"
)

// writeProblem writes an application/problem+json response. detail is in French and can be
// shown as is by the app; it never carries an internal error message.
func writeProblem(w http.ResponseWriter, r *http.Request, status int, problemType, title, detail string) {
	body := Problem{
		Type:   problemType,
		Title:  title,
		Status: status,
	}
	if detail != "" {
		body.Detail = &detail
	}
	if r != nil {
		instance := r.URL.Path
		body.Instance = &instance
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) // the status is sent: nothing left to report
}
