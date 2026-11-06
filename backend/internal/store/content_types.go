package store

import (
	"context"
	"fmt"
)

// ContentType is a content type (RG-57). Content types are shared by every user (ADR-0022):
// content_types is the only table read without user_id. The seven starting types are seeded by
// the migration 00006_analysis.sql, ids 1 to 7 in the order of docs/metier.md section 7.
type ContentType struct {
	ID          int64
	Key         string
	Name        string
	Status      string // active or proposed
	HasTemplate bool
	ChangeSeq   int64
}

// ContentTypes returns the content types that are not deleted, by id (creation order).
func (s *Store) ContentTypes(ctx context.Context) ([]ContentType, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, key, name, status, has_template, change_seq FROM content_types WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list content types: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	types := []ContentType{}
	for rows.Next() {
		var ct ContentType
		if err := rows.Scan(&ct.ID, &ct.Key, &ct.Name, &ct.Status, &ct.HasTemplate, &ct.ChangeSeq); err != nil {
			return nil, fmt.Errorf("list content types: %w", err)
		}
		types = append(types, ct)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list content types: %w", err)
	}
	return types, nil
}
