package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrNotFound is returned when a row does not exist for this user (RG-90: another user's row is
// not found either).
var ErrNotFound = errors.New("not found")

// NewItem is a captured item to store. Nil pointers are stored as NULL, never as "".
type NewItem struct {
	Kind      string
	Source    string
	Status    string
	Title     string
	URL       *string
	UserNote  *string
	ClientID  *string
	IsPrivate bool
}

// Item is a stored item, without its extracted content.
type Item struct {
	ID               string
	UserID           string
	Kind             string
	Source           string
	Title            string
	URL              *string
	SiteName         *string
	UserNote         *string
	ClientID         *string
	IsPrivate        bool
	Status           string
	ExtractionMode   *string
	ContentTruncated bool
	ReadingMinutes   *int64
	ContentOK        *bool
	ContentIssue     *string
	Language         *string
	Freshness        *string
	Model            *string
	PromptVersion    *int64
	SchemaVersion    *int64
	UserEdited       []string
	Annotations      *string
	IsPinned         bool
	ReadStatus       string
	ReadProgress     float64
	Attempts         int64
	LastError        *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ExtractedAt      *time.Time
	AnalyzedAt       *time.Time
	ReadAt           *time.Time
	ChangeSeq        int64
}

// CaptureItem stores a captured item for userID, or returns the item this user already
// captured with the same client id (RG-08). created is false for a replay, which changes
// nothing. The lookup and the insert run in one write transaction, and the item returned is
// always the row read back, so that a 201 and a later 200 carry the same values.
func (s *Store) CaptureItem(ctx context.Context, userID string, in NewItem) (item Item, created bool, err error) {
	return s.captureItem(ctx, userID, in, true)
}

// captureItem is CaptureItem; replayFirst false skips the lookup, so that a test reaches the
// unique-index backstop.
func (s *Store) captureItem(ctx context.Context, userID string, in NewItem, replayFirst bool) (Item, bool, error) {
	item, created, err := s.insertOrReplay(ctx, userID, in, replayFirst)
	if err == nil || in.ClientID == nil || !isUniqueViolation(err) {
		return item, created, err
	}
	// Writers are serialised (BEGIN IMMEDIATE), so no other capture can commit this client id
	// between the lookup and the insert. Should one ever do, the unique index refuses the
	// second insert, and the capture is a replay rather than a 500.
	existing, lookupErr := itemByClientID(ctx, s.db, userID, *in.ClientID)
	if lookupErr != nil {
		return Item{}, false, errors.Join(err, lookupErr)
	}
	return existing, false, nil
}

func (s *Store) insertOrReplay(ctx context.Context, userID string, in NewItem, replayFirst bool) (Item, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, false, fmt.Errorf("capture item: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if replayFirst && in.ClientID != nil {
		existing, err := itemByClientID(ctx, tx, userID, *in.ClientID)
		switch {
		case err == nil:
			if err := tx.Commit(); err != nil {
				return Item{}, false, fmt.Errorf("capture item: %w", err)
			}
			return existing, false, nil
		case !errors.Is(err, ErrNotFound):
			return Item{}, false, err
		}
	}

	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return Item{}, false, err
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return Item{}, false, fmt.Errorf("capture item: %w", err)
	}
	date := now.Format(timeLayout)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO items (id, user_id, kind, source, url, user_note, client_id, is_private, status, title, created_at, updated_at, change_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, in.Kind, in.Source, in.URL, in.UserNote, in.ClientID, boolInt(in.IsPrivate), in.Status, in.Title, date, date, seq,
	); err != nil {
		return Item{}, false, fmt.Errorf("insert item: %w", err)
	}
	item, err := itemByID(ctx, tx, userID, id)
	if err != nil {
		return Item{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, false, fmt.Errorf("capture item: %w", err)
	}
	return item, true, nil
}

// Item returns the item id of userID, or ErrNotFound.
func (s *Store) Item(ctx context.Context, userID, id string) (Item, error) {
	return itemByID(ctx, s.db, userID, id)
}

// querier is what a read needs from a *sql.DB or a *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const itemColumns = `id, user_id, kind, source, title, url, site_name, user_note, client_id, is_private, status,
	extraction_mode, content_truncated, reading_minutes, content_ok, content_issue, language, freshness, model,
	prompt_version, schema_version, user_edited, annotations, is_pinned, read_status, read_progress, attempts,
	last_error, created_at, updated_at, extracted_at, analyzed_at, read_at, change_seq`

func itemByID(ctx context.Context, db querier, userID, id string) (Item, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE user_id = ? AND id = ? AND deleted_at IS NULL`, userID, id)
	return scanItem(row)
}

