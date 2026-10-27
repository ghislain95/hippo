package httpapi

import (
	"encoding/json"
	"net/http"
)

// Problem types (RFC 9457). Business errors use urn:hippo:problem:<slug> (api/openapi.yaml).
const (
	problemTypeBlank            = "about:blank"
	problemTypeNotImplemented   = "urn:hippo:problem:not-implemented"
	problemTypeInvalidURL       = "urn:hippo:problem:invalid-url"
	problemTypeEmptyCapture     = "urn:hippo:problem:empty-capture"
	problemTypeAmbiguousCapture = "urn:hippo:problem:ambiguous-capture"
	problemTypeEmptyNote        = "urn:hippo:problem:empty-note"
)

// newProblem returns a problem document for a typed response of the strict server.
func newProblem(status int, problemType, title, detail, instance string) Problem {
	return Problem{Type: problemType, Title: title, Status: status, Detail: &detail, Instance: &instance}
}

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
