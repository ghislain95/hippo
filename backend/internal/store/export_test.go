package store

import "testing"

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
	columns := []string{"url", "normalized_url", "site_name", "user_note", "client_id", "extraction_mode", "content_text", "fiche_json", "deleted_at"}
	values := make([]bool, len(columns))
	dest := make([]any, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	err := s.db.QueryRowContext(t.Context(), `SELECT url IS NULL, normalized_url IS NULL, site_name IS NULL,
		user_note IS NULL, client_id IS NULL, extraction_mode IS NULL, content_text IS NULL, fiche_json IS NULL,
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

// CaptureSkippingLookupForTest is CaptureItem without the client id lookup, as if another
// writer had committed the same client id in between: it reaches the unique-index backstop.
func CaptureSkippingLookupForTest(t testing.TB, s *Store, userID string, in NewItem) (Item, bool, error) {
	t.Helper()
	return s.captureItem(t.Context(), userID, in, false)
}
