package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ghislain95/hippo/backend/internal/assetfs"
	"github.com/ghislain95/hippo/backend/internal/capture"
)

// maxJSONBody bounds every application/json request body (ADR-0019). Multipart bodies are
// bounded by boundTransfers.
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

// Bounds of a multipart image capture (RG-02). The body allows the 20 MB image, its text
// fields and the part headers; each text field is read up to maxFormField.
const (
	maxMultipartBody = 21 << 20
	maxFormField     = 1 << 20
)

// transferTimeout is the time an image upload or download has, beyond the server's global
// timeouts of 60 s: a 20 MB image on a slow mobile link.
const transferTimeout = 5 * time.Minute

// Route patterns (http.Request.Pattern) of the operations that move image files.
const (
	createItemPattern = "POST /api/items"
	getAssetPattern   = "GET /api/assets/{asset_id}"
)

// isMultipart tells whether the generated code reads the body as multipart (same test as
// api.gen.go).
func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
}

// boundTransfers bounds the multipart body of POST /api/items to maxMultipartBody (a declared
// Content-Length over it answers 413 at once) and gives the upload transferTimeout to be read
// and answered, and gives GET /api/assets/{asset_id} transferTimeout to be written. net/http
// sets the write deadline when the request headers are read, so an upload needs both. It runs
// inside the router, where r.Pattern is the matched operation.
func boundTransfers(logger *slog.Logger) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Pattern == createItemPattern && isMultipart(r):
				if r.ContentLength > maxMultipartBody {
					writeMultipartTooLarge(w, r)
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBody)
				extendDeadlines(w, r, logger, true)
			case r.Pattern == getAssetPattern:
				extendDeadlines(w, r, logger, false)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extendDeadlines moves the write deadline, and the read deadline when read is true, to
// transferTimeout from now. A writer that cannot (http.ErrNotSupported) keeps the server's
// timeouts; this is logged, not refused.
func extendDeadlines(w http.ResponseWriter, r *http.Request, logger *slog.Logger, read bool) {
	controller := http.NewResponseController(w)
	deadline := time.Now().Add(transferTimeout)
	if read {
		if err := controller.SetReadDeadline(deadline); err != nil {
			logger.WarnContext(r.Context(), "extend read deadline", slog.String("error", err.Error()))
		}
	}
	if err := controller.SetWriteDeadline(deadline); err != nil {
		logger.WarnContext(r.Context(), "extend write deadline", slog.String("error", err.Error()))
	}
}

func writeMultipartTooLarge(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusRequestEntityTooLarge, problemTypeBlank, "Payload too large", multipartTooLargeDetail)
}

const multipartTooLargeDetail = "Le corps de la requête dépasse 21 Mio."

// formFault is why a multipart image capture is refused before the domain sees it, in the
// order of precedence of the contract.
type formFault int

const (
	faultBodyTooLarge  formFault = iota + 1 // 413 about:blank
	faultImageTooLarge                      // 413 image-too-large
	faultStructure                          // 400 about:blank
)

// formError is a refused multipart body. kind names a read failure for the logs ("" when the
// body was read to its end); it never holds the error text, which may quote a part.
type formError struct {
	fault formFault
	kind  string
}

func (e *formError) Error() string {
	return fmt.Sprintf("multipart capture refused (fault %d, %s)", e.fault, e.kind)
}

// imageForm is a multipart image capture read to its end: the image file, written to a
// temporary file of the assets directory, and the declared text fields.
type imageForm struct {
	image  *assetfs.Upload
	fields map[string]string
}

// formFields are the text fields an image capture may hold (CaptureImageRequest).
var formFields = map[string]bool{"user_note": true, "is_private": true, "client_id": true, "source": true}

// readImageForm reads every part of an image capture, whatever their order, then tells the
// first fault in the order of the contract: body over the limit, image over 20 MB, structure
// (undeclared or repeated field, second file, missing image, text field over 1 MiB or not
// UTF-8). The image is copied to a temporary file as it arrives, never more than 20 MB + 1
// bytes; the caller removes it (form.image.Remove) on every path. A *formError is a refusal;
// any other error is a failure of the server (the disk).
func readImageForm(reader *multipart.Reader, assets *assetfs.Dir) (imageForm, error) {
	form := imageForm{fields: map[string]string{}}
	imageSeen, imageTooLarge, structure := false, false, false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return form, readFault(err)
		}
		name := part.FormName()
		switch {
		case name == "image":
			if imageSeen {
				structure = true // a second file is not stored; NextPart skips it
				continue
			}
			imageSeen = true
			if form.image, err = assets.CreateTemp(); err != nil {
				return form, fmt.Errorf("read image capture: %w", err)
			}
			source := &trackedReader{reader: io.LimitReader(part, capture.MaxImageSize+1)}
			if _, err := io.Copy(form.image, source); err != nil {
				if source.err != nil {
					return form, readFault(source.err)
				}
				return form, fmt.Errorf("read image capture: %w", err)
			}
			if form.image.Size() > capture.MaxImageSize {
				// The rest of the part is skipped by NextPart, within the body limit.
				imageTooLarge = true
				if err := form.image.Remove(); err != nil {
					return form, fmt.Errorf("read image capture: %w", err)
				}
			}
		case formFields[name] && part.FileName() == "":
			value, err := io.ReadAll(io.LimitReader(part, maxFormField+1))
			if err != nil {
				return form, readFault(err)
			}
			_, repeated := form.fields[name]
			if repeated || len(value) > maxFormField || !utf8.Valid(value) {
				structure = true
				continue
			}
			form.fields[name] = string(value)
		default:
			structure = true // undeclared field (url and note included), or a file in a text field
		}
	}
	switch {
	case imageTooLarge:
		return form, &formError{fault: faultImageTooLarge}
	case structure || !imageSeen:
		return form, &formError{fault: faultStructure}
	}
	return form, nil
}

// readFault is the refusal of a body whose reading failed: over the limit, or else malformed,
// cut by the client or by the read deadline.
func readFault(err error) *formError {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return &formError{fault: faultBodyTooLarge, kind: "body too large"}
	case errors.Is(err, os.ErrDeadlineExceeded):
		return &formError{fault: faultStructure, kind: "deadline exceeded"}
	case errors.Is(err, io.ErrUnexpectedEOF):
		return &formError{fault: faultStructure, kind: "truncated"}
	default:
		return &formError{fault: faultStructure, kind: "malformed"}
	}
}

// trackedReader remembers the error of its reader, so that a failed copy tells a read failure
// (the client) from a write failure (the disk).
type trackedReader struct {
	reader io.Reader
	err    error
}

func (r *trackedReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		r.err = err
	}
	return n, err // unwrapped: io.Copy tests for io.EOF itself
}
