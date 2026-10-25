package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SQLite silently ignores an unknown pragma: check that the DSN really configures every
// connection (docs/dev/conventions-backend.md, "Stockage SQLite").
func TestConnectionSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "with ? # % in the path")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	st, err := Open(t.Context(), filepath.Join(dir, "hippo.db"))
	if err != nil {
		t.Fatalf("Open with special characters in the path: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := os.Stat(filepath.Join(dir, "hippo.db")); err != nil {
		t.Fatalf("database not created at the requested path: %v", err)
	}

	for _, tc := range []struct {
		pragma string
		want   string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
		{"synchronous", "1"}, // NORMAL
	} {
		t.Run("sets "+tc.pragma, func(t *testing.T) {
			var got string
			if err := st.db.QueryRowContext(t.Context(), "PRAGMA "+tc.pragma).Scan(&got); err != nil {
				t.Fatalf("PRAGMA %s: %v", tc.pragma, err)
			}
			if got != tc.want {
				t.Errorf("PRAGMA %s = %q, want %q", tc.pragma, got, tc.want)
			}
		})
	}

	t.Run("starts write transactions immediately", func(t *testing.T) {
		if got := dsn("/data/hippo.db"); !strings.Contains(got, "_txlock=immediate") {
			t.Errorf("dsn() = %q, want _txlock=immediate", got)
		}
	})
}
