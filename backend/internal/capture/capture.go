// Package capture is the domain of the capture: what a capture must hold, how it is cleaned and
// checked, and the state of a freshly captured item (RG-01 to RG-10, RG-26). It knows neither
// HTTP nor the generated API code (docs/dev/conventions-backend.md).
package capture

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// Values of the item columns set by a capture (docs/glossary.md).
const (
	KindLink     = "link"
	SourceShare  = "share"
	SourceManual = "manual"
	StatusQueued = "queued"
)

// Errors of a refused capture. None of them carries the captured URL or note: an error message
// may reach a log.
var (
	// ErrEmptyCapture: the capture holds neither a url nor a note (RG-02).
	ErrEmptyCapture = errors.New("capture holds neither a url nor a note")
	// ErrAmbiguousCapture: the capture holds a url and a note, or a note and a capture note (RG-07).
	ErrAmbiguousCapture = errors.New("capture holds a url and a note, or a note and a capture note")
	// ErrInvalidURL: the url is not an absolute http or https URL with a host (RG-02).
	ErrInvalidURL = errors.New("capture url is not an absolute http or https url with a host")
	// ErrNoteNotImplemented: note captures arrive with story E2-03.
	ErrNoteNotImplemented = errors.New("note capture is not implemented yet")
)

// ValidationError is a field of the capture that breaks the contract (client_id, source).
// Reason never holds the value of the field.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("capture %s: %s", e.Field, e.Reason)
}

// Request is a capture as received; a nil field is absent.
type Request struct {
	URL       *string
	Note      *string
	UserNote  *string
	IsPrivate *bool
	Source    *string
	ClientID  *string
}

// Result is the captured item; Created is false when the capture replays an earlier one
// (RG-08).
type Result struct {
	Item    store.Item
	Created bool
}

// Store is the storage the capture needs.
type Store interface {
	CaptureItem(ctx context.Context, userID string, in store.NewItem) (store.Item, bool, error)
}

// Service captures items.
type Service struct {
	store Store
}

// NewService returns a capture service storing into st.
func NewService(st Store) *Service {
	return &Service{store: st}
}

// Capture checks and stores a capture of userID, or returns the item an earlier capture with
// the same client id created (RG-08). userID comes from the authenticated request, never from
// the body (RG-90). Nothing is stored when the capture is refused, and no extraction nor
// analysis runs here (RG-01).
func (s *Service) Capture(ctx context.Context, userID string, req Request) (Result, error) {
	if err := checkClientID(req.ClientID); err != nil {
		return Result{}, err
	}
	source, err := checkSource(req.Source)
	if err != nil {
		return Result{}, err
	}

	// RG-09: trim before any check; a blank capture note is absent. The note of a note
	// capture is left to E2-03.
	rawURL := trim(req.URL)
	userNote := trim(req.UserNote)
	if userNote != nil && *userNote == "" {
		userNote = nil
	}

	// Ambiguity first, so that the answer names the real problem (RG-07).
	if (rawURL != nil && req.Note != nil) || (req.Note != nil && userNote != nil) {
		return Result{}, ErrAmbiguousCapture
	}
	if rawURL == nil && req.Note == nil {
		return Result{}, ErrEmptyCapture
	}
	if rawURL == nil {
		return Result{}, ErrNoteNotImplemented
	}
	title, ok := provisionalTitle(*rawURL)
	if !ok {
		return Result{}, ErrInvalidURL
	}

	in := store.NewItem{
		Kind:      KindLink,
		Source:    source,
		Status:    StatusQueued,
		Title:     title,
		URL:       rawURL,
		UserNote:  userNote,
		ClientID:  req.ClientID,
		IsPrivate: req.IsPrivate != nil && *req.IsPrivate,
	}
	item, created, err := s.store.CaptureItem(ctx, userID, in)
	if err != nil {
		return Result{}, fmt.Errorf("capture: %w", err)
	}
	return Result{Item: item, Created: created}, nil
}

// checkClientID accepts an absent client id or a ULID in its canonical form, the pattern of the
// contract (upper case, 26 characters).
func checkClientID(clientID *string) error {
	if clientID == nil {
		return nil
	}
	id, err := ulid.ParseStrict(*clientID)
	if err != nil || id.String() != *clientID {
		return &ValidationError{Field: "client_id", Reason: "not a ULID"}
	}
	return nil
}

// checkSource returns the source of the capture, share when absent.
func checkSource(source *string) (string, error) {
	if source == nil {
		return SourceShare, nil
	}
	switch *source {
	case SourceShare, SourceManual:
		return *source, nil
	default:
		return "", &ValidationError{Field: "source", Reason: "neither share nor manual"}
	}
}

func trim(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

// provisionalTitle checks that rawURL is an absolute http or https URL with a host (RG-02) and
// returns its host name, in lower case and without port: the title of a link until extraction
// (RG-26). The error of url.Parse is dropped on purpose: it prints the URL.
func provisionalTitle(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	// url.Parse lower-cases the scheme. An opaque URL (https:example.com) has no host.
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", false
	}
	host := u.Hostname()
	if host == "" {
		return "", false
	}
	return strings.ToLower(host), true
}
