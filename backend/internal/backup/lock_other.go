//go:build !linux && !darwin

package backup

import (
	"errors"
	"os"
)

// fileLock is not available outside Linux and macOS: the service runs on Linux (ADR-0019).
type fileLock struct{}

func lock(*os.Root) (*fileLock, error) {
	return nil, errors.New("backup lock not supported on this system")
}

func (*fileLock) release() error { return nil }
