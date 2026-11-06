package childproc_test

import (
	"context"
	"io"
	"syscall"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/childproc"
)

func TestCommandDiesWithParentOnLinux(t *testing.T) {
	cmd := childproc.Command(context.Background(), childproc.Spec{Argv: []string{"/bin/true"}}, io.Discard)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("SysProcAttr = %+v, want Pdeathsig SIGKILL so that no child outlives the service", cmd.SysProcAttr)
	}
}
