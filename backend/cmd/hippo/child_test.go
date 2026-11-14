package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/imagecopy"
)

// TestMain makes the test binary the binary of production when a test runs it with
// imagecopy.ChildCommand or backupCommandName: main dispatches it. main returns on a
// successful backup, as the real binary ends with exit code 0: never run the tests then.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == imagecopy.ChildCommand || os.Args[1] == backupCommandName) {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCopyImageCommand(t *testing.T) {
	t.Run("the subcommand runs without any environment variable", func(t *testing.T) {
		// The child gets GOMAXPROCS=2 alone (childproc.Env): no API_TOKEN, no DATA_DIR. Had
		// main loaded the configuration first, the child would exit 1 and the copy fail.
		dir := t.TempDir()
		path := filepath.Join(dir, "asset.png")
		if err := os.WriteFile(path, imagetest.PNG(64, 32), 0o600); err != nil {
			t.Fatal(err)
		}
		asset, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = asset.Close() }()
		out, err := os.Create(filepath.Join(dir, "copy.jpg"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = out.Close() }()

		copier := imagecopy.NewCopier([]string{os.Args[0], imagecopy.ChildCommand}, imagecopy.DefaultLimits())
		res, err := copier.Copy(context.Background(), asset, out)
		if err != nil || res.Reason != imagecopy.ReasonOK || res.Width != 64 || res.Height != 32 {
			t.Errorf("Copy: %+v, %v; want ok 64×32", res, err)
		}
	})
}
