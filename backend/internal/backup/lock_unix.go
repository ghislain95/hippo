//go:build linux || darwin

package backup

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// fileLock is an exclusive flock on the file .lock of the backup folder (design D11). The lock
// belongs to the open file description: it goes with its last descriptor, process death
// included, and two opens of the file conflict, even in one process. Go opens every file
// close-on-exec, so the children of the service never inherit it.
type fileLock struct {
	file *os.File
}

// lock tries once to take the lock of the backup folder root; ErrLocked when another holds it.
func lock(root *os.Root) (*fileLock, error) {
	file, err := root.OpenFile(lockFile, os.O_RDWR|os.O_CREATE, fileMode)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", pathless(err))
	}
	fd := int(file.Fd()) //nolint:gosec // a file descriptor fits an int
	for {
		err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		closeErr := file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(ErrLocked, closeErr)
		}
		return nil, errors.Join(fmt.Errorf("take lock: %w", err), closeErr)
	}
	return &fileLock{file: file}, nil
}

// release closes the lock file, which releases the lock.
func (l *fileLock) release() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("release lock: %w", pathless(err))
	}
	return nil
}
