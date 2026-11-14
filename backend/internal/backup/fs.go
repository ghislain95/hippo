package backup

import (
	"errors"
	"io/fs"
	"os"
)

// Names and modes of the backup folder (design D6, D17).
const (
	lockFile      = ".lock"
	partialPrefix = ".partial-"
	databaseFile  = "hippo.db"
	assetsFolder  = "assets"
	manifestFile  = "manifest.json"
	tempPrefix    = ".tmp-" // temporary files of assetfs, never backed up

	fileMode fs.FileMode = 0o640
	dirMode  fs.FileMode = 0o750
)

// pathless drops the file names of a file system error, keeping its cause: the errors of a
// backup reach the logs and the output of the command, which name no file.
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
