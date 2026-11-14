//go:build !linux && !darwin

package freespace

// Available does not know the free space outside Linux and macOS: the service runs on Linux
// (ADR-0019); elsewhere a full disk shows as a failed write.
func Available(string) (free uint64, known bool, err error) { return 0, false, nil }
