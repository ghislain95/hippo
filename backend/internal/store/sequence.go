package store

import (
	"context"
	"database/sql"
	"fmt"
)

// nextChangeSeq advances the database-wide modification sequence and returns its new value
// (ADR-0035). It is the only writer of sync_sequence: every write of a synced row (items, and
// later categories, content types and tags) calls it inside its own transaction, so that the
// value is rolled back with a failed write. Writers are serialised by BEGIN IMMEDIATE, so two
// writes never get the same value.
func nextChangeSeq(ctx context.Context, tx *sql.Tx) (int64, error) {
	var value int64
	if err := tx.QueryRowContext(ctx,
		`UPDATE sync_sequence SET value = value + 1 WHERE id = 1 RETURNING value`).Scan(&value); err != nil {
		return 0, fmt.Errorf("advance change sequence: %w", err)
	}
	return value, nil
}
