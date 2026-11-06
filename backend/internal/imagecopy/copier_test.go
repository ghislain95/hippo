package imagecopy_test

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/imagecopy"
)

// Modes of the test binary when a test runs it as a child, besides imagecopy.ChildCommand.
const (
	hangChild    = "hang-child"    // says it started on the FIFO given, then never answers
	garbageChild = "garbage-child" // answers something that is not an answer
	liarChild    = "liar-child"    // answers ok and writes no copy
	crashChild   = "crash-child"   // exits with an unexpected code
	sizeChild    = "size-child"    // writes a JPEG of 8×8 and announces 10×10
)

// TestMain makes the test binary the child of the tests.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		runChildMode(os.Args[1], os.Args[2:])
	}
	os.Exit(m.Run())
}

// runChildMode runs the test binary as the child of mode; it returns only for an unknown mode.
// syscall.Exit, not os.Exit: under -race, a test binary that exits with 0 through os.Exit waits
// a second for the race detector.
func runChildMode(mode string, args []string) {
	switch mode {
	case imagecopy.ChildCommand:
		syscall.Exit(imagecopy.RunChild(os.Stdin, os.Stdout))
	case hangChild:
		if fifo, err := os.OpenFile(args[0], os.O_WRONLY, 0); err == nil {
			_, _ = fifo.WriteString("started\n")
			_ = fifo.Close()
		}
		never := make(chan os.Signal, 1)
		signal.Notify(never, syscall.SIGUSR2)
		<-never
		syscall.Exit(0)
	case garbageChild:
		_, _ = os.Stdout.WriteString("{\"reason\":\"maybe\"}\n")
		syscall.Exit(0)
	case liarChild:
		_, _ = os.Stdout.WriteString("{\"reason\":\"ok\",\"width\":10,\"height\":10}\n")
		syscall.Exit(0)
	case crashChild:
		syscall.Exit(7)
	case sizeChild:
		if out := os.NewFile(4, "copy"); out != nil {
			_, _ = out.Write(imagetest.JPEG(8, 8))
		}
		_, _ = os.Stdout.WriteString("{\"reason\":\"ok\",\"width\":10,\"height\":10}\n")
		syscall.Exit(0)
	}
}

func argv(mode string, args ...string) []string {
	return append([]string{os.Args[0], mode}, args...)
}

// files writes content to a file of the test's directory and opens it read-only, and opens an
// empty output file; both are closed at the end of the test.
func files(t *testing.T, content []byte) (asset, out *os.File) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "asset")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	asset, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	out, err = os.OpenFile(filepath.Join(dir, "copy"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = asset.Close()
		_ = out.Close()
	})
	return asset, out
}

// startedFIFO makes a FIFO that a hang child writes to once started; the channel is closed
// then.
func startedFIFO(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "started")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	started := make(chan struct{})
	go func() {
		fifo, err := os.Open(path)
		if err != nil {
			return
		}
		defer func() { _ = fifo.Close() }()
		if _, err := bufio.NewReader(fifo).ReadString('\n'); err == nil {
			close(started)
		}
	}()
	t.Cleanup(func() {
		if fifo, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = fifo.Close()
		}
	})
	return path, started
}

