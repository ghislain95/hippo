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
	Kind   string
	Source string
	Status string
	Title  string
	// TitleAt, when set, computes the title from the store's clock at insert time instead of
	// Title, so that a dated title (RG-26) and created_at share one clock reading.
	TitleAt       func(now time.Time) string
	URL           *string
	NormalizedURL *string // RG-05; nil for a capture that is never a duplicate (RG-04)
	UserNote      *string
	ClientID      *string
	IsPrivate     bool
	Asset         *NewAsset // the image of an image capture, inserted with the new item only
}

// NoteMerge computes the note of an existing item after a duplicate capture (RG-04), from its
// current note and the store's clock, and tells whether it changed. The rule belongs to the
// capture domain; the store runs it inside the capture transaction, so that it extends the
// note it read.
type NoteMerge func(existing *string, now time.Time) (merged *string, changed bool)

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
	Assets           []Asset // never nil
}

// CaptureItem stores a capture of userID, in one write transaction (RG-04, RG-08, RG-90):
//
//   - replay: a client id this user already sent returns the item it resolved to, and nothing
//     is written;
//   - duplicate: a normalised URL of an item of this user returns that item, after merge
//     computed its note and the privacy only tightened (RG-15); the item is written only when
//     one of them changed, and keeps its url, source, title and status;
//   - otherwise a new item is inserted.
//
// The client id, if any, is recorded with the item in every case but the replay. created is
// true only for a new item. The item returned is always the row read back, so that a 201 and a
// later 200 carry the same values.
func (s *Store) CaptureItem(ctx context.Context, userID string, in NewItem, merge NoteMerge) (item Item, created bool, err error) {
	return s.captureItem(ctx, userID, in, merge, true)
}

// captureItem is CaptureItem; lookups false skips the replay and duplicate lookups of the first
// attempt, so that a test reaches the safety net.
func (s *Store) captureItem(ctx context.Context, userID string, in NewItem, merge NoteMerge, lookups bool) (Item, bool, error) {
	item, created, err := s.captureTx(ctx, userID, in, merge, lookups)
	var violation *uniqueViolation
	if !errors.As(err, &violation) {
		return item, created, err
	}
	// Writers are serialised (BEGIN IMMEDIATE), so no other capture can commit the same client
	// id or link between the lookups and the inserts. Should one ever do, the unique indexes
	// refuse the insert, the transaction rolls back, and the statement that failed says what
	// the capture is: a taken client id is a replay; a taken normalised URL is a duplicate,
	// captured once more so that its note and privacy are merged rather than lost.
	switch violation.table {
	case "capture_ids":
		existing, lookupErr := itemByClientID(ctx, s.db, userID, *in.ClientID)
		if lookupErr != nil {
			return Item{}, false, errors.Join(err, lookupErr)
		}
		return existing, false, nil
	default:
		return s.captureTx(ctx, userID, in, merge, true)
	}
}

