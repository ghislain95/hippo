package extraction

import "syscall"

// diskFree is the space left for the service on the file system of path, in bytes.
func diskFree(path string) (uint64, bool, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false, err
	}
	return st.Bavail * uint64(st.Bsize), true, nil
}