func TestCopier(t *testing.T) {
	ctx := context.Background()
	copier := imagecopy.NewCopier(argv(imagecopy.ChildCommand), imagecopy.DefaultLimits())

	t.Run("the child writes the copy to the output file", func(t *testing.T) {
		asset, out := files(t, imagetest.QuadrantJPEG(4000, 3000))
		res, err := copier.Copy(ctx, asset, out)
		if err != nil || res.Reason != imagecopy.ReasonOK || res.Width != 1568 || res.Height != 1176 {
			t.Fatalf("Copy: %+v, %v; want ok 1568×1176", res, err)
		}
		info, err := out.Stat()
		if err != nil || info.Size() != res.Size || res.Size == 0 {
			t.Fatalf("output of %v bytes (%v), result says %d", info.Size(), err, res.Size)
		}
		img, err := jpeg.Decode(io.NewSectionReader(out, 0, res.Size))
		if err != nil || img.Bounds() != image.Rect(0, 0, 1568, 1176) {
			t.Errorf("copy %v, %v; want a 1568×1176 JPEG", img, err)
		}
		if res.PeakBytes == 0 {
			t.Error("no peak reported")
		}
	})

	t.Run("an output file used before is rewritten from its start", func(t *testing.T) {
		asset, out := files(t, imagetest.PNG(64, 32))
		if _, err := out.WriteString("bytes of a previous, larger copy that must not stay"); err != nil {
			t.Fatal(err)
		}
		res, err := copier.Copy(ctx, asset, out)
		info, statErr := out.Stat()
		if err != nil || statErr != nil || res.Reason != imagecopy.ReasonOK || info.Size() != res.Size {
			t.Fatalf("Copy: %+v, %v; output of %d bytes", res, err, info.Size())
		}
		if _, err := jpeg.Decode(io.NewSectionReader(out, 0, res.Size)); err != nil {
			t.Errorf("decode the copy: %v", err)
		}
	})

	t.Run("the stored file is left untouched", func(t *testing.T) {
		content := imagetest.WithEXIF(imagetest.QuadrantJPEG(400, 200), imagetest.EXIF(binary.BigEndian, 6))
		asset, out := files(t, content)
		if _, err := copier.Copy(ctx, asset, out); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(asset.Name())
		if err != nil || string(after) != string(content) {
			t.Errorf("stored file changed (%v)", err)
		}
	})

	reasons := []struct {
		name    string
		content []byte
		want    string
	}{
		{"too many pixels", imagetest.JPEGDeclaring(10_000, 6_000), imagecopy.ReasonTooManyPixels},
		{"undecodable", imagetest.JPEGCut(64, 48), imagecopy.ReasonUndecodable},
	}
	for _, tc := range reasons {
		t.Run("a refused image gives "+tc.want+" and an empty output: "+tc.name, func(t *testing.T) {
			asset, out := files(t, tc.content)
			res, err := copier.Copy(ctx, asset, out)
			info, _ := out.Stat()
			if err != nil || res.Reason != tc.want || info.Size() != 0 {
				t.Errorf("Copy: %+v, %v, output of %d bytes; want %s, empty", res, err, info.Size(), tc.want)
			}
		})
	}

	t.Run("a child over its memory limit gives too_large", func(t *testing.T) {
		limits := imagecopy.DefaultLimits()
		limits.Memory = 16 << 20
		asset, out := files(t, imagetest.QuadrantJPEG(4000, 3000))
		res, err := imagecopy.NewCopier(argv(imagecopy.ChildCommand), limits).Copy(ctx, asset, out)
		info, _ := out.Stat()
		if err != nil || res.Reason != imagecopy.ReasonTooLarge || info.Size() != 0 {
			t.Errorf("Copy: %+v, %v, output of %d bytes; want too_large, empty", res, err, info.Size())
		}
	})

	t.Run("a child that runs too long gives timeout", func(t *testing.T) {
		// The child may be killed before it opens the FIFO: nothing waits on it.
		fifo, _ := startedFIFO(t)
		limits := imagecopy.DefaultLimits()
		limits.Timeout = 300 * time.Millisecond
		asset, out := files(t, imagetest.PNG(4, 4))
		res, err := imagecopy.NewCopier(argv(hangChild, fifo), limits).Copy(ctx, asset, out)
		if err != nil || res.Reason != imagecopy.ReasonTimeout {
			t.Errorf("Copy: %+v, %v; want timeout", res, err)
		}
	})

	for _, mode := range []string{garbageChild, liarChild, crashChild, sizeChild} {
		t.Run("a "+mode+" gives copy_failed", func(t *testing.T) {
			asset, out := files(t, imagetest.PNG(4, 4))
			res, err := imagecopy.NewCopier(argv(mode), imagecopy.DefaultLimits()).Copy(ctx, asset, out)
			if err != nil || res.Reason != imagecopy.ReasonCopyFailed {
				t.Errorf("Copy: %+v, %v; want copy_failed", res, err)
			}
		})
	}

	t.Run("a child that cannot start is unavailable", func(t *testing.T) {
		asset, out := files(t, imagetest.PNG(4, 4))
		missing := filepath.Join(t.TempDir(), "no-such-binary")
		_, err := imagecopy.NewCopier([]string{missing, imagecopy.ChildCommand}, imagecopy.DefaultLimits()).Copy(ctx, asset, out)
		if !errors.Is(err, imagecopy.ErrUnavailable) {
			t.Errorf("Copy: %v, want ErrUnavailable", err)
		}
	})

	t.Run("a copier without a child is unavailable", func(t *testing.T) {
		asset, out := files(t, imagetest.PNG(4, 4))
		for _, c := range []*imagecopy.Copier{imagecopy.NewCopier(nil, imagecopy.DefaultLimits()), {}, nil} {
			if _, err := c.Copy(ctx, asset, out); !errors.Is(err, imagecopy.ErrUnavailable) {
				t.Errorf("Copy: %v, want ErrUnavailable", err)
			}
		}
	})

	t.Run("a cancelled context stops the copy", func(t *testing.T) {
		asset, out := files(t, imagetest.PNG(4, 4))
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := copier.Copy(cancelled, asset, out)
		if !errors.Is(err, imagecopy.ErrStopped) {
			t.Errorf("Copy: %v, want ErrStopped", err)
		}
	})

	t.Run("limits are never above the defaults", func(t *testing.T) {
		limits := imagecopy.DefaultLimits()
		limits.Memory = 1 << 40
		limits.Timeout = 0
		asset, out := files(t, imagetest.PNG(4, 4))
		// The child refuses a memory limit above its own: the copy fails, nothing is written.
		res, err := imagecopy.NewCopier(argv(imagecopy.ChildCommand), limits).Copy(ctx, asset, out)
		if err != nil || res.Reason != imagecopy.ReasonCopyFailed {
			t.Errorf("Copy: %+v, %v; want copy_failed", res, err)
		}
	})
}
