package store_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// snapshotSource is a store on a new database with a user and one committed note.
func snapshotSource(t *testing.T) (st *store.Store, path, userID string) {
	t.Helper()
	st, path = storetest.OpenWithPath(t)
	userID, err := st.CreateUser(context.Background(), "owner", "hash-owner")
	if err != nil {
		t.Fatal(err)
	}
	note := "A committed note"
	if _, _, err := st.CaptureItem(context.Background(), userID, store.NewItem{
		Kind: "note", Source: "share", Status: "queued", Title: "A committed note", UserNote: &note,
	}, nil); err != nil {
		t.Fatal(err)
	}
	return st, path, userID
}

func TestSnapshot(t *testing.T) {
	t.Run("copies the committed rows only while a write is in progress", func(t *testing.T) {
		st, path, userID := snapshotSource(t)
		rollback := store.HoldCaptureForTest(t, st, userID, store.NewItem{
			Kind: "note", Source: "share", Status: "queued", Title: "An uncommitted note",
		})
		out := filepath.Join(t.TempDir(), "copy.db")
		version, err := store.Snapshot(context.Background(), path, out)
		rollback()
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if got := storetest.IntegrityCheck(t, out); got != "ok" {
			t.Errorf("integrity check of the copy = %q, want ok", got)
		}
		if n := storetest.ItemCount(t, out, userID); n != 1 {
			t.Errorf("the copy holds %d items, want the committed one", n)
		}
		if want := storetest.MigrationVersion(t, path); version != want || version == 0 {
			t.Errorf("Snapshot version = %d, want %d", version, want)
		}
	})

	t.Run("leaves the database and its write-ahead log byte for byte", func(t *testing.T) {
		_, path, _ := snapshotSource(t)
		before := readFiles(t, path, path+"-wal")
		if len(before[path+"-wal"]) == 0 {
			t.Fatal("the write-ahead log is empty: the test would prove nothing")
		}
		if _, err := store.Snapshot(context.Background(), path, filepath.Join(t.TempDir(), "copy.db")); err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		after := readFiles(t, path, path+"-wal")
		for name, content := range before {
			if !bytes.Equal(content, after[name]) {
				t.Errorf("%s changed during the snapshot", filepath.Base(name))
			}
		}
	})

	t.Run("does not migrate a database behind the binary", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hippo.db")
		storetest.CreateAtVersion(t, path, 5)
		version, err := store.Snapshot(context.Background(), path, filepath.Join(t.TempDir(), "copy.db"))
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}
		if version != 5 {
			t.Errorf("Snapshot version = %d, want 5", version)
		}
		if got := storetest.MigrationVersion(t, path); got != 5 {
			t.Errorf("the database is at version %d after the snapshot, want 5", got)
		}
	})

	t.Run("refuses a target that already exists", func(t *testing.T) {
		_, path, _ := snapshotSource(t)
		out := filepath.Join(t.TempDir(), "copy.db")
		if err := os.WriteFile(out, []byte("not a database"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Snapshot(context.Background(), path, out); err == nil {
			t.Fatal("Snapshot over an existing file: want an error")
		}
		if got, _ := os.ReadFile(out); string(got) != "not a database" {
			t.Errorf("the existing file was changed")
		}
	})

	t.Run("refuses a database that does not exist, without creating it", func(t *testing.T) {
		dir := t.TempDir()
		_, err := store.Snapshot(context.Background(), filepath.Join(dir, "hippo.db"), filepath.Join(dir, "copy.db"))
		if err == nil {
			t.Fatal("Snapshot of a missing database: want an error")
		}
		if strings.Contains(err.Error(), dir) {
			t.Errorf("error %q names the path", err)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("the snapshot left %d files, want none", len(entries))
		}
	})

	t.Run("the snapshot connection cannot write", func(t *testing.T) {
		_, path, _ := snapshotSource(t)
		db := store.SnapshotDBForTest(t, path)
		if _, err := db.ExecContext(context.Background(), `UPDATE sync_sequence SET value = value + 1`); err == nil {
			t.Error("a write through the snapshot connection succeeded")
		}
	})
}

func readFiles(t *testing.T, paths ...string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte, len(paths))
	for _, p := range paths {
		content, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files[p] = content
	}
	return files
}

func TestSnapshotErrors(t *testing.T) {
	t.Run("an error of the copy names no file", func(t *testing.T) {
		_, path, _ := snapshotSource(t)
		dir := t.TempDir()
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		out := filepath.Join(dir, "copy.db")
		_, err := store.Snapshot(context.Background(), path, out)
		if err == nil {
			t.Fatal("Snapshot into a read-only folder: want an error")
		}
		for _, p := range []string{out, dir, path, filepath.Dir(path)} {
			if strings.Contains(err.Error(), p) {
				t.Errorf("error %q names %s", err, p)
			}
		}
	})
}
