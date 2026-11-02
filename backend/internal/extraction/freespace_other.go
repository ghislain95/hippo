//go:build !linux && !darwin

package extraction

// diskFree does not know the free space outside Linux and macOS: the service runs on Linux
// (ADR-0019); elsewhere a full disk shows as a failed write.
func diskFree(string) (uint64, bool, error) { return 0, false, nil }
