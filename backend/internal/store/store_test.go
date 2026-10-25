package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

func TestOpen(t *testing.T) {
	ctx := context.Background()

	t.Run("creates the database and applies the migrations", func(t *testing.T) {
		st := storetest.Open(t)
		if err := st.Ping(ctx); err != nil {
			t.Fatalf("Ping: %v", err)
		}
		if _, err := st.EnsureSingleUser(ctx, "hash"); err != nil {
			t.Fatalf("EnsureSingleUser on a fresh database: %v", err)
		}
	})

	t.Run("reopens an existing database without losing data", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hippo.db")
		first, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		id, err := first.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		if err := first.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		second, err := store.Open(ctx, path)
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		t.Cleanup(func() { _ = second.Close() })
		again, err := second.EnsureSingleUser(ctx, "hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser after reopen: %v", err)
		}
		if again != id {
			t.Errorf("user id after reopen = %q, want %q", again, id)
		}
	})
}

func TestEnsureSingleUser(t *testing.T) {
	ctx := context.Background()

	t.Run("creates the user at first start and keeps it", func(t *testing.T) {
		st := storetest.Open(t)
		first, err := st.EnsureSingleUser(ctx, "hash-1")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		if len(first) != 26 {
			t.Errorf("user id %q is not a ULID", first)
		}
		second, err := st.EnsureSingleUser(ctx, "hash-1")
		if err != nil {
			t.Fatalf("EnsureSingleUser again: %v", err)
		}
		if second != first {
			t.Errorf("second call created another user: %q, want %q", second, first)
		}
	})

	t.Run("keeps the same user and stores the new hash when the token is rotated", func(t *testing.T) {
		st := storetest.Open(t)
		first, err := st.EnsureSingleUser(ctx, "old-hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser: %v", err)
		}
		rotated, err := st.EnsureSingleUser(ctx, "new-hash")
		if err != nil {
			t.Fatalf("EnsureSingleUser after rotation: %v", err)
		}
		if rotated != first {
			t.Errorf("rotation changed the user: %q, want %q", rotated, first)
		}
		if got := store.TokenHashForTest(t, st, first); got != "new-hash" {
			t.Errorf("stored token hash = %q, want new-hash", got)
		}
	})
}
