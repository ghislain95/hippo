package extraction

import (
	"os/exec"
	"syscall"
)

// dieWithParent makes the kernel kill the child when the service dies, so that no child is
// left reading a page after a crash of the service. The signal follows the thread that
// started the child; the worker does not lock its goroutine to a thread, so the runtime keeps
// that thread while the service runs.
func dieWithParent(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
