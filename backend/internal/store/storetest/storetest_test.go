package storetest_test

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// readOne reads one value by SQL through a read-only connection to the database at path.
func readOne(t *testing.T, path, query string, dest any, args ...any) {
	t.Helper()
	uri := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer func() { _ = db.Close() }() // read only
	if err := db.QueryRowContext(context.Background(), query, args...).Scan(dest); err != nil {
		t.Fatalf("read %q: %v", query, err)
	}
}

func TestBatchHelpers(t *testing.T) {
	ctx := context.Background()

	t.Run("InsertBatch then SetBatchState write the status and the cost of the user's batch", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		userID, err := st.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		const batchID = "01JB8Z6Q4M1X2Y3Z4A5B6C7DA0"
		created := time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
		storetest.InsertBatch(t, path, userID, storetest.Batch{ID: batchID, Status: "pending", ItemCount: 2, CreatedAt: created})

		var state string
		readOne(t, path, `SELECT status || '|' || COALESCE(cost_usd, 'null') || '|' || created_at FROM batches WHERE user_id = ? AND id = ?`,
			&state, userID, batchID)
		if state != "pending|null|2026-11-03T01:00:00.000Z" {
			t.Errorf("inserted batch = %s, want pending without cost, created at 01:00", state)
		}

		cost := 0.42
		storetest.SetBatchState(t, path, userID, batchID, "ended", &cost)
		readOne(t, path, `SELECT status || '|' || cost_usd FROM batches WHERE user_id = ? AND id = ?`, &state, userID, batchID)
		if state != "ended|0.42" {
			t.Errorf("batch after SetBatchState = %s, want ended|0.42", state)
		}
	})
}

// ownerWithItem opens a store with its single user and one captured note.
func ownerWithItem(t *testing.T) (st *store.Store, path, userID, itemID string) {
	t.Helper()
	ctx := context.Background()
	st, path = storetest.OpenWithPath(t)
	userID, err := st.EnsureSingleUser(ctx, "hash")
	if err != nil {
		t.Fatalf("EnsureSingleUser: %v", err)
	}
	text := "A note"
	item, _, err := st.CaptureItem(ctx, userID,
		store.NewItem{Kind: "note", Source: "share", Status: "queued", Title: text, UserNote: &text}, nil)
	if err != nil {
		t.Fatalf("CaptureItem: %v", err)
	}
	return st, path, userID, item.ID
}

func TestAnalysisHelpers(t *testing.T) {
	ctx := context.Background()

	t.Run("SetUserEdited and SetLastError write the item with a new change_seq", func(t *testing.T) {
		st, path, userID, itemID := ownerWithItem(t)
		captured, err := st.Item(ctx, userID, itemID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		storetest.SetUserEdited(t, path, userID, itemID, "category", "tags")
		storetest.SetLastError(t, path, userID, itemID, "invalid_response")

		item, err := st.Item(ctx, userID, itemID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if len(item.UserEdited) != 2 || item.UserEdited[0] != "category" || item.UserEdited[1] != "tags" {
			t.Errorf("UserEdited = %v, want [category tags]", item.UserEdited)
		}
		if item.LastError == nil || *item.LastError != "invalid_response" {
			t.Errorf("LastError = %v, want invalid_response", item.LastError)
		}
		if item.ChangeSeq != captured.ChangeSeq+2 {
			t.Errorf("change_seq = %d, want %d", item.ChangeSeq, captured.ChangeSeq+2)
		}
	})

	t.Run("AddTag, AttachTag and DeleteTag write the tags of the user", func(t *testing.T) {
		st, path, userID, itemID := ownerWithItem(t)
		goID := storetest.AddTag(t, path, userID, "go")
		testingID := storetest.AddTag(t, path, userID, "testing")
		storetest.AttachTag(t, path, userID, itemID, testingID)
		storetest.AttachTag(t, path, userID, itemID, goID)
		storetest.DeleteTag(t, path, userID, testingID)

		item, err := st.Item(ctx, userID, itemID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if len(item.TagIDs) != 1 || item.TagIDs[0] != goID {
			t.Errorf("TagIDs = %v, want only the non-deleted go (%d)", item.TagIDs, goID)
		}
		var deleted bool
		readOne(t, path, `SELECT deleted_at IS NOT NULL FROM tags WHERE user_id = ? AND id = ?`, &deleted, userID, testingID)
		if !deleted {
			t.Error("tag testing is not deleted")
		}
	})

	t.Run("InsertBatch records the versions it is given, SetBatchEnvelopeVersion changes one", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		userID, err := st.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		const batchID = "01JB8Z6Q4M1X2Y3Z4A5B6C7DA1"
		storetest.InsertBatch(t, path, userID, storetest.Batch{
			ID: batchID, Status: "in_progress", ItemCount: 1, CreatedAt: time.Now(),
			Model: "claude-haiku-4-5", PromptVersion: 4,
		})
		storetest.SetBatchEnvelopeVersion(t, path, userID, batchID, 2)
		var versions string
		readOne(t, path, `SELECT model || '|' || prompt_version || '|' || envelope_version FROM batches WHERE user_id = ? AND id = ?`,
			&versions, userID, batchID)
		if versions != "claude-haiku-4-5|4|2" {
			t.Errorf("batch versions = %s, want claude-haiku-4-5|4|2", versions)
		}
	})
}
