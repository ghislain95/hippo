//go:build !linux

package childproc

import "os/exec"

// dieWithParent does nothing outside Linux, which alone has a parent-death signal. The service
// runs on Linux (ADR-0019); elsewhere, in development, an orphan child ends once its work is
// done, within its memory guard and its time limit.
func dieWithParent(*exec.Cmd) {}
