// Package storetest opens real SQLite databases for tests: a fresh file per call, with every
// migration applied. Tests never mock the store (ADR-0004, docs/dev/testing.md).
package storetest

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"
	"time"

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
	return open(t)
}

// OpenWithClock is OpenWithPath with a store that reads the time from now (store.WithClock).
// now is called from the goroutines of concurrent requests: it must be safe for that.
func OpenWithClock(t testing.TB, now func() time.Time) (*store.Store, string) {
	t.Helper()
	return open(t, store.WithClock(now))
}

func open(t testing.TB, opts ...store.Option) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hippo.db")
	st, err := store.Open(context.Background(), path, opts...)
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

// SetAnalysisState writes the status, analyzed_at and attempts of the item itemID of userID in
// the database at path, by SQL: no function of the store writes an analysis yet (E3), and the
// scenarios of the privacy switch need analysed and failed items (item-editing spec). A nil
// analyzedAt is stored as NULL. It fails the test unless exactly one item was written.
func SetAnalysisState(t testing.TB, path, userID, itemID, status string, analyzedAt *time.Time, attempts int) {
	t.Helper()
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	uri := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: query.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer func() { _ = db.Close() }() // the write below is committed when Exec returns
	var analyzed *string
	if analyzedAt != nil {
		date := analyzedAt.UTC().Format(store.TimeLayout)
		analyzed = &date
	}
	result, err := db.ExecContext(context.Background(),
		`UPDATE items SET status = ?, analyzed_at = ?, attempts = ? WHERE user_id = ? AND id = ?`,
		status, analyzed, attempts, userID, itemID)
	if err != nil {
		t.Fatalf("set analysis state: %v", err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("set analysis state: %d rows written (%v), want 1", n, err)
	}
}
