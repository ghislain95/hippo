//go:build !unix

package childproc

import "os"

// maxRSS is unknown where the system gives no resource usage of a child.
func maxRSS(*os.ProcessState) int64 { return 0 }