func itemByClientID(ctx context.Context, db querier, userID, clientID string) (Item, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE user_id = ? AND client_id = ? AND deleted_at IS NULL`, userID, clientID)
	return scanItem(row)
}

func scanItem(row *sql.Row) (Item, error) {
	var (
		item                                         Item
		url, siteName, userNote, clientID            sql.Null[string]
		extractionMode, contentIssue, language       sql.Null[string]
		freshness, model, annotations, lastError     sql.Null[string]
		readingMinutes, promptVersion, schemaVersion sql.Null[int64]
		contentOK                                    sql.Null[bool]
		userEdited, createdAt, updatedAt             string
		extractedAt, analyzedAt, readAt              sql.Null[string]
	)
	err := row.Scan(&item.ID, &item.UserID, &item.Kind, &item.Source, &item.Title, &url, &siteName, &userNote,
		&clientID, &item.IsPrivate, &item.Status, &extractionMode, &item.ContentTruncated, &readingMinutes,
		&contentOK, &contentIssue, &language, &freshness, &model, &promptVersion, &schemaVersion, &userEdited,
		&annotations, &item.IsPinned, &item.ReadStatus, &item.ReadProgress, &item.Attempts, &lastError,
		&createdAt, &updatedAt, &extractedAt, &analyzedAt, &readAt, &item.ChangeSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("read item: %w", err)
	}

	item.URL, item.SiteName, item.UserNote, item.ClientID = ptr(url), ptr(siteName), ptr(userNote), ptr(clientID)
	item.ExtractionMode, item.ContentIssue, item.Language = ptr(extractionMode), ptr(contentIssue), ptr(language)
	item.Freshness, item.Model, item.Annotations, item.LastError = ptr(freshness), ptr(model), ptr(annotations), ptr(lastError)
	item.ReadingMinutes, item.PromptVersion, item.SchemaVersion = ptr(readingMinutes), ptr(promptVersion), ptr(schemaVersion)
	item.ContentOK = ptr(contentOK)

	if err := json.Unmarshal([]byte(userEdited), &item.UserEdited); err != nil {
		return Item{}, fmt.Errorf("read item %s: user_edited: %w", item.ID, err)
	}
	if item.UserEdited == nil {
		item.UserEdited = []string{}
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return Item{}, fmt.Errorf("read item %s: created_at: %w", item.ID, err)
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return Item{}, fmt.Errorf("read item %s: updated_at: %w", item.ID, err)
	}
	for _, date := range []struct {
		raw  sql.Null[string]
		dest **time.Time
		name string
	}{{extractedAt, &item.ExtractedAt, "extracted_at"}, {analyzedAt, &item.AnalyzedAt, "analyzed_at"}, {readAt, &item.ReadAt, "read_at"}} {
		if !date.raw.Valid {
			continue
		}
		parsed, err := parseTime(date.raw.V)
		if err != nil {
			return Item{}, fmt.Errorf("read item %s: %s: %w", item.ID, date.name, err)
		}
		*date.dest = &parsed
	}
	return item, nil
}

func parseTime(raw string) (time.Time, error) {
	parsed, err := time.Parse(timeLayout, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date: %w", err)
	}
	return parsed.UTC(), nil
}

// ptr returns a pointer to the value of a nullable column, nil for NULL.
func ptr[T any](value sql.Null[T]) *T {
	if !value.Valid {
		return nil
	}
	return &value.V
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
