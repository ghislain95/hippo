// Package storetest opens real SQLite databases for tests: a fresh file per call, with every
// migration applied. Tests never mock the store (ADR-0004, docs/dev/testing.md).
package storetest

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// Open returns a store on a new database in t.TempDir(), closed when the test ends.
func Open(t testing.TB) *store.Store {
	t.Helper()
	st, _ := OpenWithPath(t)
	return st
}

// OpenWithPath is Open, and also returns the path of the database file, for ItemCount.
func OpenWithPath(t testing.TB) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hippo.db")
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return st, path
}

// ItemCount counts the items of userID in the database at path, through a read-only
// connection: it proves that a refused request stored nothing while no API reads items yet.
func ItemCount(t testing.TB, path, userID string) int {
	t.Helper()
	uri := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatalf("open test database read-only: %v", err)
	}
	defer func() { _ = db.Close() }() // read-only: nothing to flush
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM items WHERE user_id = ?`, userID).Scan(&count); err != nil {
		t.Fatalf("count items: %v", err)
	}
	return count
}
