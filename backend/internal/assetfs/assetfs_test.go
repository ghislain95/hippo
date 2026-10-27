package assetfs_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/assetfs"
)

func openDir(t *testing.T) (*assetfs.Dir, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "assets")
	dir, err := assetfs.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := dir.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return dir, path
}

// files lists every file under root, relative to it, sorted.
func files(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

func upload(t *testing.T, dir *assetfs.Dir, content string) *assetfs.Upload {
	t.Helper()
	up, err := dir.CreateTemp()
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	t.Cleanup(func() { _ = up.Remove() }) // the test checks Remove where it matters
	if _, err := io.Copy(up, strings.NewReader(content)); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	return up
}

func assertNoAccessForOthers(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	// The umask may only remove bits: nothing beyond want, and nothing for others.
	if perm := info.Mode().Perm(); perm&^want != 0 || perm&0o007 != 0 {
		t.Errorf("%s mode = %v, want %v at most", path, perm, want)
	}
}

func TestOpen(t *testing.T) {
	t.Run("creates the assets directory", func(t *testing.T) {
		_, path := openDir(t)
		assertNoAccessForOthers(t, path, 0o750)
	})

	t.Run("checks that the directory is writable and leaves nothing", func(t *testing.T) {
		dir, path := openDir(t)
		if err := dir.CheckWritable(); err != nil {
			t.Fatalf("CheckWritable: %v", err)
		}
		if got := files(t, path); len(got) != 0 {
			t.Errorf("files = %v, want none", got)
		}
	})
}

func TestUpload(t *testing.T) {
	t.Run("writes a temporary file in the assets directory", func(t *testing.T) {
		dir, path := openDir(t)
		up := upload(t, dir, "hello image")
		got := files(t, path)
		if len(got) != 1 || !strings.HasPrefix(got[0], ".tmp-") {
			t.Fatalf("files = %v, want one .tmp-* file", got)
		}
		assertNoAccessForOthers(t, filepath.Join(path, got[0]), 0o640)
		if up.Size() != int64(len("hello image")) {
			t.Errorf("Size = %d, want %d", up.Size(), len("hello image"))
		}
		head := make([]byte, 5)
		if _, err := up.ReadAt(head, 6); err != nil || string(head) != "image" {
			t.Errorf("ReadAt = %q, %v; want image", head, err)
		}
	})

	t.Run("two uploads get two files", func(t *testing.T) {
		dir, path := openDir(t)
		upload(t, dir, "a")
		upload(t, dir, "b")
		if got := files(t, path); len(got) != 2 {
			t.Errorf("files = %v, want two", got)
		}
	})

	t.Run("commit renames the file into place, unchanged", func(t *testing.T) {
		dir, path := openDir(t)
		up := upload(t, dir, "hello image")
		if err := up.Commit("01JB8Z6Q4M1X2Y3Z4A5B6C7D80/01JB8Z6Q4M1X2Y3Z4A5B6C7D81.png"); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if got := files(t, path); len(got) != 1 || got[0] != "01JB8Z6Q4M1X2Y3Z4A5B6C7D80/01JB8Z6Q4M1X2Y3Z4A5B6C7D81.png" {
			t.Fatalf("files = %v, want only the committed file", got)
		}
		assertNoAccessForOthers(t, filepath.Join(path, "01JB8Z6Q4M1X2Y3Z4A5B6C7D80"), 0o750)
		raw, err := os.ReadFile(filepath.Join(path, "01JB8Z6Q4M1X2Y3Z4A5B6C7D80", "01JB8Z6Q4M1X2Y3Z4A5B6C7D81.png"))
		if err != nil || string(raw) != "hello image" {
			t.Errorf("committed file = %q, %v; want hello image", raw, err)
		}
	})

	t.Run("commit refuses a path outside the directory or a temporary name", func(t *testing.T) {
		dir, path := openDir(t)
		for _, rel := range []string{"../escape.png", "/abs.png", "", ".tmp-x", "a/../../b.png"} {
			up := upload(t, dir, "x")
			if err := up.Commit(rel); err == nil {
				t.Errorf("Commit(%q) succeeded, want an error", rel)
			}
			if err := up.Remove(); err != nil {
				t.Errorf("Remove after a refused commit: %v", err)
			}
		}
		if got := files(t, path); len(got) != 0 {
			t.Errorf("files = %v, want none", got)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), "escape.png")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a file escaped the directory: %v", err)
		}
	})

	t.Run("remove deletes the temporary file", func(t *testing.T) {
		dir, path := openDir(t)
		up := upload(t, dir, "x")
		if err := up.Remove(); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if err := up.Remove(); err != nil {
			t.Errorf("second Remove: %v", err)
		}
		if _, err := up.Write([]byte("x")); err == nil || strings.Contains(err.Error(), ".tmp-") {
			t.Errorf("Write after Remove = %v, want an error without the file name", err)
		}
		if got := files(t, path); len(got) != 0 {
			t.Errorf("files = %v, want none", got)
		}
	})

	t.Run("remove deletes the committed file", func(t *testing.T) {
		dir, path := openDir(t)
		up := upload(t, dir, "x")
		if err := up.Commit("u/a.png"); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		if err := up.Remove(); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if got := files(t, path); len(got) != 0 {
			t.Errorf("files = %v, want none", got)
		}
	})

	t.Run("a kept file is not removed", func(t *testing.T) {
		dir, path := openDir(t)
		up := upload(t, dir, "x")
		if err := up.Commit("u/a.png"); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		up.Keep()
		if err := up.Remove(); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if got := files(t, path); len(got) != 1 {
			t.Errorf("files = %v, want the kept file", got)
		}
	})
}

