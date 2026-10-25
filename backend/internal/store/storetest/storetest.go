// Package storetest opens real SQLite databases for tests: a fresh file per call, with every
// migration applied. Tests never mock the store (ADR-0004, docs/dev/testing.md).
package storetest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// Open returns a store on a new database in t.TempDir(), closed when the test ends.
func Open(t testing.TB) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "hippo.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return st
}
