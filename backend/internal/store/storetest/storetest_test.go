package storetest_test

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

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