func (s *Store) captureTx(ctx context.Context, userID string, in NewItem, merge NoteMerge, lookups bool) (Item, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, false, fmt.Errorf("capture item: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	now := s.now().UTC()
	item, created, err := s.resolveCapture(ctx, tx, userID, in, merge, lookups, now)
	if err != nil {
		return Item{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Item{}, false, fmt.Errorf("capture item: %w", err)
	}
	return item, created, nil
}

// resolveCapture runs the replay, duplicate and insert steps of CaptureItem inside tx.
func (s *Store) resolveCapture(ctx context.Context, tx *sql.Tx, userID string, in NewItem, merge NoteMerge, lookups bool, now time.Time) (Item, bool, error) {
	if lookups && in.ClientID != nil {
		existing, err := itemByClientID(ctx, tx, userID, *in.ClientID)
		switch {
		case err == nil:
			return existing, false, nil
		case !errors.Is(err, ErrNotFound):
			return Item{}, false, err
		}
	}

	if lookups && in.NormalizedURL != nil {
		existing, err := itemByNormalizedURL(ctx, tx, userID, *in.NormalizedURL)
		switch {
		case err == nil:
			item, err := s.mergeDuplicate(ctx, tx, existing, in, merge, now)
			if err != nil {
				return Item{}, false, err
			}
			if err := recordClientID(ctx, tx, userID, in.ClientID, item.ID, now); err != nil {
				return Item{}, false, err
			}
			return item, false, nil
		case !errors.Is(err, ErrNotFound):
			return Item{}, false, err
		}
	}

	id, err := s.insertItem(ctx, tx, userID, in, now)
	if err != nil {
		return Item{}, false, err
	}
	if in.Asset != nil {
		if err := insertAsset(ctx, tx, userID, id, *in.Asset, now); err != nil {
			return Item{}, false, err
		}
	}
	if err := recordClientID(ctx, tx, userID, in.ClientID, id, now); err != nil {
		return Item{}, false, err
	}
	item, err := itemByID(ctx, tx, userID, id)
	if err != nil {
		return Item{}, false, err
	}
	return item, true, nil
}

// mergeDuplicate applies a duplicate capture to the existing item: the note merge, and the
// privacy that only tightens (RG-04, RG-15). Nothing is written when neither changes.
func (s *Store) mergeDuplicate(ctx context.Context, tx *sql.Tx, existing Item, in NewItem, merge NoteMerge, now time.Time) (Item, error) {
	note, noteChanged := existing.UserNote, false
	if merge != nil {
		note, noteChanged = merge(existing.UserNote, now)
	}
	becomesPrivate := in.IsPrivate && !existing.IsPrivate
	if !noteChanged && !becomesPrivate {
		return existing, nil
	}

	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return Item{}, err
	}
	if noteChanged {
		if _, err := tx.ExecContext(ctx,
			`UPDATE items SET user_note = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
			note, now.Format(timeLayout), seq, existing.UserID, existing.ID); err != nil {
			return Item{}, fmt.Errorf("update item note: %w", err)
		}
	}
	if becomesPrivate {
		if err := setPrivate(ctx, tx, existing.UserID, existing.ID, true, now, seq); err != nil {
			return Item{}, err
		}
	}
	return itemByID(ctx, tx, existing.UserID, existing.ID)
}

func (s *Store) insertItem(ctx context.Context, tx *sql.Tx, userID string, in NewItem, now time.Time) (string, error) {
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return "", err
	}
	id, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("capture item: %w", err)
	}
	date := now.Format(timeLayout)
	title := in.Title
	if in.TitleAt != nil {
		title = in.TitleAt(now)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO items (id, user_id, kind, source, url, normalized_url, user_note, is_private, status, title, created_at, updated_at, change_seq)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, userID, in.Kind, in.Source, in.URL, in.NormalizedURL, in.UserNote, boolInt(in.IsPrivate), in.Status, title, date, date, seq,
	); err != nil {
		return "", asUniqueViolation("items", fmt.Errorf("insert item: %w", err))
	}
	return id, nil
}

// recordClientID remembers the client id of a capture with the item it resolved to (RG-08).
// capture_ids is not synced, so this write does not advance the sequence.
func recordClientID(ctx context.Context, tx *sql.Tx, userID string, clientID *string, itemID string, now time.Time) error {
	if clientID == nil {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO capture_ids (user_id, client_id, item_id, created_at) VALUES (?, ?, ?, ?)`,
		userID, *clientID, itemID, now.Format(timeLayout)); err != nil {
		return asUniqueViolation("capture_ids", fmt.Errorf("record client id: %w", err))
	}
	return nil
}

// SetPrivate sets the privacy flag of the item id of userID, or returns ErrNotFound. It leaves
// the status and the fiche alone: making an analysed item private keeps its fiche, and the flag
// alone prevents any re-analysis (RG-12, RG-14). Putting an item made public back into the
// analysis queue (RG-14) belongs to the story of the fiche's switch, not to this primitive.
// Nothing is written when the flag already has this value.
func (s *Store) SetPrivate(ctx context.Context, userID, id string, private bool) (Item, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Item{}, fmt.Errorf("set item privacy: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	item, err := itemByID(ctx, tx, userID, id)
	if err != nil {
		return Item{}, err
	}
	if item.IsPrivate != private {
		seq, err := nextChangeSeq(ctx, tx)
		if err != nil {
			return Item{}, err
		}
		if err := setPrivate(ctx, tx, userID, id, private, s.now().UTC(), seq); err != nil {
			return Item{}, err
		}
		if item, err = itemByID(ctx, tx, userID, id); err != nil {
			return Item{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Item{}, fmt.Errorf("set item privacy: %w", err)
	}
	return item, nil
}

// setPrivate writes the privacy flag of an item inside tx, with the modification date and the
// change_seq the caller got from nextChangeSeq in the same transaction.
func setPrivate(ctx context.Context, tx *sql.Tx, userID, id string, private bool, now time.Time, seq int64) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE items SET is_private = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
		boolInt(private), now.Format(timeLayout), seq, userID, id); err != nil {
		return fmt.Errorf("set item privacy: %w", err)
	}
	return nil
}

// Item returns the item id of userID, or ErrNotFound.
func (s *Store) Item(ctx context.Context, userID, id string) (Item, error) {
	return itemByID(ctx, s.db, userID, id)
}

// querier is what a read needs from a *sql.DB or a *sql.Tx.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

const itemColumns = `id, user_id, kind, source, title, url, site_name, user_note, is_private, status,
	extraction_mode, content_truncated, reading_minutes, content_ok, content_issue, language, freshness, model,
	prompt_version, schema_version, user_edited, annotations, is_pinned, read_status, read_progress, attempts,
	last_error, created_at, updated_at, extracted_at, analyzed_at, read_at, change_seq`

func itemByID(ctx context.Context, db querier, userID, id string) (Item, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE user_id = ? AND id = ? AND deleted_at IS NULL`, userID, id)
	return withAssets(ctx, db, row)
}

// itemByClientID returns the item a capture of userID with clientID resolved to. Both tables
// are filtered by userID, so a client id never leads to another user's item (RG-90).
func itemByClientID(ctx context.Context, db querier, userID, clientID string) (Item, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+itemColumns+` FROM items
		 WHERE user_id = ? AND deleted_at IS NULL
		   AND id = (SELECT item_id FROM capture_ids WHERE user_id = ? AND client_id = ?)`, userID, userID, clientID)
	return withAssets(ctx, db, row)
}

// itemByNormalizedURL returns the item of userID with this normalised URL (RG-04). The
// predicate is the one of the unique index items_user_normalized_url: the story that deletes
// items must keep both aligned if it filters deleted_at here.
func itemByNormalizedURL(ctx context.Context, db querier, userID, normalizedURL string) (Item, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+itemColumns+` FROM items WHERE user_id = ? AND normalized_url = ?`, userID, normalizedURL)
	return withAssets(ctx, db, row)
}

// withAssets reads the item of row and its assets, so that every item read back carries them.
func withAssets(ctx context.Context, db querier, row *sql.Row) (Item, error) {
	item, err := scanItem(row)
	if err != nil {
		return Item{}, err
	}
	if item.Assets, err = assetsOfItem(ctx, db, item.UserID, item.ID); err != nil {
		return Item{}, fmt.Errorf("read item %s: %w", item.ID, err)
	}
	return item, nil
}

func scanItem(row *sql.Row) (Item, error) {
	var (
		item                                         Item
		url, siteName, userNote                      sql.Null[string]
		extractionMode, contentIssue, language       sql.Null[string]
		freshness, model, annotations, lastError     sql.Null[string]
		readingMinutes, promptVersion, schemaVersion sql.Null[int64]
		contentOK                                    sql.Null[bool]
		userEdited, createdAt, updatedAt             string
		extractedAt, analyzedAt, readAt              sql.Null[string]
	)
	err := row.Scan(&item.ID, &item.UserID, &item.Kind, &item.Source, &item.Title, &url, &siteName, &userNote,
		&item.IsPrivate, &item.Status, &extractionMode, &item.ContentTruncated, &readingMinutes,
		&contentOK, &contentIssue, &language, &freshness, &model, &promptVersion, &schemaVersion, &userEdited,
		&annotations, &item.IsPinned, &item.ReadStatus, &item.ReadProgress, &item.Attempts, &lastError,
		&createdAt, &updatedAt, &extractedAt, &analyzedAt, &readAt, &item.ChangeSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("read item: %w", err)
	}

	item.URL, item.SiteName, item.UserNote = ptr(url), ptr(siteName), ptr(userNote)
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

// uniqueViolation is a unique constraint refused by the insert into table. The safety net of
// CaptureItem tells a replay from a duplicate by the statement that failed, not by the text of
// the SQLite message.
type uniqueViolation struct {
	table string
	err   error
}

func (e *uniqueViolation) Error() string { return e.err.Error() }

func (e *uniqueViolation) Unwrap() error { return e.err }

// asUniqueViolation marks err as a uniqueViolation of table when SQLite refused a unique
// constraint, and returns it unchanged otherwise.
func asUniqueViolation(table string, err error) error {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
		sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY) {
		return &uniqueViolation{table: table, err: err}
	}
	return err
}
