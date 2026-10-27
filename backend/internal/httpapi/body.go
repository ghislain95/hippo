package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// maxJSONBody bounds every application/json request body (ADR-0019). Multipart bodies are left
// to their operation: the image capture owns the 20 MB rule (RG-02, E2-04).
const maxJSONBody = 1 << 20

// strictBody returns an empty value of the JSON body declared for the operation pattern
// (http.Request.Pattern), or false when the operation has no strict body yet. Each story that
// implements an operation with a JSON body adds it here: the generated decoder alone ignores
// undeclared members and data after the value.
func strictBody(pattern string) (any, bool) {
	switch pattern {
	case "POST /api/items":
		return &CaptureRequest{}, true
	default:
		return nil, false
	}
}

// isJSON tells whether the generated decoder reads the body as JSON (same test as api.gen.go).
func isJSON(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
}

// strictJSONBodies bounds every JSON body to maxJSONBody and, for the operations of
// strictBody, refuses a body with an undeclared member or data after the value. It runs inside
// the router, where r.Pattern is the matched operation; the body is then restored for the
// generated decoder. A body over the limit answers 413, here or in the generated decoder
// (RequestErrorHandlerFunc).
func strictJSONBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isJSON(r) {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
		target, ok := strictBody(r.Pattern)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeBodyError(w, r, err)
			return
		}
		if err := decodeStrict(raw, target); err != nil {
			writeBodyError(w, r, err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		next.ServeHTTP(w, r)
	})
}

// errTrailingData reports data after the JSON value of a body.
var errTrailingData = errors.New("data after the JSON value")

// decodeStrict decodes raw into target, refusing undeclared members and anything but
// whitespace after the value. An empty body fails (io.EOF).
func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errTrailingData
	}
	return nil
}

// writeBodyError answers 413 for a body over the limit, 400 otherwise. The error itself is
// never written nor logged: it may quote the body.
func writeBodyError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, problemTypeBlank, "Payload too large", "Le corps de la requête dépasse 1 Mio.")
		return
	}
	writeProblem(w, r, http.StatusBadRequest, problemTypeBlank, "Bad request", "La requête est invalide.")
}
