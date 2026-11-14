package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/ghislain95/hippo/backend/internal/freespace"
	"github.com/ghislain95/hippo/backend/internal/store"
)

const (
	// lockRetry is how often a backup that waits tries the lock again (design D11).
	lockRetry = time.Second
	// copyBuffer is the buffer of an image copy. A plain buffer, never io.Copy between two files:
	// on Linux that goes to copy_file_range, which on Btrfs may share the blocks of the live
	// image with the backup instead of copying them (design D5).
	copyBuffer = 256 << 10
)

// errWriteFailed is the failure of a write the tests make fail (FailWritesAfterDatabase).
var errWriteFailed = errors.New("write failed")

// Run makes one backup of the database and the images in the backup folder, under its lock,
// and writes its one log line (design D1 to D12). It never writes the database nor touches the
// data folder: the database is copied by store.Snapshot on a read-only connection, the images
// are read through an os.Root on the images folder. The backup is written in .partial-<name>,
// synced, then renamed to <name>; any failure or stop removes the partial folder. After a
// successful backup, the backups older than 14 days are removed, the 3 most recent kept.
//
// It returns nil only when a complete backup exists under its final name. Its errors name no
// file; ErrLocked, ErrNoRoom and ErrNameTaken tell the outcomes of the same names.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	r := &runner{opts: opts}
	began := opts.Now()
	err := r.run(ctx)
	r.res.Duration = opts.Now().Sub(began)
	switch {
	case err == nil:
		r.res.Outcome = OutcomeOK
	case ctx.Err() != nil:
		r.res.Outcome = OutcomeStopped
	case r.res.Outcome == "" || r.res.Outcome == OutcomeOK:
		r.res.Outcome = OutcomeFailed
	}
	r.log(err)
	return r.res, err
}

// runner is one backup in progress.
type runner struct {
	opts Options
	res  Result
}

func (r *runner) run(ctx context.Context) (err error) {
	root, err := os.OpenRoot(r.opts.Dir)
	if err != nil {
		return fmt.Errorf("open backup folder: %w", pathless(err))
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close backup folder: %w", pathless(closeErr))
		}
	}()
	held, err := r.lock(ctx, root)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := held.release(); releaseErr != nil {
			// The backup, if any, is complete: a lock file that does not close is reported, but
			// does not undo it. The lock goes with the process anyway.
			r.opts.Logger.Error("backup lock", slog.String("error", releaseErr.Error()))
		}
	}()

	start := r.opts.Now()
	name := start.In(r.opts.Location).Format(nameLayout)
	r.res.Name = name
	if r.res.LeftoversRemoved, err = removeLeftovers(root); err != nil {
		return err
	}
	previous, err := latestBackup(root)
	if err != nil {
		return err
	}
	images, err := os.OpenRoot(r.opts.Assets)
	if err != nil {
		return fmt.Errorf("open images folder: %w", pathless(err))
	}
	defer func() {
		if closeErr := images.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close images folder: %w", pathless(closeErr))
		}
	}()
	if err := r.checkRoom(root, images, previous); err != nil {
		return err
	}
	// Checked after the room, still before anything is written.
	if err := checkNameFree(root, name); err != nil {
		if errors.Is(err, ErrNameTaken) {
			r.res.Outcome = OutcomeNameTaken
		}
		return err
	}

	partial := partialPrefix + name
	if err := root.Mkdir(partial, dirMode); err != nil {
		return fmt.Errorf("create partial folder: %w", pathless(err))
	}
	if err := r.write(ctx, root, images, partial, previous, start); err != nil {
		if removeErr := root.RemoveAll(partial); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove partial folder: %w", pathless(removeErr)))
		}
		if errors.Is(err, ErrNameTaken) {
			r.res.Outcome = OutcomeNameTaken
		}
		return err
	}
	r.res.BackupsRemoved = r.retain(root, name, start)
	return nil
}

