package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// tagsOfItem returns the ids of the non-deleted tags of the item itemID of userID, ascending.
// Never nil.
func tagsOfItem(ctx context.Context, db querier, userID, itemID string) ([]int64, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT it.tag_id FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.user_id = it.user_id
		 WHERE it.user_id = ? AND it.item_id = ? AND t.deleted_at IS NULL ORDER BY it.tag_id`, userID, itemID)
	if err != nil {
		return nil, fmt.Errorf("list item tags: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list item tags: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list item tags: %w", err)
	}
	return ids, nil
}

// ReferentielTags returns the names of userID's non-deleted tags carried by at least one item
// that is neither private nor deleted, most carried first, then by name, limit at most: the
// <tags> block of the référentiel (RG-12, RG-52, RG-90, design D10). Names are normalised
// (ASCII), so the name order is the byte order.
func (s *Store) ReferentielTags(ctx context.Context, userID string, limit int) ([]string, error) {
	if limit < 1 {
		// SQLite reads a negative LIMIT as no limit.
		return nil, fmt.Errorf("list référentiel tags: limit %d, want at least 1", limit)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.name FROM tags t
		   JOIN item_tags it ON it.tag_id = t.id AND it.user_id = t.user_id
		   JOIN items i ON i.id = it.item_id AND i.user_id = it.user_id
		  WHERE t.user_id = ? AND t.deleted_at IS NULL AND i.deleted_at IS NULL AND i.is_private = 0
		  GROUP BY t.id ORDER BY COUNT(*) DESC, t.name ASC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list référentiel tags: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list référentiel tags: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list référentiel tags: %w", err)
	}
	return names, nil
}

// resolveTag returns the id of userID's non-deleted tag named name, inserting it with its own
// change_seq when there is none (RG-58). name is already normalised.
func resolveTag(ctx context.Context, tx *sql.Tx, userID, name, date string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM tags WHERE user_id = ? AND name = ? AND deleted_at IS NULL`, userID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("read tag: %w", err)
	}
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO tags (user_id, name, updated_at, change_seq) VALUES (?, ?, ?, ?) RETURNING id`,
		userID, name, date, seq).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert tag: %w", err)
	}
	return id, nil
}

// replaceItemTags sets the tags of the item itemID of userID to tagIDs inside tx. The item's
// change_seq is advanced by the caller, in the same transaction.
func replaceItemTags(ctx context.Context, tx *sql.Tx, userID, itemID string, tagIDs []int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_tags WHERE user_id = ? AND item_id = ?`, userID, itemID); err != nil {
		return fmt.Errorf("replace item tags: %w", err)
	}
	for _, tagID := range tagIDs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO item_tags (item_id, tag_id, user_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
			itemID, tagID, userID); err != nil {
			return fmt.Errorf("replace item tags: %w", err)
		}
	}
	return nil
}
