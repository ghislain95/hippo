package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// probePrefix starts the name of the file CheckDir creates and removes at once. A crash in
// between leaves one: never a backup name, never touched by the retention.
const probePrefix = ".probe-"

// CheckDir checks, at the start of the service, that the backup folder at path exists, is a
// folder that can be written (a probe file is created, then removed, as extraction.OpenTempDir
// does), and that its lock file opens for writing (design D15). It never creates the folder:
// in the container a missing mount leaves no /backups, and the service refuses to start rather
// than run without backups. Its errors name no file.
func CheckDir(path string) (err error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return fmt.Errorf("backup folder: %w", pathless(err))
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("backup folder: %w", pathless(closeErr))
		}
	}()
	if err := probe(root); err != nil {
		return err
	}
	f, err := root.OpenFile(lockFile, os.O_RDWR|os.O_CREATE, fileMode)
	if err == nil {
		err = f.Close()
	}
	if err != nil {
		return fmt.Errorf("backup folder: lock file: %w", pathless(err))
	}
	return nil
}

// probe creates a new file in root and removes it: a folder whose lock file exists but where
// nothing can be created would fail at the first backup, not at start-up.
func probe(root *os.Root) error {
	var random [8]byte
	_, _ = rand.Read(random[:]) // crypto/rand never fails on supported platforms
	name := probePrefix + hex.EncodeToString(random[:])
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return fmt.Errorf("backup folder: cannot write: %w", pathless(err))
	}
	closeErr := f.Close()
	if err := root.Remove(name); err != nil {
		return errors.Join(closeErr, fmt.Errorf("backup folder: remove probe: %w", pathless(err)))
	}
	if closeErr != nil {
		return fmt.Errorf("backup folder: probe: %w", pathless(closeErr))
	}
	return nil
}
