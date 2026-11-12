package freespace

import "syscall"

// Available is the space left for the service on the file system of path, in bytes; known is
// true on Linux and macOS.
func Available(path string) (free uint64, known bool, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false, err
	}
	return st.Bavail * uint64(st.Bsize), true, nil //nolint:gosec // a block size is positive
}
