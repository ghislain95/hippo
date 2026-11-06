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

// InsertAssetRowForTest inserts an asset row by SQL, with the user and item it is given (tests
// of 00004 only: no function of the store can write an asset of another user than its item's).
func InsertAssetRowForTest(t testing.TB, s *Store, id, itemID, userID string) error {
	t.Helper()
	_, err := s.db.ExecContext(t.Context(),
		`INSERT INTO assets (id, item_id, user_id, path, mime_type, size_bytes, created_at)
		 VALUES (?, ?, ?, 'test.png', 'image/png', 1, '2026-10-27T10:00:00.000Z')`, id, itemID, userID)
	return err
}

// AssetCountForTest counts the assets of a user (tests only).
func AssetCountForTest(t testing.TB, s *Store, userID string) int {
	t.Helper()
	var count int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM assets WHERE user_id = ?`, userID).Scan(&count); err != nil {
		t.Fatalf("count assets: %v", err)
	}
	return count
}

// ItemAssetsForTest lists the assets of an item of a user, as the item read back does (tests
// only).
func ItemAssetsForTest(t testing.TB, s *Store, userID, itemID string) ([]Asset, error) {
	t.Helper()
	return assetsOfItem(t.Context(), s.db, userID, itemID)
}

// AttemptCountForTest counts the extraction attempt rows of a user (tests only).
func AttemptCountForTest(t testing.TB, s *Store, userID string) int {
	t.Helper()
	var count int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM extraction_attempts WHERE user_id = ?`, userID).Scan(&count); err != nil {
		t.Fatalf("count extraction attempts: %v", err)
	}
	return count
}

// AttemptUnderWayForTest tells whether the attempt of an item has its started_at set (tests
// only).
func AttemptUnderWayForTest(t testing.TB, s *Store, userID, itemID string) bool {
	t.Helper()
	var underWay bool
	if err := s.db.QueryRowContext(t.Context(),
		`SELECT started_at IS NOT NULL FROM extraction_attempts WHERE user_id = ? AND item_id = ?`, userID, itemID).Scan(&underWay); err != nil {
		t.Fatalf("read extraction attempt: %v", err)
	}
	return underWay
}

// InsertDeferredPDFAttemptForTest writes the attempt row that E2-05 left for a link answered as
// a PDF (tests only: no function of the store writes it any more).
func InsertDeferredPDFAttemptForTest(t testing.TB, s *Store, userID, itemID string) {
	t.Helper()
	if _, err := s.db.ExecContext(t.Context(),
		`INSERT INTO extraction_attempts (item_id, user_id, outcome) VALUES (?, ?, 'deferred_pdf')`, itemID, userID); err != nil {
		t.Fatalf("insert deferred attempt: %v", err)
	}
}

// SeedVersion5ForTest creates the database at path with the migrations up to 00005 only, and
// stores, by SQL as E2 did, a user and two items: an extracted link and a queued note (tests of
// 00006 only). It returns the ids of the user and the items, and the value of the sequence.
func SeedVersion5ForTest(t testing.TB, path string) (userID string, itemIDs []string, sequence int64) {
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
	if _, err := provider.UpTo(ctx, 5); err != nil {
		t.Fatalf("migrate to version 5: %v", err)
	}
	userID = "01JB8Z6Q4M1X2Y3Z4A5B6C7D90"
	itemIDs = []string{"01JB8Z6Q4M1X2Y3Z4A5B6C7D91", "01JB8Z6Q4M1X2Y3Z4A5B6C7D92"}
	const date = "2026-11-01T10:00:00.000Z"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, name, api_token_hash, created_at) VALUES (?, 'owner', 'hash', ?)`,
		userID, date); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO items (id, user_id, kind, source, url, normalized_url, status, extraction_mode, title, created_at, updated_at, extracted_at, change_seq)
		 VALUES (?, ?, 'link', 'share', 'https://example.com/a', 'https://example.com/a', 'extracted', 'full', 'A page', ?, ?, ?, 1)`,
		itemIDs[0], userID, date, date, date); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO items (id, user_id, kind, source, user_note, status, title, created_at, updated_at, change_seq)
		 VALUES (?, ?, 'note', 'share', 'An idea', 'queued', 'An idea', ?, ?, 2)`,
		itemIDs[1], userID, date, date); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE sync_sequence SET value = 2 WHERE id = 1`); err != nil {
		t.Fatalf("seed sequence: %v", err)
	}
	return userID, itemIDs, 2
}

