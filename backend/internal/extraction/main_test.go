package extraction_test

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	fullChild      = "full-child"       // answers a full page, whatever it was asked
	memoryChild    = "memory-child"     // exits as a child over its memory limit does
	floodChild     = "flood-child"      // answers an outcome valid for its format, beyond what the parent reads
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
		case fullChild:
			fmt.Println(`{"mode":"full","reason":"ok","title":"x"}`)
			syscall.Exit(0)
		case memoryChild:
			syscall.Exit(extraction.ChildExitMemory)
		case floodChild:
			// An answer valid for the format asked, after more than the parent reads: only the
			// output bound refuses it (leading spaces do not stop JSON from decoding).
			var req struct {
				Format string `json:"format"`
			}
			line, _ := bufio.NewReader(os.Stdin).ReadBytes('\n')
			_ = json.Unmarshal(line, &req)
			answer := `{"mode":"full","reason":"ok","title":"x"}`
			if req.Format == "youtube" {
				answer = `{"mode":"fallback","reason":"ok","title":"x"}`
			}
			_, _ = os.Stdout.Write(bytes.Repeat([]byte(" "), extraction.MaxChildOutput+1))
			fmt.Println(answer)
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
