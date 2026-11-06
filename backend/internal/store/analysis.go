package store

import (
	"context"
	"database/sql"
	"fmt"
)

// AnalysisQueueItem is an item waiting for the nightly batch, with what its message to the AI
// needs except its content, read one item at a time with ItemContent while the request is
// written, so that a single content is in memory at once.
type AnalysisQueueItem struct {
	ID                string
	Kind              string
	URL               *string // as captured
	SiteName          *string
	Title             string
	ExtractionMode    *string
	ContentTruncated  bool
	UserNote          *string
	IncompleteRetried bool // the response was cut once already: the cap is doubled (RG-36)
	// IsPrivate is always false as selected; it is returned so that the request builder checks
	// it again rather than trust the selection (RG-12, fail closed).
	IsPrivate bool
	Assets    []Asset // never nil
}

// AnalysisQueue returns at most limit items of userID waiting for the nightly batch: extracted,
// not private, not deleted, oldest capture first (ULIDs from the store clock), the order in
// which the batch takes them and leaves the surplus waiting (RG-12, RG-30 to RG-32, RG-90).
func (s *Store) AnalysisQueue(ctx context.Context, userID string, limit int) ([]AnalysisQueueItem, error) {
	if limit < 1 {
		// SQLite reads a negative LIMIT as no limit: refuse it rather than send the whole queue.
		return nil, fmt.Errorf("list analysis queue: limit %d, want at least 1", limit)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, url, site_name, title, extraction_mode, content_truncated, user_note, incomplete_retried, is_private
		 FROM items
		 WHERE user_id = ? AND status = 'extracted' AND is_private = 0 AND deleted_at IS NULL
		 ORDER BY id
		 LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list analysis queue: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	queue := []AnalysisQueueItem{}
	for rows.Next() {
		var (
			item                                AnalysisQueueItem
			url, siteName, extractionMode, note sql.Null[string]
		)
		if err := rows.Scan(&item.ID, &item.Kind, &url, &siteName, &item.Title, &extractionMode, &item.ContentTruncated,
			&note, &item.IncompleteRetried, &item.IsPrivate); err != nil {
			return nil, fmt.Errorf("list analysis queue: %w", err)
		}
		item.URL, item.SiteName, item.ExtractionMode, item.UserNote = ptr(url), ptr(siteName), ptr(extractionMode), ptr(note)
		queue = append(queue, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list analysis queue: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("list analysis queue: %w", err)
	}
	// The assets are read once the rows are closed, so that the selection holds one connection.
	for i := range queue {
		if queue[i].Assets, err = assetsOfItem(ctx, s.db, userID, queue[i].ID); err != nil {
			return nil, fmt.Errorf("list analysis queue: %w", err)
		}
	}
	return queue, nil
}