// lock takes the lock of the backup folder: once without LockWait (outcome skipped), else
// every second until LockWait has passed (outcome locked) or ctx ends.
func (r *runner) lock(ctx context.Context, root *os.Root) (*fileLock, error) {
	var deadline <-chan time.Time
	for {
		held, err := lock(root)
		if err == nil || !errors.Is(err, ErrLocked) {
			return held, err
		}
		if r.opts.LockWait <= 0 {
			r.res.Outcome = OutcomeSkipped
			return nil, err
		}
		if deadline == nil {
			timer := time.NewTimer(r.opts.LockWait)
			defer timer.Stop()
			deadline = timer.C
			r.opts.Logger.Info("waiting for the backup lock", slog.Duration("wait", r.opts.LockWait))
		}
		retry := time.NewTimer(min(lockRetry, r.opts.LockWait))
		select {
		case <-ctx.Done():
			retry.Stop()
			return nil, ctx.Err()
		case <-deadline:
			retry.Stop()
			r.res.Outcome = OutcomeLocked
			return nil, err
		case <-retry.C:
		}
	}
}

// checkRoom refuses, before anything is written, a backup whose database (main file and WAL,
// an upper bound of the copy), images to copy (not those to link) and freespace.Margin do not
// fit in the free space of the backup folder (design D10). The listing is an estimate: the
// real one comes after the copy of the database.
func (r *runner) checkRoom(root, images *os.Root, previous string) error {
	needed := uint64(freespace.Margin)
	for _, p := range []string{r.opts.Database, r.opts.Database + "-wal"} {
		info, err := os.Stat(p)
		if errors.Is(err, fs.ErrNotExist) && p != r.opts.Database {
			continue
		}
		if err != nil {
			return fmt.Errorf("read database size: %w", pathless(err))
		}
		needed += uint64(info.Size()) //nolint:gosec // a file size is not negative
	}
	files, _, err := listImages(images)
	if err != nil {
		return err
	}
	for _, f := range files {
		if !linkable(root, previous, f.rel, f.size, f.modTime) {
			needed += uint64(f.size) //nolint:gosec // a file size is not negative
		}
	}
	measure := r.opts.hooks.freeSpace
	if measure == nil {
		measure = freespace.Available
	}
	free, known, err := measure(r.opts.Dir)
	if err != nil {
		return fmt.Errorf("measure free space: %w", pathless(err))
	}
	if known && free < needed {
		r.res.Outcome = OutcomeNoRoom
		return ErrNoRoom
	}
	return nil
}

// write fills the partial folder: database, images, manifest; syncs it, then renames it to the
// backup's name and syncs the backup folder (design D6).
func (r *runner) write(ctx context.Context, root, images *os.Root, partial, previous string, start time.Time) error {
	dbCopy := path.Join(partial, databaseFile)
	version, err := store.Snapshot(ctx, r.opts.Database, dbCopyPath(r.opts.Dir, dbCopy))
	if err != nil {
		return err
	}
	r.res.MigrationVersion = version
	if err := root.Chmod(dbCopy, fileMode); err != nil {
		return fmt.Errorf("set database copy mode: %w", pathless(err))
	}
	info, err := root.Lstat(dbCopy)
	if err != nil {
		return fmt.Errorf("read database copy size: %w", pathless(err))
	}
	r.res.DatabaseBytes = info.Size()

	files, dirs, err := listImages(images)
	if err != nil {
		return err
	}
	if hook := r.opts.hooks.afterDatabase; hook != nil {
		hook()
	}
	assets := path.Join(partial, assetsFolder)
	created := []string{partial, assets}
	if err := root.Mkdir(assets, dirMode); err != nil {
		return fmt.Errorf("create images folder: %w", pathless(err))
	}
	for _, dir := range dirs {
		if err := root.Mkdir(path.Join(assets, dir), dirMode); err != nil {
			return fmt.Errorf("create images folder: %w", pathless(err))
		}
		created = append(created, path.Join(assets, dir))
	}
	buf := make([]byte, copyBuffer)
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.backupImage(root, images, assets, previous, f.rel, buf); err != nil {
			return err
		}
	}
	r.res.Images = r.res.ImagesCopied + r.res.ImagesLinked

	if err := r.writeManifest(root, path.Join(partial, manifestFile), start); err != nil {
		return err
	}
	// Each folder after its entries, deepest first, the partial folder last.
	for i := len(created) - 1; i >= 0; i-- {
		if err := syncDir(root, created[i]); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Root.Rename replaces an empty folder (renameat), unlike os.Rename: the name is checked
	// again, under the lock that keeps any other backup from creating it.
	name := r.res.Name
	if err := checkNameFree(root, name); err != nil {
		return err
	}
	if err := root.Rename(partial, name); err != nil {
		return fmt.Errorf("rename partial folder: %w", pathless(err))
	}
	return syncDir(root, ".")
}

