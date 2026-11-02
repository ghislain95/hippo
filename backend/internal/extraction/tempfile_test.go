package extraction_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/extraction"
)

// entries lists the names in dir.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestTempDir(t *testing.T) {
	t.Run("the directory is created for the service only", func(t *testing.T) {
		_, path := tempDir(t)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("directory mode %v, want 0700", info.Mode().Perm())
		}
		if names := entries(t, path); len(names) != 0 {
			t.Errorf("directory holds %v after opening, want nothing: the probe is removed", names)
		}
	})

	t.Run("a file loses its name before it is used", func(t *testing.T) {
		dir, path := tempDir(t)
		w, r, closeBoth, err := extraction.CreateTempFile(dir)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer func() { _ = closeBoth() }()
		if names := entries(t, path); len(names) != 0 {
			t.Errorf("directory holds %v, want the file without name", names)
		}
		info, err := extraction.WriterFile(w).Stat()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v (%v), want 0600", info.Mode().Perm(), err)
		}
		if _, err := w.Write([]byte("%PDF-1.7 fictional")); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := io.ReadAll(io.NewSectionReader(r, 0, 1<<20))
		if err != nil || string(got) != "%PDF-1.7 fictional" {
			t.Errorf("read back %q, %v", got, err)
		}
		if _, err := r.Write([]byte("x")); err == nil {
			t.Error("the descriptor of the child can write, want read-only")
		}
		if err := closeBoth(); err != nil {
			t.Errorf("close: %v", err)
		}
		if err := closeBoth(); err != nil {
			t.Errorf("second close: %v", err)
		}
	})

	t.Run("Leftover temporary files are removed at start-up", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "tmp")
		if err := os.MkdirAll(filepath.Join(path, "kept-dir"), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{".pdf-0123456789abcdef", "other-leftover"} {
			if err := os.WriteFile(filepath.Join(path, name), []byte("%PDF-1.7"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		dir, removed, err := extraction.OpenTempDir(path)
		if err != nil {
			t.Fatalf("OpenTempDir: %v", err)
		}
		defer func() { _ = dir.Close() }()
		if removed != 2 {
			t.Errorf("removed %d files, want 2", removed)
		}
		if names := entries(t, path); len(names) != 1 || names[0] != "kept-dir" {
			t.Errorf("directory holds %v, want the sub-directory only", names)
		}
	})

	t.Run("an unwritable directory is refused at opening", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "tmp")
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		readOnly(t, path)
		_, _, err := extraction.OpenTempDir(path)
		if err == nil {
			t.Fatal("an unwritable directory was accepted")
		}
		if !errors.Is(err, fs.ErrPermission) || strings.Contains(err.Error(), path) {
			t.Errorf("error %q, want a permission error that names no path", err)
		}
	})
}

// readOnly makes dir unwritable until the end of the test. The tests do not run as root, for
// whom no directory is unwritable: a probe makes sure.
func readOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if probe, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		_ = probe.Close()
		t.Fatal("the directory is still writable: the tests must not run as root")
	}
}
