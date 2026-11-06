package extraction_test

import (
	"io"
	"testing"
)

func TestTempDirNewFile(t *testing.T) {
	t.Run("the directory stays empty after creation and after close", func(t *testing.T) {
		dir, path := tempDir(t)
		w, r, err := dir.NewFile()
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}
		if names := entries(t, path); len(names) != 0 {
			t.Errorf("directory holds %v after creation, want the file without name", names)
		}
		info, err := w.Stat()
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v (%v), want 0600", info.Mode().Perm(), err)
		}
		if _, err := w.WriteString("fictional batch body"); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := io.ReadAll(r)
		if err != nil || string(got) != "fictional batch body" {
			t.Errorf("read back %q, %v; want what was written", got, err)
		}
		if _, err := r.WriteString("x"); err == nil {
			t.Error("the reading end accepts a write, want it read-only")
		}
		if err := w.Close(); err != nil {
			t.Errorf("close writer: %v", err)
		}
		if err := r.Close(); err != nil {
			t.Errorf("close reader: %v", err)
		}
		if names := entries(t, path); len(names) != 0 {
			t.Errorf("directory holds %v after close, want nothing", names)
		}
	})
}
