// Package storetest opens real SQLite databases for tests: a fresh file per call, with every
// migration applied. Tests never mock the store (ADR-0004, docs/dev/testing.md).
//
// It also writes, by SQL, the states no function of the store writes yet (a deleted item,
// category or tag, a category or a tag created by the user, a correction by hand, a batch in
// any status, cost and versions): each helper names the story that will write it, and goes
// when that story ships. Only tests import it.
// Those helpers advance sync_sequence by SQL, outside the store's single sequence function: a
// test-only exception to that rule, like the seeding of migration 00006. A few others read,
// read-only, states no store function reads yet (tags, search text, a batch's tokens), each
// naming the story that will.
package storetest

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/naming"
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
// the database at path, by SQL. The store writes them only through a batch's results
// (WriteAnalysis, SettleItem); tests use this helper to set a starting state in one step: an
// analysed or failed item for the privacy switch (item-editing spec), a submitted item that
// already counts attempts for the threshold of RG-35. A nil analyzedAt is stored as NULL. It
// fails the test unless exactly one item was written.
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
// response (RG-36), by SQL. The store writes the mark only when a cut response comes back
// (SettleItem); tests use this helper to start from an item retried once, without playing a
// first batch.
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

// AddCategory inserts a category of userID by SQL, with its normalised name (name_key, RG-54),
// and returns its id, until E4 writes a category created by the user. The parent must be a
// category of the same user (composite foreign key); the depth is not checked (RG-51 is the
// store's). A name whose key a non-deleted category already holds under the same parent fails
// the test (unique index categories_user_parent_name).
func AddCategory(t testing.TB, path, userID string, c NewCategory) int64 {
	t.Helper()
	var id int64
	writeSynced(t, path, "add category", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		result, err := tx.ExecContext(context.Background(),
			`INSERT INTO categories (user_id, name, name_key, description, parent_id, status, proposal_reason, created_at, updated_at, change_seq)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, c.Name, naming.CategoryKey(c.Name), c.Description, c.ParentID, c.Status, c.ProposalReason, date, date, seq)
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
	Model            string // claude-haiku-test when empty
	PromptVersion    int    // 3 when 0
	EnvelopeVersion  int    // 1 when 0
}

// InsertBatch inserts a batch of userID by SQL, in any status, with any cost and any recorded
// versions, until E3-07 lets a batch be sent with other versions than the running ones (the
// envelope check of apply-batch-results, design D4) and the guards of the nightly pass read
// costs no store function writes before a results file was read (RG-81).
func InsertBatch(t testing.TB, path, userID string, b Batch) {
	t.Helper()
	db := openForWrite(t, path)
	defer func() { _ = db.Close() }() // the write below is committed when Exec returns
	var sentAt *string
	if b.AnthropicBatchID != nil {
		date := b.CreatedAt.UTC().Format(store.TimeLayout)
		sentAt = &date
	}
	if b.Model == "" {
		b.Model = "claude-haiku-test"
	}
	if b.PromptVersion == 0 {
		b.PromptVersion = 3
	}
	if b.EnvelopeVersion == 0 {
		b.EnvelopeVersion = 1
	}
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO batches (id, user_id, anthropic_batch_id, status, model, prompt_version, envelope_version, item_count,
		   cost_usd, created_at, sent_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, userID, b.AnthropicBatchID, b.Status, b.Model, b.PromptVersion, b.EnvelopeVersion, b.ItemCount, b.CostUSD,
		b.CreatedAt.UTC().Format(store.TimeLayout), sentAt); err != nil {
		t.Fatalf("insert batch: %v", err)
	}
}

// SetBatchEnvelopeVersion writes the envelope version recorded with the batch batchID of
// userID, by SQL, until a batch can be sent with another envelope version than the running
// one: the check of design D4 (apply-batch-results) sends such a batch's items back.
func SetBatchEnvelopeVersion(t testing.TB, path, userID, batchID string, version int) {
	t.Helper()
	db := openForWrite(t, path)
	defer func() { _ = db.Close() }() // the write below is committed when Exec returns
	result, err := db.ExecContext(context.Background(),
		`UPDATE batches SET envelope_version = ? WHERE user_id = ? AND id = ?`, version, userID, batchID)
	if err != nil {
		t.Fatalf("set batch envelope version: %v", err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("set batch envelope version: %d rows written (%v), want 1", n, err)
	}
}

// SetUserEdited writes the fields of the item itemID of userID corrected by hand (RG-60,
// RG-61: content_type, category, tags), by SQL, until E4 writes a correction.
func SetUserEdited(t testing.TB, path, userID, itemID string, fields ...string) {
	t.Helper()
	if fields == nil {
		fields = []string{}
	}
	edited, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("set user edited: %v", err)
	}
	writeSynced(t, path, "set user edited", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		return tx.ExecContext(context.Background(),
			`UPDATE items SET user_edited = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
			string(edited), date, seq, userID, itemID)
	})
}

