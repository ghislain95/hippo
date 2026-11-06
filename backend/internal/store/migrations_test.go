package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// openVersion5 seeds a version-5 database (a user and items, as E2 left them) and opens the
// store on it, which applies every later migration.
func openVersion5(t *testing.T) (st *store.Store, userID string, itemIDs []string, sequence int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hippo.db")
	userID, itemIDs, sequence = store.SeedVersion5ForTest(t, path)
	st, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open (migrate to the latest version): %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, userID, itemIDs, sequence
}

func TestMigrationAnalysis(t *testing.T) {
	t.Run("seeds the seven starting content types in the order of metier.md", func(t *testing.T) {
		st, _, _, sequence := openVersion5(t)

		want := []struct{ key, name string }{
			{"tech_article", "Article technique"},
			{"tutorial", "Tuto"},
			{"tip", "Astuce"},
			{"recipe", "Recette"},
			{"place", "Lieu"},
			{"resource", "Ressource"},
			{"note", "Note perso"},
		}
		rows := store.ContentTypeRowsForTest(t, st)
		if len(rows) != len(want) {
			t.Fatalf("content types = %d rows, want %d", len(rows), len(want))
		}
		seen := map[int64]bool{}
		for i, row := range rows {
			if row.ID != int64(i+1) || row.Key != want[i].key || row.Name != want[i].name ||
				row.Status != "active" || row.HasTemplate != 1 || row.Deleted {
				t.Errorf("content type %d = %+v, want id %d %s %q active with a template", i, row, i+1, want[i].key, want[i].name)
			}
			if _, err := time.Parse(store.TimeLayout, row.UpdatedAt); err != nil {
				t.Errorf("content type %s updated_at %q is not in the storage format: %v", row.Key, row.UpdatedAt, err)
			}
			if row.ChangeSeq <= sequence || row.ChangeSeq > sequence+7 || seen[row.ChangeSeq] {
				t.Errorf("content type %s change_seq = %d, want a distinct value in (%d, %d]", row.Key, row.ChangeSeq, sequence, sequence+7)
			}
			seen[row.ChangeSeq] = true
		}
		if got := store.SequenceForTest(t, st); got != sequence+7 {
			t.Errorf("sequence after the migration = %d, want %d", got, sequence+7)
		}
	})

	t.Run("leaves the existing items unclassified and the user unseeded", func(t *testing.T) {
		st, userID, itemIDs, _ := openVersion5(t)

		for _, itemID := range itemIDs {
			typeNull, categoryNull, batchNull := store.ClassificationNullsForTest(t, st, userID, itemID)
			if !typeNull || !categoryNull || !batchNull {
				t.Errorf("item %s: type_id NULL %v, category_id NULL %v, batch_id NULL %v; want all NULL",
					itemID, typeNull, categoryNull, batchNull)
			}
		}
		if marker := store.CategoriesSeededAtForTest(t, st, userID); marker != nil {
			t.Errorf("categories_seeded_at = %q after the migration, want NULL", *marker)
		}
		if got := store.ItemCountForTest(t, st, userID); got != len(itemIDs) {
			t.Errorf("items after the migration = %d, want %d", got, len(itemIDs))
		}
	})
}
