//go:build !linux

package extraction

import "os/exec"

// dieWithParent does nothing outside Linux, which alone has a parent-death signal. The service
// runs on Linux (ADR-0019); elsewhere, in development, an orphan child ends once it has read
// its page, within its memory guard.
func dieWithParent(*exec.Cmd) {}
