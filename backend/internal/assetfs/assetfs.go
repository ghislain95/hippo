// Package assetfs stores the image files of the assets under DATA_DIR/assets (ADR-0004,
// ADR-0019). An upload is written to a temporary file of that directory, then synced and
// renamed to its final path before the capture transaction runs, so that a database row never
// points to a missing file; a file that ends up unused is removed by its caller.
//
// Every access goes through an os.Root on the assets directory: no path can leave it, whatever
// it holds. The errors of a file never carry its name. The package knows neither HTTP nor the
// database.
package assetfs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Modes of the files and directories: readable by the service's group, never by others.
const (
	dirMode  fs.FileMode = 0o750
	fileMode fs.FileMode = 0o640
)

// tempPrefix starts the name of every temporary file, at the top of the assets directory only.
// Start-up removes the files left by a crash (RemoveTemps), and backups skip them.
const tempPrefix = ".tmp-"

// Dir is the assets directory.
type Dir struct {
	root *os.Root
}

// Open creates the assets directory at path if needed and opens it.
func Open(path string) (*Dir, error) {
	if err := os.MkdirAll(path, dirMode); err != nil {
		return nil, fmt.Errorf("create assets directory: %w", err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open assets directory: %w", err)
	}
	return &Dir{root: root}, nil
}

// Close releases the directory.
func (d *Dir) Close() error {
	if err := d.root.Close(); err != nil {
		return fmt.Errorf("close assets directory: %w", err)
	}
	return nil
}

// CheckWritable creates and removes a temporary file, so that the service fails at start-up
// rather than at the first image when it cannot write.
func (d *Dir) CheckWritable() error {
	probe, err := d.CreateTemp()
	if err != nil {
		return err
	}
	return probe.Remove()
}

// RemoveTemps removes the temporary files left at the top of the directory by an interrupted
// upload, and returns how many it removed. Committed files are never touched: a file without
// row after a crash is harmless.
func (d *Dir) RemoveTemps() (int, error) {
	top, err := d.root.Open(".")
	if err != nil {
		return 0, fmt.Errorf("list assets directory: %w", err)
	}
	entries, err := top.ReadDir(-1)
	if closeErr := top.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return 0, fmt.Errorf("list assets directory: %w", err)
	}
	removed := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), tempPrefix) {
			continue
		}
		if err := d.root.Remove(entry.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return removed, fmt.Errorf("remove temporary file: %w", pathless(err))
		}
		removed++
	}
	return removed, nil
}

// Open opens the committed file rel, relative to the directory, for reading. It never follows a
// path out of the directory, symbolic links included.
func (d *Dir) Open(rel string) (*os.File, error) {
	file, err := d.root.Open(filepath.FromSlash(rel))
	if err != nil {
		return nil, fmt.Errorf("open asset file: %w", pathless(err))
	}
	return file, nil
}

// CreateTemp creates an empty temporary file in the directory, for one upload.
func (d *Dir) CreateTemp() (*Upload, error) {
	var random [16]byte
	_, _ = rand.Read(random[:]) // crypto/rand never fails on supported platforms
	name := tempPrefix + hex.EncodeToString(random[:])
	file, err := d.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return nil, fmt.Errorf("create temporary file: %w", pathless(err))
	}
	return &Upload{dir: d, file: file, name: name}, nil
}

// Upload is the file of one upload: a temporary file until Commit, then its final file. It is
// used by one goroutine at a time.
type Upload struct {
	dir     *Dir
	file    *os.File // nil once closed
	name    string   // current name, relative to the directory
	size    int64
	kept    bool
	removed bool
}

// Write appends p to the temporary file.
func (u *Upload) Write(p []byte) (int, error) {
	if u.file == nil {
		return 0, errors.New("write upload: file closed")
	}
	n, err := u.file.Write(p)
	u.size += int64(n)
	if err != nil {
		return n, fmt.Errorf("write upload: %w", pathless(err))
	}
	return n, nil
}

// ReadAt reads the written content at off, for the format checks.
func (u *Upload) ReadAt(p []byte, off int64) (int, error) {
	if u.file == nil {
		return 0, errors.New("read upload: file closed")
	}
	// Unwrapped: an io.ReaderAt reports the end of the file with io.EOF itself.
	return u.file.ReadAt(p, off)
}

// Size is the number of bytes written.
func (u *Upload) Size() int64 {
	return u.size
}

// Commit syncs the file, renames it to rel (relative to the directory, slash-separated, built
// from server identifiers) and syncs the directories, so that the file survives a power cut
// before the transaction that references it commits.
func (u *Upload) Commit(rel string) error {
	target := filepath.FromSlash(rel)
	if u.file == nil || u.removed {
		return errors.New("commit upload: file closed")
	}
	if !filepath.IsLocal(target) || strings.HasPrefix(filepath.Base(target), tempPrefix) {
		return fmt.Errorf("commit upload: invalid asset path")
	}
	if err := u.file.Sync(); err != nil {
		return fmt.Errorf("commit upload: sync file: %w", pathless(err))
	}
	if err := u.close(); err != nil {
		return err
	}
	parent := filepath.Dir(target)
	if err := u.dir.root.MkdirAll(parent, dirMode); err != nil {
		return fmt.Errorf("commit upload: create directory: %w", pathless(err))
	}
	if err := u.dir.root.Rename(u.name, target); err != nil {
		return fmt.Errorf("commit upload: rename: %w", pathless(err))
	}
	u.name = target
	// The rename and a new user directory live in the entries of both directories.
	for _, dir := range []string{parent, "."} {
		if err := u.dir.syncDir(dir); err != nil {
			return fmt.Errorf("commit upload: %w", err)
		}
	}
	return nil
}

// Keep marks the committed file as referenced by a stored asset: Remove leaves it.
func (u *Upload) Keep() {
	u.kept = true
}

// Remove deletes the file, temporary or committed, unless Keep was called. It can be called
// more than once, and after a failed Commit.
func (u *Upload) Remove() error {
	if u.kept || u.removed {
		return nil
	}
	closeErr := u.close()
	if err := u.dir.root.Remove(u.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.Join(closeErr, fmt.Errorf("remove upload: %w", pathless(err)))
	}
	u.removed = true
	return closeErr
}

func (u *Upload) close() error {
	if u.file == nil {
		return nil
	}
	err := u.file.Close()
	u.file = nil
	if err != nil {
		return fmt.Errorf("close upload: %w", pathless(err))
	}
	return nil
}

// pathless drops the file name of a file system error, keeping its cause (fs.ErrNotExist
// stays detectable): the errors of an upload reach the logs, which name identifiers only.
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

func (d *Dir) syncDir(name string) error {
	dir, err := d.root.Open(name)
	if err != nil {
		return fmt.Errorf("open directory: %w", pathless(err))
	}
	syncErr := dir.Sync()
	if err := dir.Close(); syncErr == nil && err != nil {
		syncErr = err
	}
	if syncErr != nil {
		return fmt.Errorf("sync directory: %w", pathless(syncErr))
	}
	return nil
}