// dbCopyPath is the path store.Snapshot writes the copy to, under the backup folder.
func dbCopyPath(dir, rel string) string {
	return filepath.Join(dir, filepath.FromSlash(rel))
}

// backupImage puts the image rel of the images folder in the backup's assets folder: a hard
// link to the previous backup's copy when it has the same size and modification time, else a
// byte copy. A file gone since the listing is counted as gone.
func (r *runner) backupImage(root, images *os.Root, assets, previous, rel string, buf []byte) error {
	src, err := images.Open(rel)
	if errors.Is(err, fs.ErrNotExist) {
		r.res.ImagesGone++
		return nil
	}
	if err != nil {
		return fmt.Errorf("open image: %w", pathless(err))
	}
	defer func() { _ = src.Close() }() // read only: nothing to flush
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("read image: %w", pathless(err))
	}
	if !info.Mode().IsRegular() {
		// Replaced since the listing by something that is not a file: not an image any more.
		r.res.ImagesGone++
		return nil
	}
	target := path.Join(assets, rel)
	if linkable(root, previous, rel, info.Size(), info.ModTime()) {
		link := r.opts.hooks.link
		if link == nil {
			link = (*os.Root).Link
		}
		if err := link(root, path.Join(previous, assetsFolder, rel), target); err == nil {
			r.res.ImagesLinked++
			return nil
		}
		// EXDEV, EMLINK, EPERM…: a copy does the same, with more room.
	}
	written, err := r.copyImage(root, src, target, info.ModTime(), buf)
	if err != nil {
		return err
	}
	r.res.ImagesCopied++
	r.res.ImageBytesWritten += written
	return nil
}

// copyImage copies src to the new file target through buf, sets its modification time to the
// source's, so that the next backup can link it, then syncs it.
func (r *runner) copyImage(root *os.Root, src io.Reader, target string, modTime time.Time, buf []byte) (n int64, err error) {
	dst, err := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return 0, fmt.Errorf("create image copy: %w", pathless(err))
	}
	defer func() {
		if closeErr := dst.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close image copy: %w", pathless(closeErr))
		}
	}()
	var w io.Writer = writerOnly{dst}
	if r.opts.hooks.failWrites {
		w = failingWriter{}
	}
	n, err = io.CopyBuffer(w, readerOnly{src}, buf)
	if err != nil {
		return n, fmt.Errorf("copy image: %w", pathless(err))
	}
	if err := root.Chtimes(target, time.Time{}, modTime); err != nil {
		return n, fmt.Errorf("set image copy time: %w", pathless(err))
	}
	if err := dst.Sync(); err != nil {
		return n, fmt.Errorf("sync image copy: %w", pathless(err))
	}
	return n, nil
}

// linkable tells whether the previous backup holds the image rel as a regular file of the
// same size and modification time, to the nanosecond.
func linkable(root *os.Root, previous, rel string, size int64, modTime time.Time) bool {
	if previous == "" {
		return false
	}
	info, err := root.Lstat(path.Join(previous, assetsFolder, rel))
	return err == nil && info.Mode().IsRegular() && info.Size() == size && info.ModTime().Equal(modTime)
}

