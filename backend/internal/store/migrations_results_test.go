package store_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/naming"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

func TestMigrationResults(t *testing.T) {
	ctx := context.Background()

	// migrated seeds a version-6 base and applies 00007 only, without the backfill of Open.
	migrated := func(t *testing.T) (*sql.DB, store.Version6ForTest) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "hippo.db")
		seed := store.SeedVersion6ForTest(t, path)
		return store.MigrateOnlyForTest(t, path), seed
	}

	t.Run("leaves the new columns NULL and the new tables empty", func(t *testing.T) {
		db, seed := migrated(t)

		for _, itemID := range seed.ItemIDs {
			var envelopeNull bool
			if err := db.QueryRowContext(ctx, `SELECT envelope_version IS NULL FROM items WHERE user_id = ? AND id = ?`,
				seed.UserID, itemID).Scan(&envelopeNull); err != nil {
				t.Fatalf("read item %s: %v", itemID, err)
			}
			if !envelopeNull {
				t.Errorf("item %s envelope_version is not NULL", itemID)
			}
		}
		var keys, nullKeys int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) - COUNT(name_key) FROM categories WHERE user_id = ?`,
			seed.UserID).Scan(&keys, &nullKeys); err != nil {
			t.Fatalf("read categories: %v", err)
		}
		if keys != 24 || nullKeys != 24 {
			t.Errorf("categories = %d, %d without name_key; want 24, all without", keys, nullKeys)
		}
		var unavailableNull, linesNull bool
		if err := db.QueryRowContext(ctx,
			`SELECT results_unavailable_at IS NULL, results_lines_read IS NULL FROM batches WHERE user_id = ? AND id = ?`,
			seed.UserID, seed.BatchID).Scan(&unavailableNull, &linesNull); err != nil {
			t.Fatalf("read batch: %v", err)
		}
		if !unavailableNull || !linesNull {
			t.Errorf("batch results_unavailable_at NULL %v, results_lines_read NULL %v; want both NULL", unavailableNull, linesNull)
		}
		for _, table := range []string{"tags", "item_tags"} {
			var count int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			if count != 0 {
				t.Errorf("%s holds %d rows, want 0", table, count)
			}
		}
	})

	t.Run("refuses two non-deleted roots of the same normalised name, accepts one when the other is deleted", func(t *testing.T) {
		db, seed := migrated(t)
		insertRoot := func(name, key string, deleted bool) error {
			var deletedAt any
			if deleted {
				deletedAt = "2026-11-07T10:00:00.000Z"
			}
			_, err := db.ExecContext(ctx,
				`INSERT INTO categories (user_id, name, name_key, status, created_at, updated_at, deleted_at, change_seq)
				 VALUES (?, ?, ?, 'proposed', '2026-11-07T10:00:00.000Z', '2026-11-07T10:00:00.000Z', ?, 100)`,
				seed.UserID, name, key, deletedAt)
			return err
		}
		if err := insertRoot("Photo", "photo", false); err != nil {
			t.Fatalf("first root: %v", err)
		}
		if err := insertRoot("PHOTO", "photo", false); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
			t.Errorf("second non-deleted root of the same key: err = %v, want a UNIQUE violation", err)
		}
		if err := insertRoot("Photo", "photo", true); err != nil {
			t.Errorf("a deleted root of the same key: %v, want accepted", err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE categories SET deleted_at = '2026-11-07T11:00:00.000Z' WHERE user_id = ? AND name = 'Photo' AND deleted_at IS NULL`,
			seed.UserID); err != nil {
			t.Fatalf("delete first root: %v", err)
		}
		if err := insertRoot("Photo", "photo", false); err != nil {
			t.Errorf("a root of the same key once the first is deleted: %v, want accepted", err)
		}
		// The same key under a parent, or for another user, is another category.
		if _, err := db.ExecContext(ctx,
			`INSERT INTO categories (user_id, name, name_key, parent_id, status, created_at, updated_at, change_seq)
			 VALUES (?, 'Photo', 'photo', ?, 'proposed', '2026-11-07T10:00:00.000Z', '2026-11-07T10:00:00.000Z', 101)`,
			seed.UserID, seed.CategoryIDs["Dev"]); err != nil {
			t.Errorf("the same key under a parent: %v, want accepted", err)
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO categories (user_id, name, name_key, status, created_at, updated_at, change_seq)
			 VALUES (?, 'Photo', 'photo', 'proposed', '2026-11-07T10:00:00.000Z', '2026-11-07T10:00:00.000Z', 102)`,
			seed.OtherUserID); err != nil {
			t.Errorf("the same key for another user: %v, want accepted", err)
		}
	})

	t.Run("refuses a tag of another user on an item, and two non-deleted tags of one name", func(t *testing.T) {
		db, seed := migrated(t)
		insertTag := func(userID, name string) (int64, error) {
			var id int64
			err := db.QueryRowContext(ctx,
				`INSERT INTO tags (user_id, name, updated_at, change_seq) VALUES (?, ?, '2026-11-07T10:00:00.000Z', 100) RETURNING id`,
				userID, name).Scan(&id)
			return id, err
		}
		own, err := insertTag(seed.UserID, "go")
		if err != nil {
			t.Fatalf("own tag: %v", err)
		}
		other, err := insertTag(seed.OtherUserID, "go")
		if err != nil {
			t.Fatalf("the same name for another user: %v, want accepted", err)
		}
		if _, err := insertTag(seed.UserID, "go"); err == nil {
			t.Errorf("a second non-deleted tag go of the user was accepted")
		}
		if _, err := insertTag(seed.UserID, strings.Repeat("a", 41)); err == nil {
			t.Errorf("a tag of 41 characters was accepted")
		}
		attach := func(tagID int64, userID string) error {
			_, err := db.ExecContext(ctx, `INSERT INTO item_tags (item_id, tag_id, user_id) VALUES (?, ?, ?)`,
				seed.ItemIDs[0], tagID, userID)
			return err
		}
		if err := attach(other, seed.UserID); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Errorf("another user's tag under the item's user: err = %v, want a FOREIGN KEY violation", err)
		}
		if err := attach(other, seed.OtherUserID); err == nil || !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Errorf("another user's tag under that user: err = %v, want a FOREIGN KEY violation", err)
		}
		if err := attach(own, seed.UserID); err != nil {
			t.Errorf("the user's own tag: %v, want accepted", err)
		}
	})
}

func TestCategoryKeysBackfill(t *testing.T) {
	ctx := context.Background()

	t.Run("a version-6 base gets the 24 normalised names once", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hippo.db")
		seed := store.SeedVersion6ForTest(t, path)
		st, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if got := st.CategoryKeysFilled(); got != 24 {
			t.Errorf("CategoryKeysFilled = %d, want 24", got)
		}
		keys, nulls := store.CategoryKeysForTest(t, st, seed.UserID)
		if nulls != 0 || len(keys) != 24 {
			t.Fatalf("keys = %d, %d NULL; want 24, none NULL", len(keys), nulls)
		}
		for name, want := range map[string]string{
			"Ménage": "menage", "Java & Spring": "java & spring", "Jeux vidéo": "jeux video",
			"Auto-hébergement": "auto-hebergement", "CI/CD": "ci/cd", "Dev": "dev",
		} {
			if got := keys[seed.CategoryIDs[name]]; got != want {
				t.Errorf("name_key of %q = %q, want %q", name, got, want)
			}
		}
		sequence := store.SequenceForTest(t, st)
		rows := store.CategoryRowsForTest(t, st, seed.UserID)
		if err := st.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if sequence != 9+24 {
			t.Errorf("sequence after the backfill = %d, want %d: the backfill advances no change_seq", sequence, 9+24)
		}

		again, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("second Open: %v", err)
		}
		t.Cleanup(func() { _ = again.Close() })
		if got := again.CategoryKeysFilled(); got != 0 {
			t.Errorf("CategoryKeysFilled at the second open = %d, want 0", got)
		}
		after := store.CategoryRowsForTest(t, again, seed.UserID)
		for i := range rows {
			if rows[i].ChangeSeq != after[i].ChangeSeq || rows[i].UpdatedAt != after[i].UpdatedAt {
				t.Errorf("category %d changed at the second open: change_seq %d then %d, updated_at %s then %s",
					rows[i].ID, rows[i].ChangeSeq, after[i].ChangeSeq, rows[i].UpdatedAt, after[i].UpdatedAt)
			}
		}
		if got := store.SequenceForTest(t, again); got != sequence {
			t.Errorf("sequence after the second open = %d, want %d", got, sequence)
		}
	})

	t.Run("a collision of normalised names stops the open and names both ids", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hippo.db")
		seed := store.SeedVersion6ForTest(t, path,
			`INSERT INTO categories (user_id, name, parent_id, status, created_at, updated_at, change_seq)
			 SELECT user_id, 'menage', id, 'active', created_at, updated_at, 100 FROM categories WHERE name = 'Maison & quotidien'`)
		st, err := store.Open(ctx, path)
		if err == nil {
			_ = st.Close()
			t.Fatal("Open succeeded, want an error")
		}
		menage := strconv.FormatInt(seed.CategoryIDs["Ménage"], 10)
		if !strings.Contains(err.Error(), menage) || !strings.Contains(err.Error(), "25") {
			t.Errorf("Open error = %q, want it to name the ids %s and 25", err, menage)
		}
		if strings.Contains(err.Error(), "enage") {
			t.Errorf("Open error = %q names a category, want ids only", err)
		}
	})

	t.Run("the starting categories of a new user carry their normalised name", func(t *testing.T) {
		st := storetest.Open(t)
		userID, err := st.CreateUser(ctx, "second", "hash")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		keys, nulls := store.CategoryKeysForTest(t, st, userID)
		if nulls != 0 || len(keys) != 24 {
			t.Fatalf("keys = %d, %d NULL; want 24, none NULL", len(keys), nulls)
		}
		for _, row := range store.CategoryRowsForTest(t, st, userID) {
			if want := naming.CategoryKey(row.Name); keys[row.ID] != want {
				t.Errorf("name_key of category %d = %q, want %q", row.ID, keys[row.ID], want)
			}
		}
		if got := st.CategoryKeysFilled(); got != 0 {
			t.Errorf("CategoryKeysFilled of a new database = %d, want 0", got)
		}
	})

	t.Run("a category added by the test helper carries its normalised name", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		userID, err := st.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		id := storetest.AddCategory(t, path, userID, storetest.NewCategory{Name: "  Bonsaï  Zen ", Status: "proposed"})
		if keys, _ := store.CategoryKeysForTest(t, st, userID); keys[id] != "bonsai zen" {
			t.Errorf("name_key = %q, want %q", keys[id], "bonsai zen")
		}
	})

	t.Run("a deleted category of the same name does not collide", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hippo.db")
		seed := store.SeedVersion6ForTest(t, path,
			`INSERT INTO categories (user_id, name, parent_id, status, created_at, updated_at, deleted_at, change_seq)
			 SELECT user_id, 'menage', id, 'active', created_at, updated_at, updated_at, 100 FROM categories WHERE name = 'Maison & quotidien'`)
		st, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })
		if got := st.CategoryKeysFilled(); got != 25 {
			t.Errorf("CategoryKeysFilled = %d, want 25 (the deleted one included)", got)
		}
		if _, nulls := store.CategoryKeysForTest(t, st, seed.UserID); nulls != 0 {
			t.Errorf("%d categories without name_key, want 0", nulls)
		}
	})
}
