package store

import (
	"database/sql"
	"testing"
)

// TokenHashForTest reads the stored token hash of a user (tests only).
func TokenHashForTest(t testing.TB, s *Store, userID string) string {
	t.Helper()
	var hash string
	if err := s.db.QueryRowContext(t.Context(), `SELECT api_token_hash FROM users WHERE id = ?`, userID).Scan(&hash); err != nil {
		t.Fatalf("read token hash: %v", err)
	}
	return hash
}

// SequenceForTest reads the current value of the modification sequence (tests only).
func SequenceForTest(t testing.TB, s *Store) int64 {
	t.Helper()
	var value int64
	if err := s.db.QueryRowContext(t.Context(), `SELECT value FROM sync_sequence WHERE id = 1`).Scan(&value); err != nil {
		t.Fatalf("read sequence: %v", err)
	}
	return value
}

// ItemCountForTest counts the items of a user (tests only).
func ItemCountForTest(t testing.TB, s *Store, userID string) int {
	t.Helper()
	var count int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM items WHERE user_id = ?`, userID).Scan(&count); err != nil {
		t.Fatalf("count items: %v", err)
	}
	return count
}

// NullColumnsForTest tells, for the nullable text columns of an item, which ones hold NULL
// (tests only): an absent value must be NULL, never "".
func NullColumnsForTest(t testing.TB, s *Store, userID, id string) map[string]bool {
	t.Helper()
	columns := []string{"url", "normalized_url", "site_name", "user_note", "extraction_mode", "content_text", "fiche_json", "deleted_at"}
	values := make([]bool, len(columns))
	dest := make([]any, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	err := s.db.QueryRowContext(t.Context(), `SELECT url IS NULL, normalized_url IS NULL, site_name IS NULL,
		user_note IS NULL, extraction_mode IS NULL, content_text IS NULL, fiche_json IS NULL,
		deleted_at IS NULL FROM items WHERE user_id = ? AND id = ?`, userID, id).Scan(dest...)
	if err != nil {
		t.Fatalf("read null columns: %v", err)
	}
	nulls := make(map[string]bool, len(columns))
	for i, column := range columns {
		nulls[column] = values[i]
	}
	return nulls
}

// CaptureSkippingLookupsForTest is CaptureItem without the client id and normalised URL
// lookups, as if another writer had committed the same client id or link in between: it
// reaches the safety net of each insert.
func CaptureSkippingLookupsForTest(t testing.TB, s *Store, userID string, in NewItem, merge NoteMerge) (Item, bool, error) {
	t.Helper()
	return s.captureItem(t.Context(), userID, in, merge, false)
}

// SeedVersion2ForTest creates the database at path with the migrations up to 00002 only, and
// stores a user and a link captured with clientID as E2-01 did, in items.client_id (tests of
// 00003 only). It returns the ids of the user and the item.
func SeedVersion2ForTest(t testing.TB, path, clientID string) (userID, itemID string) {
	t.Helper()
	ctx := t.Context()
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }() // every write below is committed
	provider, err := migrationProvider(db)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if _, err := provider.UpTo(ctx, 2); err != nil {
		t.Fatalf("migrate to version 2: %v", err)
	}
	userID, itemID = "01JB8Z6Q4M1X2Y3Z4A5B6C7D80", "01JB8Z6Q4M1X2Y3Z4A5B6C7D81"
	const date = "2026-10-26T10:00:00.000Z"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, name, api_token_hash, created_at) VALUES (?, 'owner', 'hash', ?)`,
		userID, date); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// The schema of 00002_items.sql: client_id is a column of items, normalized_url is NULL.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO items (id, user_id, kind, source, url, client_id, status, title, created_at, updated_at, change_seq)
		 VALUES (?, ?, 'link', 'share', 'https://example.com/a', ?, 'queued', 'example.com', ?, ?, 1)`,
		itemID, userID, clientID, date, date); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sync_sequence SET value = 1 WHERE id = 1`); err != nil {
		t.Fatalf("seed sequence: %v", err)
	}
	return userID, itemID
}
