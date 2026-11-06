// Package storetest opens real SQLite databases for tests: a fresh file per call, with every
// migration applied. Tests never mock the store (ADR-0004, docs/dev/testing.md).
//
// It also writes, by SQL, the states no function of the store writes yet (an analysis result,
// a deleted item or category, a proposed category, a batch's status and cost): each helper
// names the story that will write it, and goes when that story ships. Only tests import it.
// Those helpers advance sync_sequence by SQL, outside the store's single sequence function: a
// test-only exception to that rule, like the seeding of migration 00006.
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
	db := openForWrite(t, path)
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

// openForWrite opens the database at path for a helper that writes by SQL, waiting for the
// store's writers rather than failing, with foreign keys on as in the store.
func openForWrite(t testing.TB, path string) *sql.DB {
	t.Helper()
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Set("_txlock", "immediate")
	uri := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: query.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	return db
}

// writeSynced runs update in one transaction with the next value of the modification sequence,
// as a store write of a synced row does (ADR-0035), and fails the test unless it wrote exactly
// one row.
func writeSynced(t testing.TB, path, what string, update func(tx *sql.Tx, seq int64, date string) (sql.Result, error)) {
	t.Helper()
	ctx := context.Background()
	db := openForWrite(t, path)
	defer func() { _ = db.Close() }() // the transaction below is committed or rolled back
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	var seq int64
	if err := tx.QueryRowContext(ctx, `UPDATE sync_sequence SET value = value + 1 WHERE id = 1 RETURNING value`).Scan(&seq); err != nil {
		t.Fatalf("%s: advance sequence: %v", what, err)
	}
	result, err := update(tx, seq, time.Now().UTC().Format(store.TimeLayout))
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("%s: %d rows written (%v), want 1", what, n, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// DeleteItem marks the item itemID of userID deleted, by SQL, until E4 writes a deletion: the
// selection of the nightly batch must leave deleted items out.
func DeleteItem(t testing.TB, path, userID, itemID string) {
	t.Helper()
	writeSynced(t, path, "delete item", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		return tx.ExecContext(context.Background(),
			`UPDATE items SET deleted_at = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
			date, date, seq, userID, itemID)
	})
}

// SetIncompleteRetried marks the item itemID of userID as already sent once more after a cut
// response (RG-36), by SQL, until E3-02 writes it.
func SetIncompleteRetried(t testing.TB, path, userID, itemID string) {
	t.Helper()
	writeSynced(t, path, "set incomplete retried", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		return tx.ExecContext(context.Background(),
			`UPDATE items SET incomplete_retried = 1, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
			date, seq, userID, itemID)
	})
}

// NewCategory is a category written by AddCategory.
type NewCategory struct {
	Name           string
	Description    *string
	ParentID       *int64 // nil for a root
	Status         string // active or proposed
	ProposalReason *string
}

// AddCategory inserts a category of userID by SQL and returns its id, until E3-03 writes a
// proposed category and E4 a category created by the user. The parent must be a category of
// the same user (composite foreign key); the depth is not checked (RG-51 is the store's, from
// E3-03).
func AddCategory(t testing.TB, path, userID string, c NewCategory) int64 {
	t.Helper()
	var id int64
	writeSynced(t, path, "add category", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		result, err := tx.ExecContext(context.Background(),
			`INSERT INTO categories (user_id, name, description, parent_id, status, proposal_reason, created_at, updated_at, change_seq)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, c.Name, c.Description, c.ParentID, c.Status, c.ProposalReason, date, date, seq)
		if err != nil {
			return nil, err
		}
		id, err = result.LastInsertId()
		return result, err
	})
	return id
}

// DeleteCategory marks the category id of userID deleted, by SQL, until E4 writes a deletion.
func DeleteCategory(t testing.TB, path, userID string, id int64) {
	t.Helper()
	writeSynced(t, path, "delete category", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		return tx.ExecContext(context.Background(),
			`UPDATE categories SET deleted_at = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
			date, date, seq, userID, id)
	})
}

// Batch is a batch row written by InsertBatch.
type Batch struct {
	ID               string // a ULID chosen by the test
	Status           string // pending, in_progress, ended or failed
	AnthropicBatchID *string
	ItemCount        int
	CostUSD          *float64
	CreatedAt        time.Time
}

// InsertBatch inserts a batch of userID by SQL, in any status and with any cost, until E3-02
// writes ended batches and their cost: the guards of the nightly pass read them (RG-81).
func InsertBatch(t testing.TB, path, userID string, b Batch) {
	t.Helper()
	db := openForWrite(t, path)
	defer func() { _ = db.Close() }() // the write below is committed when Exec returns
	var sentAt *string
	if b.AnthropicBatchID != nil {
		date := b.CreatedAt.UTC().Format(store.TimeLayout)
		sentAt = &date
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO batches (id, user_id, anthropic_batch_id, status, model, prompt_version, envelope_version, item_count,
		   cost_usd, created_at, sent_at)
		 VALUES (?, ?, ?, ?, 'claude-haiku-test', 3, 1, ?, ?, ?, ?)`,
		b.ID, userID, b.AnthropicBatchID, b.Status, b.ItemCount, b.CostUSD, b.CreatedAt.UTC().Format(store.TimeLayout), sentAt); err != nil {
		t.Fatalf("insert batch: %v", err)
	}
}

// SetBatchState writes the status and cost of the batch batchID of userID by SQL, until E3-02
// writes them when the results come back. A nil cost is stored as NULL.
func SetBatchState(t testing.TB, path, userID, batchID, status string, costUSD *float64) {
	t.Helper()
	db := openForWrite(t, path)
	defer func() { _ = db.Close() }() // the write below is committed when Exec returns
	result, err := db.ExecContext(context.Background(),
		`UPDATE batches SET status = ?, cost_usd = ? WHERE user_id = ? AND id = ?`, status, costUSD, userID, batchID)
	if err != nil {
		t.Fatalf("set batch state: %v", err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("set batch state: %d rows written (%v), want 1", n, err)
	}
}
