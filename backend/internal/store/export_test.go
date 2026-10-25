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
