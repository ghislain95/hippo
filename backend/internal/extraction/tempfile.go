package extraction

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/ghislain95/hippo/backend/internal/freespace"
)

// The temporary directory, DATA_DIR/tmp, holds the PDFs being downloaded and, through NewFile,
// the files other packages write for a moment (the body of a batch, the copies of images): on
// the volume of the data, not in /tmp, which in the container is the image's layer or a tmpfs
// counted in its memory. A file loses its name before it is written: its data lives as long as
// a descriptor is open on it, in the service or in the child that reads it, and disappears with
// the last one, crash included. Nothing of a PDF stays on the NAS, and backups never see one
// (ADR-0020). Every access goes through an os.Root on the directory (design.md).
const (
	tempDirMode  fs.FileMode = 0o700
	tempFileMode fs.FileMode = 0o600
	tempPrefix               = ".pdf-"
)

// errStorage: the NAS cannot store a PDF (no room, read-only volume, input-output error). The
// item stays queued; it never names a file.
var errStorage = errors.New("temporary file unavailable")

// TempDir is the temporary directory of the extraction.
type TempDir struct {
	root *os.Root
	// freeSpace measures the space left on the volume; known is false where it cannot be
	// measured. Tests replace it.
	freeSpace  func() (free uint64, known bool, err error)
	failWrites bool // tests: every file fails its writes, as on a full disk
}

// OpenTempDir creates the directory at path if needed (0700), removes the files a crash left
// in it, and checks that a file can be created there, so that the service fails at start-up
// rather than at the first PDF. It returns the number of files removed.
func OpenTempDir(path string) (*TempDir, int, error) {
	if err := os.MkdirAll(path, tempDirMode); err != nil {
		return nil, 0, fmt.Errorf("create temporary directory: %w", pathless(err))
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open temporary directory: %w", pathless(err))
	}
	d := &TempDir{root: root, freeSpace: func() (uint64, bool, error) { return freespace.Available(path) }}
	removed, err := d.removeLeftovers()
	if err == nil {
		var probe *tempFile
		if probe, err = d.create(); err == nil {
			err = probe.Close()
		}
	}
	if err != nil {
		return nil, removed, errors.Join(err, d.Close())
	}
	return d, removed, nil
}

// Close releases the directory.
func (d *TempDir) Close() error {
	if err := d.root.Close(); err != nil {
		return fmt.Errorf("close temporary directory: %w", pathless(err))
	}
	return nil
}

// removeLeftovers removes every regular file of the directory: only a crash between the
// creation of a file and the removal of its name leaves one.
func (d *TempDir) removeLeftovers() (int, error) {
	top, err := d.root.Open(".")
	if err != nil {
		return 0, fmt.Errorf("list temporary directory: %w", pathless(err))
	}
	entries, err := top.ReadDir(-1)
	if closeErr := top.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return 0, fmt.Errorf("list temporary directory: %w", pathless(err))
	}
	removed := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		if err := d.root.Remove(entry.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("remove temporary file: %w", pathless(err))
		}
		removed++
	}
	return removed, nil
}

// errNoRoom: the volume has not the room for a PDF and freespace.Margin.
var errNoRoom = errors.New("not enough free space")

// checkRoom tells, as a storage failure, that the volume cannot hold size bytes more and
// freespace.Margin: a PDF whose size, or MaxPDFSize when unknown, does not fit with it is not
// downloaded.
func (d *TempDir) checkRoom(size uint64) error {
	free, known, err := d.freeSpace()
	if err != nil {
		return fmt.Errorf("%w: free space: %w", errStorage, pathless(err))
	}
	if known && free < size+freespace.Margin {
		return fmt.Errorf("%w: %w", errStorage, errNoRoom)
	}
	return nil
}

// tempFile is a file without name: the service writes the PDF to w, the child reads it from r.
type tempFile struct {
	w, r *os.File
}

// NewFile creates a file in the directory, opens it a second time read-only, and removes its
// name before returning: w writes it, r reads it (or is handed to a child), and its data goes
// with the last of the two closed, crash included; nothing is left in the directory. The caller
// closes both. Its errors wrap errStorage and name no file.
func (d *TempDir) NewFile() (w, r *os.File, err error) {
	var random [16]byte
	_, _ = rand.Read(random[:]) // crypto/rand never fails on supported platforms
	name := tempPrefix + hex.EncodeToString(random[:])
	w, err = d.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, tempFileMode)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: create: %w", errStorage, pathless(err))
	}
	r, err = d.root.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		_ = w.Close()           // empty: nothing to flush
		_ = d.root.Remove(name) // best effort: the start-up sweep removes it otherwise
		return nil, nil, fmt.Errorf("%w: open: %w", errStorage, pathless(err))
	}
	if err := d.root.Remove(name); err != nil {
		_ = w.Close() // empty: nothing to flush
		_ = r.Close() // read-only
		return nil, nil, fmt.Errorf("%w: remove name: %w", errStorage, pathless(err))
	}
	return w, r, nil
}

// create creates a file as NewFile does, for a PDF. Its errors wrap errStorage.
func (d *TempDir) create() (*tempFile, error) {
	w, r, err := d.NewFile()
	if err != nil {
		return nil, err
	}
	if d.failWrites {
		_ = w.Close() // its writes fail from now on
	}
	return &tempFile{w: w, r: r}, nil
}

// Write writes to the file; its errors wrap errStorage, so that a full disk is told from a
// network that fails while the body is copied.
func (f *tempFile) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if err != nil {
		return n, fmt.Errorf("%w: write: %w", errStorage, pathless(err))
	}
	return n, nil
}

// Close closes both descriptors; the data goes with the last one, in the child or here. It
// may run more than once.
func (f *tempFile) Close() error {
	var errs []error
	for _, file := range []**os.File{&f.w, &f.r} {
		if *file != nil {
			errs = append(errs, (*file).Close())
			*file = nil
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("close temporary file: %w", pathless(err))
	}
	return nil
}

// pathless drops the file name of a file system error, keeping its cause: the errors of the
// temporary directory reach the logs, which name no file.
func pathless(err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return linkErr.Err
	}
	return err
}