func TestDirOpen(t *testing.T) {
	t.Run("opens a committed file", func(t *testing.T) {
		dir, _ := openDir(t)
		up := upload(t, dir, "hello image")
		if err := up.Commit("u/a.png"); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		file, err := dir.Open("u/a.png")
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer func() { _ = file.Close() }()
		raw, err := io.ReadAll(file)
		if err != nil || !bytes.Equal(raw, []byte("hello image")) {
			t.Errorf("read = %q, %v", raw, err)
		}
	})

	t.Run("never leaves the directory", func(t *testing.T) {
		dir, path := openDir(t)
		outside := filepath.Join(filepath.Dir(path), "outside.png")
		if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
			t.Fatalf("write outside file: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(path, "link.png")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		for _, rel := range []string{"../outside.png", outside, "link.png"} {
			if file, err := dir.Open(rel); err == nil {
				_ = file.Close()
				t.Errorf("Open(%q) succeeded, want an error", rel)
			}
		}
	})

	t.Run("a missing file is not found", func(t *testing.T) {
		dir, _ := openDir(t)
		_, err := dir.Open("u/missing.png")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Open = %v, want fs.ErrNotExist", err)
		}
		if err != nil && strings.Contains(err.Error(), "missing.png") {
			t.Errorf("error %q names the file", err)
		}
	})
}

func TestErrorsNameNoFile(t *testing.T) {
	t.Run("a failed commit does not name the file", func(t *testing.T) {
		dir, path := openDir(t)
		// A file where the user directory should be: the commit cannot create it.
		if err := os.WriteFile(filepath.Join(path, "u"), []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		up := upload(t, dir, "x")
		err := up.Commit("u/a.png")
		if err == nil {
			t.Fatal("Commit under a file succeeded")
		}
		if msg := err.Error(); strings.Contains(msg, "a.png") || strings.Contains(msg, ".tmp-") || strings.Contains(msg, path) {
			t.Errorf("error %q names a file", msg)
		}
	})
}

func TestRemoveTemps(t *testing.T) {
	t.Run("removes the leftover temporary files only", func(t *testing.T) {
		dir, path := openDir(t)
		kept := upload(t, dir, "kept")
		if err := kept.Commit("u/a.png"); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		kept.Keep()
		upload(t, dir, "left over")
		upload(t, dir, "left over too")
		if err := os.WriteFile(filepath.Join(path, "u", ".tmp-not-top-level"), []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}

		removed, err := dir.RemoveTemps()
		if err != nil {
			t.Fatalf("RemoveTemps: %v", err)
		}
		if removed != 2 {
			t.Errorf("removed %d files, want 2", removed)
		}
		if got := files(t, path); len(got) != 2 || got[0] != "u/.tmp-not-top-level" || got[1] != "u/a.png" {
			t.Errorf("files = %v, want the committed file and the nested one", got)
		}
	})
}
