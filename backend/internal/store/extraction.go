package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// QueuedItem is an item waiting for its extraction, with what the extraction needs.
type QueuedItem struct {
	ID    string
	Kind  string
	URL   *string // nil for a note or an image
	Title string  // the provisional title (RG-26)
	// AttemptStarted tells that an extraction of this link started and never finished: the
	// backend stopped abruptly while reading the page.
	AttemptStarted bool
	// NetworkRetries is the number of network outages of the NAS this link already met.
	NetworkRetries int
}

// Extraction is the result of an extraction, written once on its item. Nil pointers are stored
// as NULL; a nil Title keeps the item's title.
type Extraction struct {
	Mode             string // full, fallback or none
	Title            *string
	SiteName         *string
	ContentText      *string
	ContentTruncated bool
	ReadingMinutes   *int64
}

// UserIDs returns the id of every user, so that the extraction scans each user's items on its
// own (RG-90).
func (s *Store) UserIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list users: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return ids, nil
}

// QueuedItems returns at most limit queued items of userID whose id is above afterID ("" for
// the first page), oldest first. Links whose extraction was deferred as a PDF are left out:
// their own story picks them up (E2-08).
func (s *Store) QueuedItems(ctx context.Context, userID, afterID string, limit int) ([]QueuedItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.kind, i.url, i.title, a.started_at IS NOT NULL, COALESCE(a.network_retries, 0)
		 FROM items i
		 LEFT JOIN extraction_attempts a ON a.user_id = ? AND a.item_id = i.id
		 WHERE i.user_id = ? AND i.status = 'queued' AND i.deleted_at IS NULL AND i.id > ?
		   AND a.outcome IS NULL
		 ORDER BY i.id
		 LIMIT ?`, userID, userID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list queued items: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only
	var items []QueuedItem
	for rows.Next() {
		var (
			item QueuedItem
			url  sql.Null[string]
		)
		if err := rows.Scan(&item.ID, &item.Kind, &url, &item.Title, &item.AttemptStarted, &item.NetworkRetries); err != nil {
			return nil, fmt.Errorf("list queued items: %w", err)
		}
		item.URL = ptr(url)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list queued items: %w", err)
	}
	return items, nil
}

// errOtherUsersAttempt: the attempt row of the item belongs to another user.
var errOtherUsersAttempt = errors.New("extraction attempt of another user")

// StartExtractionAttempt records that the extraction of the item itemID of userID starts: its
// attempt row is created, or an attempt released earlier gets its started_at back, keeping its
// network retries. The row is committed before the page is downloaded, so that a crash while
// reading it is seen at the next start. An item of another user is refused (RG-90): by the
// foreign key for a new row, by the user of an existing one.
func (s *Store) StartExtractionAttempt(ctx context.Context, userID, itemID string) error {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO extraction_attempts (item_id, user_id, started_at) VALUES (?, ?, ?)
		 ON CONFLICT (item_id) DO UPDATE SET started_at = excluded.started_at
		 WHERE extraction_attempts.user_id = excluded.user_id`,
		itemID, userID, s.now().UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("start extraction attempt: %w", err)
	}
	if n, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("start extraction attempt: %w", err)
	} else if n == 0 {
		return fmt.Errorf("start extraction attempt: %w", errOtherUsersAttempt)
	}
	return nil
}

// DeferExtraction marks the attempt of the item itemID of userID as a PDF: the item stays
// queued, and QueuedItems leaves it out until its own extraction (E2-08).
func (s *Store) DeferExtraction(ctx context.Context, userID, itemID string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE extraction_attempts SET outcome = 'deferred_pdf', started_at = NULL WHERE user_id = ? AND item_id = ?`,
		userID, itemID); err != nil {
		return fmt.Errorf("defer extraction: %w", err)
	}
	return nil
}

// ReleaseExtractionAttempt marks the attempt of the item itemID of userID as no longer under
// way, keeping its network retries: the item stays queued and is extracted again later, as
// after a stop of the backend.
func (s *Store) ReleaseExtractionAttempt(ctx context.Context, userID, itemID string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE extraction_attempts SET started_at = NULL WHERE user_id = ? AND item_id = ?`, userID, itemID); err != nil {
		return fmt.Errorf("release extraction attempt: %w", err)
	}
	return nil
}

// PostponeExtraction releases the attempt of the item itemID of userID after a network outage
// of the NAS, and adds it to network_retries when counted: the item stays queued until the
// worker gives up on it.
func (s *Store) PostponeExtraction(ctx context.Context, userID, itemID string, counted bool) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE extraction_attempts SET started_at = NULL, network_retries = network_retries + ?
		 WHERE user_id = ? AND item_id = ?`, boolInt(counted), userID, itemID); err != nil {
		return fmt.Errorf("postpone extraction: %w", err)
	}
	return nil
}

// WriteExtraction writes the result of the extraction of the item itemID of userID, in one
// short transaction: status extracted, the extraction columns only (never the note nor the
// privacy), updated_at and extracted_at from the store's clock, a new change_seq (ADR-0035).
// The attempt row is deleted in the same transaction. An item that is no longer queued, is
// deleted or belongs to another user is not written, and written is false.
func (s *Store) WriteExtraction(ctx context.Context, userID, itemID string, e Extraction) (written bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("write extraction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM extraction_attempts WHERE user_id = ? AND item_id = ?`, userID, itemID); err != nil {
		return false, fmt.Errorf("write extraction: %w", err)
	}
	var queued bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM items WHERE user_id = ? AND id = ? AND status = 'queued' AND deleted_at IS NULL)`,
		userID, itemID).Scan(&queued); err != nil {
		return false, fmt.Errorf("write extraction: %w", err)
	}
	if queued {
		seq, err := nextChangeSeq(ctx, tx)
		if err != nil {
			return false, err
		}
		date := s.now().UTC().Format(timeLayout)
		if _, err := tx.ExecContext(ctx,
			`UPDATE items SET status = 'extracted', extraction_mode = ?, title = COALESCE(?, title), site_name = ?,
			   content_text = ?, content_truncated = ?, reading_minutes = ?, extracted_at = ?, updated_at = ?, change_seq = ?
			 WHERE user_id = ? AND id = ? AND status = 'queued' AND deleted_at IS NULL`,
			e.Mode, e.Title, e.SiteName, e.ContentText, boolInt(e.ContentTruncated), e.ReadingMinutes, date, date, seq,
			userID, itemID); err != nil {
			return false, fmt.Errorf("write extraction: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("write extraction: %w", err)
	}
	return queued, nil
}

// ItemContent returns the extracted content of the item id of userID, nil when it has none, or
// ErrNotFound.
func (s *Store) ItemContent(ctx context.Context, userID, id string) (*string, error) {
	var content sql.Null[string]
	err := s.db.QueryRowContext(ctx,
		`SELECT content_text FROM items WHERE user_id = ? AND id = ? AND deleted_at IS NULL`, userID, id).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read item content: %w", err)
	}
	return ptr(content), nil
}