// ContentTypeRowForTest is a row of content_types as the migration wrote it (tests only).
type ContentTypeRowForTest struct {
	ID          int64
	Key         string
	Name        string
	Status      string
	HasTemplate int64
	UpdatedAt   string
	Deleted     bool
	ChangeSeq   int64
}

// ContentTypeRowsForTest reads every row of content_types, by id (tests only).
func ContentTypeRowsForTest(t testing.TB, s *Store) []ContentTypeRowForTest {
	t.Helper()
	rows, err := s.db.QueryContext(t.Context(),
		`SELECT id, key, name, status, has_template, updated_at, deleted_at IS NOT NULL, change_seq FROM content_types ORDER BY id`)
	if err != nil {
		t.Fatalf("read content types: %v", err)
	}
	defer func() { _ = rows.Close() }() // read only
	var types []ContentTypeRowForTest
	for rows.Next() {
		var row ContentTypeRowForTest
		if err := rows.Scan(&row.ID, &row.Key, &row.Name, &row.Status, &row.HasTemplate, &row.UpdatedAt, &row.Deleted, &row.ChangeSeq); err != nil {
			t.Fatalf("read content types: %v", err)
		}
		types = append(types, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read content types: %v", err)
	}
	return types
}

// ClassificationNullsForTest tells whether type_id, category_id and batch_id of an item are
// NULL (tests only).
func ClassificationNullsForTest(t testing.TB, s *Store, userID, itemID string) (typeNull, categoryNull, batchNull bool) {
	t.Helper()
	if err := s.db.QueryRowContext(t.Context(),
		`SELECT type_id IS NULL, category_id IS NULL, batch_id IS NULL FROM items WHERE user_id = ? AND id = ?`,
		userID, itemID).Scan(&typeNull, &categoryNull, &batchNull); err != nil {
		t.Fatalf("read classification columns: %v", err)
	}
	return typeNull, categoryNull, batchNull
}

// CategoriesSeededAtForTest reads the seeding marker of a user, nil when NULL (tests only).
func CategoriesSeededAtForTest(t testing.TB, s *Store, userID string) *string {
	t.Helper()
	var marker sql.Null[string]
	if err := s.db.QueryRowContext(t.Context(),
		`SELECT categories_seeded_at FROM users WHERE id = ?`, userID).Scan(&marker); err != nil {
		t.Fatalf("read seeding marker: %v", err)
	}
	return ptr(marker)
}

// CategoryRowForTest is a row of categories as stored (tests only).
type CategoryRowForTest struct {
	ID             int64
	UserID         string
	Name           string
	Description    *string
	ParentID       *int64
	Status         string
	ProposalReason *string
	Color          *string
	CreatedAt      string
	UpdatedAt      string
	Deleted        bool
	ChangeSeq      int64
}

// CategoryRowsForTest reads every row of categories of a user, deleted ones included, by id
// (tests only).
func CategoryRowsForTest(t testing.TB, s *Store, userID string) []CategoryRowForTest {
	t.Helper()
	rows, err := s.db.QueryContext(t.Context(),
		`SELECT id, user_id, name, description, parent_id, status, proposal_reason, color, created_at, updated_at,
		   deleted_at IS NOT NULL, change_seq
		 FROM categories WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		t.Fatalf("read categories: %v", err)
	}
	defer func() { _ = rows.Close() }() // read only
	var categories []CategoryRowForTest
	for rows.Next() {
		var (
			row                                CategoryRowForTest
			description, proposalReason, color sql.Null[string]
			parentID                           sql.Null[int64]
		)
		if err := rows.Scan(&row.ID, &row.UserID, &row.Name, &description, &parentID, &row.Status, &proposalReason,
			&color, &row.CreatedAt, &row.UpdatedAt, &row.Deleted, &row.ChangeSeq); err != nil {
			t.Fatalf("read categories: %v", err)
		}
		row.Description, row.ParentID, row.ProposalReason, row.Color = ptr(description), ptr(parentID), ptr(proposalReason), ptr(color)
		categories = append(categories, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read categories: %v", err)
	}
	return categories
}
