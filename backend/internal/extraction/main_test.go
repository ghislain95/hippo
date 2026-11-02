package extraction_test

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/extraction"
)

// Modes of the test binary when a test runs it as the child that reads a page.
const (
	hangChild      = "hang-child"       // says it started on the FIFO given, then never answers
	crashChild     = "crash-child"      // exits with an unexpected code
	garbageChild   = "garbage-child"    // answers something that is not an outcome
	unknownChild   = "unknown-child"    // answers an outcome whose reason the parent does not know
	pdfReasonChild = "pdf-reason-child" // answers a PDF's reason, whatever it was asked
	stoppedChild   = "stopped-child"    // reads a PDF with a clock that passes its deadline after 50 pages
)

// TestMain makes the test binary the child of the tests: run with extraction.ChildCommand, it
// reads a page exactly as `hippo extract-page` does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case extraction.ChildCommand:
			// syscall.Exit, not os.Exit: under -race, a test binary that exits with 0 through
			// os.Exit waits a second for the race detector, which every reading would pay.
			syscall.Exit(extraction.RunChild(os.Stdin, os.Stdout))
		case hangChild:
			if fifo, err := os.OpenFile(os.Args[2], os.O_WRONLY, 0); err == nil {
				_, _ = fifo.WriteString("started\n")
				_ = fifo.Close()
			}
			// Waits for a signal that never comes: a bare select{} is a deadlock the runtime
			// reports, not a child that hangs.
			never := make(chan os.Signal, 1)
			signal.Notify(never, syscall.SIGUSR2)
			<-never
			os.Exit(0)
		case crashChild:
			os.Exit(7)
		case garbageChild:
			fmt.Println("not an outcome")
			syscall.Exit(0)
		case unknownChild:
			fmt.Println(`{"mode":"full","reason":"x"}`)
			syscall.Exit(0)
		case pdfReasonChild:
			fmt.Println(`{"mode":"fallback","reason":"encrypted"}`)
			syscall.Exit(0)
		case stoppedChild:
			syscall.Exit(extraction.RunChildStoppedAfterPages(os.Stdin, os.Stdout, 50))
		}
	}
	os.Exit(m.Run())
}

// childArgv runs the test binary as the child that reads a page.
func childArgv(mode string, args ...string) []string {
	return append([]string{os.Args[0], mode}, args...)
}
