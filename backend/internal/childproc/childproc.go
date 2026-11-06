// Package childproc runs the work that may grow without bound (an HTML tree, a PDF, a decoded
// image) in a child process of the same binary, bounded in memory and in time, so that whatever
// happens to the child the service keeps running. It is the only package that starts a
// subprocess.
//
// The child side calls Guard at its start; the parent side calls Run. The child gets no
// environment of the service (Env), which holds the API token and the Anthropic key, its stderr
// is discarded (a panic message may quote an item), and its output is bounded (CappedBuffer).
package childproc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

const (
	// ExitMemory is the exit code of a child that reached the hard limit of its Guard.
	ExitMemory = 86
	// ExitInput is the exit code of a child that could not read its request.
	ExitInput = 2
)

var (
	// ErrStopped: the parent's context was done while the child ran, or before it started.
	ErrStopped = errors.New("child stopped")
	// ErrTimeout: the child ran beyond Spec.Timeout and was killed.
	ErrTimeout = errors.New("child timed out")
	// ErrUnavailable: the child could not start (binary missing, fork refused); the host is at
	// fault, not the input.
	ErrUnavailable = errors.New("child unavailable")
	// ErrMemory: the child exited with ExitMemory.
	ErrMemory = errors.New("child over its memory limit")
	// ErrFailed: any other failure of the child (another exit code, a signal, its pipes).
	ErrFailed = errors.New("child failed")
)

// Env is the whole environment of a child: never the service's. Two processors at least, so
// that the memory watch of Guard runs while the work loops.
func Env() []string {
	return []string{"GOMAXPROCS=2"}
}

// Spec describes one run of a child.
type Spec struct {
	// Argv is the binary and its arguments; never empty.
	Argv []string
	// Timeout bounds the run; zero means the parent's context only, kept for the measures of
	// the tests. A service caller always sets it. A negative value is refused.
	Timeout time.Duration
	// Stdin is what the child reads on its standard input; nil gives it an empty one.
	Stdin io.Reader
	// ExtraFiles become descriptors 3, 4…: the only descriptors the child inherits, since Go
	// opens every other one close-on-exec.
	ExtraFiles []*os.File
	// MaxOutput bounds what is kept of the child's standard output.
	MaxOutput int
	// WaitDelay bounds the wait for the pipes of a child killed at its time limit.
	WaitDelay time.Duration
}

// Result is what a child left.
type Result struct {
	// Output is the first Spec.MaxOutput bytes of the child's standard output.
	Output []byte
	// Overflow tells the child wrote more than Spec.MaxOutput.
	Overflow bool
	// ExitCode is the exit code of the child, -1 when killed by a signal or never started.
	ExitCode int
	// MaxRSS is the resident set of the child at its peak, in bytes; 0 where unknown.
	MaxRSS int64
}

// Command builds the child of spec without starting it, its standard output going to stdout:
// environment Env only, stderr discarded, killed with the parent where the system allows it.
// Run is the way to start it; Command lets a test inspect the child.
func Command(ctx context.Context, spec Spec, stdout io.Writer) *exec.Cmd {
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Env = Env()
	cmd.Stdin = spec.Stdin
	cmd.ExtraFiles = spec.ExtraFiles
	cmd.Stdout = stdout
	cmd.Stderr = nil // discarded: a panic message may quote an item
	cmd.WaitDelay = spec.WaitDelay
	dieWithParent(cmd)
	return cmd
}

// Run runs the child of spec and returns what it left. The error is ErrStopped when ctx is
// done, ErrTimeout past spec.Timeout, ErrUnavailable (wrapping the cause) when the child could
// not start, ErrMemory on ExitMemory, ErrFailed (wrapping the cause) on any other failure of
// the child (another exit code, a signal, its pipes): every error wraps exactly one of them.
// The Result is filled whatever the outcome. An output beyond spec.MaxOutput is not an error:
// Result.Overflow tells it.
func Run(ctx context.Context, spec Spec) (Result, error) {
	if spec.Timeout < 0 {
		return Result{ExitCode: -1}, fmt.Errorf("%w: negative timeout %v", ErrUnavailable, spec.Timeout)
	}
	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if spec.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, spec.Timeout)
	}
	defer cancel()
	output := NewCappedBuffer(spec.MaxOutput)
	cmd := Command(runCtx, spec, output)
	runErr := cmd.Run()
	res := Result{Output: output.Bytes(), Overflow: output.Overflow(), ExitCode: -1}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
		res.MaxRSS = maxRSS(cmd.ProcessState)
	}
	switch {
	case ctx.Err() != nil:
		return res, fmt.Errorf("%w: %w", ErrStopped, ctx.Err())
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return res, ErrTimeout
	case cmd.ProcessState == nil:
		return res, fmt.Errorf("%w: %w", ErrUnavailable, runErr)
	case res.ExitCode == ExitMemory:
		return res, ErrMemory
	case runErr != nil:
		return res, fmt.Errorf("%w: %w", ErrFailed, runErr)
	}
	return res, nil
}
