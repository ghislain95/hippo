//go:build unix

package childproc

import (
	"os"
	"runtime"
	"syscall"
)

// maxRSS is the resident set of the child at its peak, in bytes.
func maxRSS(state *os.ProcessState) int64 {
	usage, ok := state.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}
	rss := int64(usage.Maxrss) //nolint:unconvert // int32 on linux/386 and linux/arm; bytes on Darwin
	if runtime.GOOS != "darwin" && runtime.GOOS != "ios" {
		rss *= 1024 // kilobytes on Linux and the BSDs
	}
	return rss
}
