package backup

import (
	"errors"
	"os"
)

// SetFreeSpace makes the backup measure the free space of the backup folder with free.
func SetFreeSpace(opts *Options, free func(path string) (uint64, bool, error)) {
	opts.hooks.freeSpace = free
}

// SetLink makes the backup create its hard links with link.
func SetLink(opts *Options, link func(root *os.Root, oldname, newname string) error) {
	opts.hooks.link = link
}

// AfterDatabase calls hook once the database is copied and the images folder listed, before the
// first image is copied.
func AfterDatabase(opts *Options, hook func()) {
	opts.hooks.afterDatabase = hook
}

// FailWritesAfterDatabase makes every write of the backup fail once the database is copied, as
// on a full disk.
func FailWritesAfterDatabase(opts *Options) {
	opts.hooks.failWrites = true
}

// HoldLock takes the lock of the backup folder dir as a backup does, and returns its release.
func HoldLock(dir string) (release func() error, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	l, err := lock(root)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return func() error { return errors.Join(l.release(), root.Close()) }, nil
}
