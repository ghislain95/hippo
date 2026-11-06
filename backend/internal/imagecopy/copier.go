package imagecopy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/ghislain95/hippo/backend/internal/childproc"
)

var (
	// ErrStopped: the context of the copy was done; nothing is to be sent for this pass.
	ErrStopped = errors.New("image copy stopped")
	// ErrUnavailable: the child could not start (binary missing, fork refused): the NAS is at
	// fault, not the image (RG-37); the pass of the user stops before submitting anything.
	ErrUnavailable = errors.New("image copier unavailable")
)

// Limits are the bounds of a copy. DefaultLimits are those of production; tests lower them.
// The child refuses any limit above the constants of the package.
type Limits struct {
	MaxPixels int64
	LongEdge  int
	Quality   int
	MaxOutput int64
	// Memory is the memory limit of the child, in bytes.
	Memory uint64
	// Timeout bounds one copy; zero or less means the constant Timeout.
	Timeout time.Duration
}

// DefaultLimits are the limits of production: MaxPixels, LongEdge, Quality, MaxOutput,
// ChildMemoryLimit and Timeout.
func DefaultLimits() Limits {
	return Limits{
		MaxPixels: MaxPixels, LongEdge: LongEdge, Quality: Quality, MaxOutput: MaxOutput,
		Memory: ChildMemoryLimit, Timeout: Timeout,
	}
}

// Copier runs the child that copies one image.
type Copier struct {
	argv   []string // the binary and its arguments, [executable, ChildCommand] in production
	limits Limits
}

// NewCopier returns a copier that runs the child argv (the binary and its arguments) under
// limits.
func NewCopier(argv []string, limits Limits) *Copier {
	if limits.Timeout <= 0 {
		limits.Timeout = Timeout // a copy is always bounded in time
	}
	return &Copier{argv: argv, limits: limits}
}

// Result is the outcome of a copy.
type Result struct {
	// Reason is ReasonOK when out holds the copy, else why the item goes without its image.
	Reason string
	// Width and Height are the size of the copy, Size its length in bytes.
	Width, Height int
	Size          int64
	// PeakBytes is the highest memory the child's runtime held, MaxRSS its resident set at its
	// peak, for the measures; 0 when unknown.
	PeakBytes uint64
	MaxRSS    int64
}

// Copy copies the image of asset, the stored file opened read-only, to out, a file open for
// reading and writing (the write end of extraction.TempDir.NewFile), rewritten from its start: when the
// reason is ReasonOK, out holds exactly Size bytes of JPEG; otherwise it is empty. The error is
// ErrStopped when ctx is done, ErrUnavailable when the child could not start, or the error of
// out; any outcome of the image itself is the Result's reason.
func (c *Copier) Copy(ctx context.Context, asset, out *os.File) (Result, error) {
	if c == nil || len(c.argv) == 0 || c.argv[0] == "" {
		return Result{}, fmt.Errorf("%w: no child to run", ErrUnavailable)
	}
	if err := reset(out); err != nil {
		return Result{}, err
	}
	line, err := json.Marshal(request{
		MaxPixels: c.limits.MaxPixels, LongEdge: c.limits.LongEdge, Quality: c.limits.Quality,
		MaxOutput: c.limits.MaxOutput, MemoryLimit: c.limits.Memory,
	})
	if err != nil {
		return Result{}, fmt.Errorf("image copy request: %w", err)
	}
	res, runErr := childproc.Run(ctx, childproc.Spec{
		Argv:    c.argv,
		Timeout: c.limits.Timeout,
		Stdin:   bytes.NewReader(append(line, '\n')),
		// Read-only, then the output: the only descriptors the child inherits, since Go opens
		// every other one close-on-exec.
		ExtraFiles: []*os.File{asset, out},
		MaxOutput:  maxAnswer,
		WaitDelay:  childWaitDelay,
	})
	result := Result{MaxRSS: res.MaxRSS}
	switch {
	case errors.Is(runErr, childproc.ErrStopped):
		return result, fmt.Errorf("%w: %w", ErrStopped, runErr)
	case errors.Is(runErr, childproc.ErrUnavailable):
		return result, fmt.Errorf("%w: %w", ErrUnavailable, runErr)
	case errors.Is(runErr, childproc.ErrTimeout):
		result.Reason = ReasonTimeout
	case errors.Is(runErr, childproc.ErrMemory):
		result.Reason = ReasonTooLarge
	case errors.Is(runErr, childproc.ErrFailed) || res.Overflow:
		result.Reason = ReasonCopyFailed
	default:
		result = c.read(res.Output, out, result)
	}
	if result.Reason != ReasonOK {
		result.Width, result.Height, result.Size = 0, 0, 0
		if err := reset(out); err != nil {
			return result, err
		}
	}
	return result, nil
}

// read turns the answer of a child that exited normally into a result, checked against the
// copy it wrote to out.
func (c *Copier) read(output []byte, out *os.File, result Result) Result {
	var ans answer
	if json.Unmarshal(output, &ans) != nil || !childReasons[ans.Reason] {
		result.Reason = ReasonCopyFailed
		return result
	}
	result.Reason, result.PeakBytes = ans.Reason, ans.PeakBytes
	if ans.Reason != ReasonOK {
		return result
	}
	info, err := out.Stat()
	switch {
	case err != nil || info.Size() == 0:
		result.Reason = ReasonCopyFailed
	case info.Size() > c.limits.MaxOutput:
		result.Reason = ReasonTooLarge
	case ans.Width < 1 || ans.Height < 1 || max(ans.Width, ans.Height) > c.limits.LongEdge:
		result.Reason = ReasonCopyFailed
	case !isJPEGOf(out, info.Size(), ans.Width, ans.Height):
		// Defence in depth: the parent reads the header of what it will send, and nothing else.
		result.Reason = ReasonCopyFailed
	default:
		result.Width, result.Height, result.Size = ans.Width, ans.Height, info.Size()
	}
	return result
}

// reset empties out and puts its offset, which the child shares, at its start. Its error names
// no file.
func reset(out *os.File) error {
	if err := out.Truncate(0); err != nil {
		return fmt.Errorf("reset the image copy file: %w", pathless(err))
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("reset the image copy file: %w", pathless(err))
	}
	return nil
}

// pathless drops the path of a file error.
func pathless(err error) error {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return fmt.Errorf("%s: %w", pathErr.Op, pathErr.Err)
	}
	return err
}

// isJPEGOf tells whether the size bytes of out are a JPEG whose header declares width×height.
func isJPEGOf(out *os.File, size int64, width, height int) bool {
	cfg, err := jpeg.DecodeConfig(io.NewSectionReader(out, 0, size))
	return err == nil && cfg.Width == width && cfg.Height == height
}