// imageFile is a regular file of the images folder, as listed.
type imageFile struct {
	rel     string
	size    int64
	modTime time.Time
}

// listImages walks the images folder through its root, Lstat only: regular files are images,
// folders are walked, .tmp- entries (uploads in progress, at any level), symbolic links and
// anything else are left out, never followed. It returns the images and the folders, each
// folder before its content.
func listImages(images *os.Root) (files []imageFile, dirs []string, err error) {
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := readDir(images, dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if len(entry.Name()) >= len(tempPrefix) && entry.Name()[:len(tempPrefix)] == tempPrefix {
				continue
			}
			rel := path.Join(dir, entry.Name())
			info, err := images.Lstat(rel)
			if errors.Is(err, fs.ErrNotExist) {
				continue // removed while listed: no row can still need it
			}
			if err != nil {
				return fmt.Errorf("list images: %w", pathless(err))
			}
			switch {
			case info.IsDir():
				dirs = append(dirs, rel)
				if err := walk(rel); err != nil {
					return err
				}
			case info.Mode().IsRegular():
				files = append(files, imageFile{rel: rel, size: info.Size(), modTime: info.ModTime()})
			}
		}
		return nil
	}
	if err := walk("."); err != nil {
		return nil, nil, err
	}
	return files, dirs, nil
}

func readDir(root *os.Root, dir string) ([]fs.DirEntry, error) {
	f, err := root.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("list folder: %w", pathless(err))
	}
	entries, err := f.ReadDir(-1)
	if closeErr := f.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("list folder: %w", pathless(err))
	}
	return entries, nil
}

func syncDir(root *os.Root, name string) error {
	dir, err := root.Open(name)
	if err != nil {
		return fmt.Errorf("open folder: %w", pathless(err))
	}
	syncErr := dir.Sync()
	if err := dir.Close(); syncErr == nil && err != nil {
		syncErr = err
	}
	if syncErr != nil {
		return fmt.Errorf("sync folder: %w", pathless(syncErr))
	}
	return nil
}

// checkNameFree fails with ErrNameTaken when anything of the backup folder has the name.
func checkNameFree(root *os.Root, name string) error {
	_, err := root.Lstat(name)
	switch {
	case err == nil:
		return ErrNameTaken
	case errors.Is(err, fs.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("check backup name: %w", pathless(err))
	}
}

// writerOnly and readerOnly hide ReadFrom and WriteTo from io.CopyBuffer, so that it copies
// through its buffer (copyBuffer).
type writerOnly struct{ io.Writer }

type readerOnly struct{ io.Reader }

// failingWriter fails every write (tests: FailWritesAfterDatabase).
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriteFailed }

// log writes the one log line of the backup (design D12): counters and names, never an image
// path nor an item's content. Its error is path-free by construction.
func (r *runner) log(err error) {
	level := slog.LevelError
	switch r.res.Outcome {
	case OutcomeOK:
		level = slog.LevelInfo
	case OutcomeSkipped:
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		slog.String("outcome", string(r.res.Outcome)),
		slog.String("trigger", string(r.opts.Trigger)),
		slog.String("backup", r.res.Name),
		slog.String("backend_version", r.opts.Version),
		slog.Int64("migration_version", r.res.MigrationVersion),
		slog.Int64("database_bytes", r.res.DatabaseBytes),
		slog.Int("images", r.res.Images),
		slog.Int("images_copied", r.res.ImagesCopied),
		slog.Int("images_linked", r.res.ImagesLinked),
		slog.Int("images_gone", r.res.ImagesGone),
		slog.Int64("image_bytes_written", r.res.ImageBytesWritten),
		slog.Int64("duration_ms", r.res.Duration.Milliseconds()),
		slog.Int("backups_removed", r.res.BackupsRemoved),
		slog.Int("leftovers_removed", r.res.LeftoversRemoved),
	}
	if err != nil {
		attrs = append(attrs, slog.String("error", err.Error()))
	}
	r.opts.Logger.LogAttrs(context.Background(), level, "backup", attrs...)
}
