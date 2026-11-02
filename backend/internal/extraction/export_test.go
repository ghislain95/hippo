package extraction

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// Response exposes response to the tests of extraction_test.
type Response = response

// Outcome exposes outcome to the tests of extraction_test.
type Outcome = outcome

// ReadResponse reads the answer of a site as the worker does, but reads an HTML page in the
// test process, so that the tests of the reading need no child. provisional is both the title
// and the host of the link, as for a link just captured.
func ReadResponse(resp Response, provisional string) Outcome {
	l := link{title: provisional, host: provisional}
	out := readBody(resp, l)
	if out.needsPage {
		out = readPage(resp.Body, resp.ContentType, resp.URL, l)
	}
	out.Status = resp.Status
	return out
}

// ReadInChild reads an HTML page as the worker does, in a child run by argv.
func ReadInChild(ctx context.Context, argv []string, timeout time.Duration, resp Response, provisional string) (Outcome, error) {
	r := &pageReader{argv: argv, timeout: timeout}
	return r.read(ctx, resp, link{title: provisional, host: provisional})
}

// ChildCommandForTest builds the child of one page as the worker does, for its environment.
func ChildCommandForTest(ctx context.Context, argv []string) *exec.Cmd {
	cmd, err := (&pageReader{argv: argv, timeout: time.Second}).command(ctx, childRequest{}, nil, io.Discard)
	if err != nil {
		panic(err)
	}
	return cmd
}

// ErrReaderStopped exposes errReaderStopped to the tests of extraction_test.
var ErrReaderStopped = errReaderStopped

// Truncate exposes truncate to the tests of extraction_test.
var Truncate = truncate

// ReadingMinutes exposes readingMinutes to the tests of extraction_test.
var ReadingMinutes = readingMinutes

// WithAfterPass returns opts with a function the worker calls after each pass, so that a test
// waits for the worker instead of sleeping.
func WithAfterPass(opts Options, afterPass func()) Options {
	opts.afterPass = afterPass
	return opts
}

// ChildMeasure is what a measure of the child reports.
type ChildMeasure struct {
	ExitCode  int
	PeakBytes uint64 // the highest memory its runtime held, 0 when it did not answer
	MaxRSS    int64  // its resident set at its peak, in bytes
}

// MeasureChild reads the HTML page of resp in a child run by argv and reports its memory.
func MeasureChild(argv []string, resp Response) (ChildMeasure, error) {
	output := &cappedBuffer{limit: maxChildOutput}
	cmd, err := (&pageReader{argv: argv, timeout: ReadTimeout}).command(context.Background(),
		childRequest{ContentType: resp.ContentType, URL: resp.URL.String(), Title: "t", Host: "h"}, resp.Body, output)
	if err != nil {
		return ChildMeasure{}, err
	}
	_ = cmd.Run() // the exit code is the measure
	m := ChildMeasure{ExitCode: cmd.ProcessState.ExitCode()}
	if usage, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		m.MaxRSS = usage.Maxrss // bytes on Darwin
		if runtime.GOOS == "linux" {
			m.MaxRSS *= 1024 // kilobytes on Linux
		}
	}
	var result childResult
	if json.Unmarshal(output.Bytes(), &result) == nil {
		m.PeakBytes = result.PeakBytes
	}
	return m, nil
}
