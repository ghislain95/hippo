// Package capture is the domain of the capture: what a capture must hold, how it is cleaned and
// checked, the state of a freshly captured item, what a duplicate link does to the item it
// matches, and how an image and its file are stored (RG-01 to RG-10, RG-15, RG-26). It knows
// neither HTTP nor the generated API code (docs/dev/conventions-backend.md).
package capture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// Values of the item columns set by a capture (docs/glossary.md).
const (
	KindLink     = "link"
	KindNote     = "note"
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
	// ErrEmptyNote: the text of a note capture is empty after trimming (RG-02, RG-09).
	ErrEmptyNote = errors.New("capture note text is empty")
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
// (RG-08) or is the duplicate of a stored link (RG-04).
type Result struct {
	Item    store.Item
	Created bool
}

// Store is the storage the capture needs.
type Store interface {
	CaptureItem(ctx context.Context, userID string, in store.NewItem, merge store.NoteMerge) (store.Item, bool, error)
	// NewID returns a new ULID, for an asset whose file is named before its transaction.
	NewID() (string, error)
}

// Service captures items.
type Service struct {
	store  Store
	paris  *time.Location
	logger *slog.Logger
}

// NewService returns a capture service storing into st. paris is the time zone of the dates
// written in a note (RG-04), from LoadParis. logger receives the failures that do not change
// the answer (a file left after a replayed image); it never receives content.
func NewService(st Store, paris *time.Location, logger *slog.Logger) *Service {
	return &Service{store: st, paris: paris, logger: logger}
}

// Capture checks and stores a capture of userID. It returns the item an earlier capture with
// the same client id resolved to (RG-08), or the item of the same user whose normalised URL
// matches, after appending the capture note and tightening the privacy (RG-04, RG-05, RG-15).
// A note capture stores its trimmed text as the item's note and is never a duplicate (RG-04).
// userID comes from the authenticated request, never from the body (RG-90). Nothing is stored
// when the capture is refused, and no extraction nor analysis runs here (RG-01).
func (s *Service) Capture(ctx context.Context, userID string, req Request) (Result, error) {
	if err := checkClientID(req.ClientID); err != nil {
		return Result{}, err
	}
	source, err := checkSource(req.Source)
	if err != nil {
		return Result{}, err
	}

	// RG-09: trim before any check; a blank capture note is absent, a blank note text is
	// refused below (RG-02).
	rawURL := trim(req.URL)
	note := trim(req.Note)
	userNote := trim(req.UserNote)
	if userNote != nil && *userNote == "" {
		userNote = nil
	}

	// Ambiguity first, so that the answer names the real problem (RG-07): a url or a note
	// counts when present, even blank.
	if (rawURL != nil && note != nil) || (note != nil && userNote != nil) {
		return Result{}, ErrAmbiguousCapture
	}
	if rawURL == nil && note == nil {
		return Result{}, ErrEmptyCapture
	}
	if note != nil {
		return s.captureNote(ctx, userID, *note, source, req)
	}
	return s.captureLink(ctx, userID, *rawURL, userNote, source, req)
}

// captureNote stores the trimmed text of a note capture as a new note item, or returns the item
// of a replayed client id (RG-08). It has no normalised URL, so it is never a duplicate and the
// note merge never runs (RG-04).
func (s *Service) captureNote(ctx context.Context, userID, text, source string, req Request) (Result, error) {
	if text == "" {
		return Result{}, ErrEmptyNote
	}
	in := store.NewItem{
		Kind:      KindNote,
		Source:    source,
		Status:    StatusQueued,
		Title:     noteTitle(text),
		UserNote:  &text,
		ClientID:  req.ClientID,
		IsPrivate: req.IsPrivate != nil && *req.IsPrivate,
	}
	item, created, err := s.store.CaptureItem(ctx, userID, in, nil)
	if err != nil {
		return Result{}, fmt.Errorf("capture note: %w", err)
	}
	return Result{Item: item, Created: created}, nil
}

// captureLink checks rawURL, the trimmed url of a link capture, and stores it, or returns the
// item of a replayed client id or of the same normalised URL (RG-04, RG-05, RG-08).
func (s *Service) captureLink(ctx context.Context, userID, rawURL string, userNote *string, source string, req Request) (Result, error) {
	title, ok := provisionalTitle(rawURL)
	if !ok {
		return Result{}, ErrInvalidURL
	}

	normalized := Normalize(rawURL)
	in := store.NewItem{
		Kind:          KindLink,
		Source:        source,
		Status:        StatusQueued,
		Title:         title,
		URL:           &rawURL,
		NormalizedURL: &normalized,
		UserNote:      userNote,
		ClientID:      req.ClientID,
		IsPrivate:     req.IsPrivate != nil && *req.IsPrivate,
	}
	item, created, err := s.store.CaptureItem(ctx, userID, in, s.noteMerge(userNote))
	if err != nil {
		return Result{}, fmt.Errorf("capture: %w", err)
	}
	return Result{Item: item, Created: created}, nil
}

// noteMerge is the merge of RG-04 for a duplicate carrying userNote: the day is the one of the
// store's clock in Paris, the server's reception day (RG-06). A capture without note keeps the
// item's note.
func (s *Service) noteMerge(userNote *string) store.NoteMerge {
	return func(existing *string, now time.Time) (*string, bool) {
		if userNote == nil {
			return existing, false
		}
		return MergeNote(existing, *userNote, dayIn(now, s.paris))
	}
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

// maxNoteTitle is the length of the provisional title of a note, in Unicode code points (RG-26).
const maxNoteTitle = 80

// noteTitle is the title of a note until analysis (RG-26): its first line that is not blank,
// trimmed (so a \r left by a \r\n line end goes too), cut after maxNoteTitle code points and
// followed by "…" when longer. A grapheme of several code points may be cut; the analysis
// replaces the title. text is the trimmed, non-empty text of the note, so a line is found.
func noteTitle(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > maxNoteTitle {
			return string(runes[:maxNoteTitle]) + "…"
		}
		return line
	}
	return ""
}