// SetLastError writes the failure code of the last attempt of the item itemID of userID
// (invalid_response, request_errored, too_long), by SQL: the scenarios of a success after
// failed attempts need an item that failed with one batch and is submitted with the next,
// which no store function writes until E3-07's « Relancer ».
func SetLastError(t testing.TB, path, userID, itemID, code string) {
	t.Helper()
	writeSynced(t, path, "set last error", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		return tx.ExecContext(context.Background(),
			`UPDATE items SET last_error = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
			code, date, seq, userID, itemID)
	})
}

// AddTag inserts a tag of userID by SQL and returns its id, until E4 writes a tag created by
// the user. name must be the normalised form (RG-58).
func AddTag(t testing.TB, path, userID, name string) int64 {
	t.Helper()
	var id int64
	writeSynced(t, path, "add tag", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		result, err := tx.ExecContext(context.Background(),
			`INSERT INTO tags (user_id, name, updated_at, change_seq) VALUES (?, ?, ?, ?)`, userID, name, date, seq)
		if err != nil {
			return nil, err
		}
		id, err = result.LastInsertId()
		return result, err
	})
	return id
}

// AttachTag attaches the tag tagID to the item itemID, both of userID, by SQL, with a new
// change_seq of the item, until E4 writes a correction of the tags: the référentiel counts the
// items that carry a tag (RG-52) and a correction by hand keeps them (RG-61).
func AttachTag(t testing.TB, path, userID, itemID string, tagID int64) {
	t.Helper()
	writeSynced(t, path, "attach tag", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		if _, err := tx.ExecContext(context.Background(),
			`INSERT INTO item_tags (item_id, tag_id, user_id) VALUES (?, ?, ?)`, itemID, tagID, userID); err != nil {
			return nil, err
		}
		return tx.ExecContext(context.Background(),
			`UPDATE items SET updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`, date, seq, userID, itemID)
	})
}

// DeleteTag marks the tag id of userID deleted, by SQL, until E4 writes a deletion or a merge
// of tags (RG-74).
func DeleteTag(t testing.TB, path, userID string, id int64) {
	t.Helper()
	writeSynced(t, path, "delete tag", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		return tx.ExecContext(context.Background(),
			`UPDATE tags SET deleted_at = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ? AND deleted_at IS NULL`,
			date, date, seq, userID, id)
	})
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

// openForRead opens the database at path read-only, for a helper that reads a state no store
// function reads yet.
func openForRead(t testing.TB, path string) *sql.DB {
	t.Helper()
	uri := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatalf("open test database read-only: %v", err)
	}
	return db
}

// TagIDs returns the ids of the non-deleted tags of userID by name, read-only, until E4 reads
// the tags (GET /api/tags): the tests of a batch's results check which tags were created.
func TagIDs(t testing.TB, path, userID string) map[string]int64 {
	t.Helper()
	db := openForRead(t, path)
	defer func() { _ = db.Close() }() // read-only: nothing to flush
	rows, err := db.QueryContext(context.Background(),
		`SELECT name, id FROM tags WHERE user_id = ? AND deleted_at IS NULL`, userID)
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	defer func() { _ = rows.Close() }() // read only
	ids := map[string]int64{}
	for rows.Next() {
		var (
			name string
			id   int64
		)
		if err := rows.Scan(&name, &id); err != nil {
			t.Fatalf("read tags: %v", err)
		}
		ids[name] = id
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read tags: %v", err)
	}
	return ids
}

// AnalysisColumns are columns of an item that store.Item does not carry.
type AnalysisColumns struct {
	SearchText        *string
	IncompleteRetried bool
}

// ItemAnalysisColumns reads the search text and the cut-retry mark of the item itemID of
// userID, read-only, until E4 reads the search text (search) and E3-07 the mark: the tests of a
// batch's results check them (ADR-0007, RG-36).
func ItemAnalysisColumns(t testing.TB, path, userID, itemID string) AnalysisColumns {
	t.Helper()
	db := openForRead(t, path)
	defer func() { _ = db.Close() }() // read-only: nothing to flush
	var (
		cols AnalysisColumns
		text sql.Null[string]
	)
	if err := db.QueryRowContext(context.Background(),
		`SELECT search_text, incomplete_retried FROM items WHERE user_id = ? AND id = ?`, userID, itemID).Scan(
		&text, &cols.IncompleteRetried); err != nil {
		t.Fatalf("read item analysis columns: %v", err)
	}
	if text.Valid {
		cols.SearchText = &text.V
	}
	return cols
}

// BatchClosing is the closing state of a batch row: its token sums (nil until written), cost,
// end and whether its results were unavailable.
type BatchClosing struct {
	Status                                  string
	Input, Output, CacheCreation, CacheRead *int64
	CostUSD                                 *float64
	EndedAt                                 *string
	ResultsUnavailable                      bool
	ResultsLinesRead                        *int64
}

// BatchClosingOf reads the closing state of the batch batchID of userID, read-only, until E3-08
// reads the tokens and the cost of a batch (Réglages): the tests of a batch's results check
// them (RG-80).
func BatchClosingOf(t testing.TB, path, userID, batchID string) BatchClosing {
	t.Helper()
	db := openForRead(t, path)
	defer func() { _ = db.Close() }() // read-only: nothing to flush
	var (
		c                                    BatchClosing
		input, output, creation, read, lines sql.Null[int64]
		cost                                 sql.Null[float64]
		endedAt                              sql.Null[string]
	)
	if err := db.QueryRowContext(context.Background(),
		`SELECT status, input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens, cost_usd, ended_at,
		   results_unavailable_at IS NOT NULL, results_lines_read
		 FROM batches WHERE user_id = ? AND id = ?`, userID, batchID).Scan(
		&c.Status, &input, &output, &creation, &read, &cost, &endedAt, &c.ResultsUnavailable, &lines); err != nil {
		t.Fatalf("read batch closing: %v", err)
	}
	nullable := func(v sql.Null[int64]) *int64 {
		if !v.Valid {
			return nil
		}
		return &v.V
	}
	c.Input, c.Output, c.CacheCreation, c.CacheRead, c.ResultsLinesRead = nullable(input), nullable(output),
		nullable(creation), nullable(read), nullable(lines)
	if cost.Valid {
		c.CostUSD = &cost.V
	}
	if endedAt.Valid {
		c.EndedAt = &endedAt.V
	}
	return c
}

// RefuseItemWrites makes every later update of the item itemID of userID fail, by a trigger of
// the database at path: an update that would set the status toStatus, or any update when
// toStatus is empty. It plays a write of the store that fails for another reason than a context
// done (a constraint, a full disk), which no store function can be made to do otherwise: the
// tests of the follow-up check that such a failure neither blocks the batch for good nor loses a
// write (apply-batch-results design D8). userID and itemID are ids of the store.
func RefuseItemWrites(t testing.TB, path, userID, itemID, toStatus string) {
	t.Helper()
	when := "OLD.user_id = " + quoted(t, userID) + " AND OLD.id = " + quoted(t, itemID)
	if toStatus != "" {
		when += " AND NEW.status = " + quoted(t, toStatus)
	}
	refuse(t, path, "items", "storetest_refuse_item_"+itemID+toStatus, when)
}

// RefuseBatchWrites makes every later update of the batch batchID of userID fail, by a trigger
// of the database at path, as RefuseItemWrites does for an item: the tests of the follow-up
// check that a batch row the store cannot write is logged and read again (design D8).
func RefuseBatchWrites(t testing.TB, path, userID, batchID string) {
	t.Helper()
	refuse(t, path, "batches", "storetest_refuse_batch_"+batchID,
		"OLD.user_id = "+quoted(t, userID)+" AND OLD.id = "+quoted(t, batchID))
}

// refuse creates the trigger name that aborts the updates of table matching when; name is built
// from values quoted checked.
func refuse(t testing.TB, path, table, name, when string) {
	t.Helper()
	db := openForWrite(t, path)
	defer func() { _ = db.Close() }() // the statement below is committed when Exec returns
	if _, err := db.ExecContext(context.Background(),
		`CREATE TRIGGER `+name+` BEFORE UPDATE ON `+table+` WHEN `+when+
			` BEGIN SELECT RAISE(ABORT, 'storetest: write refused'); END`); err != nil {
		t.Fatalf("refuse writes of %s: %v", table, err)
	}
}

// quoted is value as an SQL string literal. A trigger takes no parameter: the ids and statuses
// written in it, and in its name, are checked to be non-empty, of letters, digits and
// underscores (ULIDs, statuses).
func quoted(t testing.TB, value string) string {
	t.Helper()
	if value == "" {
		t.Fatal("refuse writes: an empty id or status")
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && c != '_' {
			t.Fatalf("refuse writes: %q is not an id nor a status", value)
		}
	}
	return "'" + value + "'"
}

// SetFicheJSON writes raw as the stored fiche of the item itemID of userID, by SQL: a fiche
// that may not decode, which no store function writes (every fiche written passed the schema).
// The tests of the API check that such a stored value is an internal error that quotes nothing
// (apply-batch-results design D13).
func SetFicheJSON(t testing.TB, path, userID, itemID, raw string) {
	t.Helper()
	writeSynced(t, path, "set fiche", func(tx *sql.Tx, seq int64, date string) (sql.Result, error) {
		return tx.ExecContext(context.Background(),
			`UPDATE items SET fiche_json = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
			raw, date, seq, userID, itemID)
	})
}
