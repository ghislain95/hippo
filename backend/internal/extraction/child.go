package extraction

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"sync"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// The reading of a page runs in a child process of the same binary, `hippo extract-page`: the
// memory of an HTML tree follows the markup, not the size of the page, and a hostile page of a
// few hundred kilobytes can build a tree of a hundred megabytes. The child is bounded in
// memory and in time; whatever happens to it, the service keeps running (design.md).
const (
	// ChildCommand is the subcommand of the binary that reads one page (RunChild).
	ChildCommand = "extract-page"
	// ChildMemoryLimit is the memory the child's runtime may hold; above it, the child exits
	// with childExitMemory and the link falls back (too_large).
	ChildMemoryLimit = 48 << 20
	// childSoftLimit makes the garbage collector of the child work harder before the hard limit,
	// so that garbage is not taken for a heavy page.
	childSoftLimit = 36 << 20
	// ReadTimeout bounds the reading of a page, apart from the 15 s of its download.
	ReadTimeout = 10 * time.Second
	// childExitMemory is the exit code of a child that reached ChildMemoryLimit.
	childExitMemory = 86
	// childExitInput is the exit code of a child that could not read its request.
	childExitInput = 2
	// maxChildHeader bounds the request line the child reads before the body.
	maxChildHeader = 64 << 10
	// maxChildOutput bounds the answer the parent reads: a content of 120 000 code points is
	// at most 720 KB in JSON, where only control characters are escaped (6 bytes each).
	maxChildOutput = 2 << 20
	// childWaitDelay bounds the wait for the pipes of a child killed at its time limit.
	childWaitDelay = time.Second
	// watchInterval is how often the child samples its memory.
	watchInterval = time.Millisecond
)

// childEnv is the whole environment of the child: never the service's, which holds the API
// token and the Anthropic key. Two processors at least, so that the watchdog runs while the
// HTML parser loops.
func childEnv() []string {
	return []string{"GOMAXPROCS=2"}
}

// childRequest is the first line the child reads; the body of the page follows, to the end.
type childRequest struct {
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Host        string `json:"host"`
}

// childResult is what the child writes: the outcome of readPage, and the peak of memory its
// runtime held, for the measures.
type childResult struct {
	Reason           string  `json:"reason"`
	Mode             string  `json:"mode"`
	Title            *string `json:"title"`
	SiteName         *string `json:"site_name"`
	ContentText      *string `json:"content_text"`
	ContentTruncated bool    `json:"content_truncated"`
	ReadingMinutes   *int64  `json:"reading_minutes"`
	PeakBytes        uint64  `json:"peak_bytes"`
}

// RunChild is the child process: it reads one request from stdin, reads the page with
// readPage, writes the outcome as JSON to stdout and returns the exit code. It loads no
// configuration, opens no database and logs nothing. Above ChildMemoryLimit it exits at once
// with childExitMemory.
func RunChild(stdin io.Reader, stdout io.Writer) int {
	debug.SetMemoryLimit(childSoftLimit)
	if runtime.GOMAXPROCS(0) < 2 {
		runtime.GOMAXPROCS(2)
	}
	watch := watchMemory(ChildMemoryLimit, func() { os.Exit(childExitMemory) })
	defer watch.stop()

	input := bufio.NewReader(io.LimitReader(stdin, maxChildHeader+MaxBodySize))
	line, err := input.ReadBytes('\n')
	if err != nil {
		return childExitInput
	}
	var req childRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return childExitInput
	}
	body, err := io.ReadAll(input)
	if err != nil {
		return childExitInput
	}
	pageURL, err := url.Parse(req.URL)
	if err != nil {
		return childExitInput
	}

	out := readPage(body, req.ContentType, pageURL, link{title: req.Title, host: req.Host})
	e := out.Extraction
	result := childResult{
		Reason: out.Reason, Mode: e.Mode, Title: e.Title, SiteName: e.SiteName, ContentText: e.ContentText,
		ContentTruncated: e.ContentTruncated, ReadingMinutes: e.ReadingMinutes, PeakBytes: watch.peak(),
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false) // <, > and & stay one byte: the answer stays under maxChildOutput
	if err := encoder.Encode(result); err != nil {
		return childExitInput
	}
	return 0
}

// memoryWatch samples the memory the runtime holds and calls exit above a limit.
type memoryWatch struct {
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
	max     uint64 // written by the watch only; read after stop
}

// watchMemory starts a goroutine that reads, every watchInterval, the memory the runtime holds
// (all its classes, minus the heap returned to the system), and calls exit above limit.
func watchMemory(limit uint64, exit func()) *memoryWatch {
	w := &memoryWatch{done: make(chan struct{}), stopped: make(chan struct{})}
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	go func() {
		defer close(w.stopped)
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		for {
			metrics.Read(samples)
			held := samples[0].Value.Uint64() - samples[1].Value.Uint64()
			w.max = max(w.max, held)
			if held > limit {
				exit()
			}
			select {
			case <-w.done:
				return
			case <-ticker.C:
			}
		}
	}()
	return w
}

// stop ends the watch and waits for it; it may run more than once.
func (w *memoryWatch) stop() {
	w.once.Do(func() { close(w.done) })
	<-w.stopped
}

// peak stops the watch and returns the highest memory it saw.
func (w *memoryWatch) peak() uint64 {
	w.stop()
	return w.max
}

// pageReader runs the child that reads a page.
type pageReader struct {
	argv    []string // the binary and its arguments, ChildCommand in production
	timeout time.Duration
}

var (
	// errReaderStopped: the worker is stopping while a page is read; the item stays queued.
	errReaderStopped = errors.New("page reading stopped")
	// errReaderUnavailable: the child could not start (binary missing, fork refused): the NAS is
	// at fault, not the page; the item stays queued.
	errReaderUnavailable = errors.New("page reader unavailable")
)

// childReasons are the reasons readPage gives, by mode: an answer with another one is not the
// child's.
var childReasons = map[string]map[string]bool{
	ModeFull:     {reasonOK: true},
	ModeFallback: {reasonShort: true, reasonTooLarge: true, reasonNotHTML: true, reasonReadingFailed: true},
}

// command builds the child for one page; its environment is childEnv only.
func (r *pageReader) command(ctx context.Context, req childRequest, body []byte, stdout io.Writer) (*exec.Cmd, error) {
	header, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}
	cmd := exec.CommandContext(ctx, r.argv[0], r.argv[1:]...)
	cmd.Env = childEnv()
	cmd.Stdin = io.MultiReader(bytes.NewReader(append(header, '\n')), bytes.NewReader(body))
	cmd.Stdout = stdout
	cmd.Stderr = nil // discarded: a panic message may quote the page
	cmd.WaitDelay = childWaitDelay
	dieWithParent(cmd)
	return cmd, nil
}

// read reads the HTML page of resp for the link l in a child process. It returns
// errReaderStopped when ctx, the worker's, is done, and errReaderUnavailable when the child
// could not start.
func (r *pageReader) read(ctx context.Context, resp response, l link) (outcome, error) {
	readCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	output := &cappedBuffer{limit: maxChildOutput}
	cmd, err := r.command(readCtx, childRequest{
		ContentType: resp.ContentType, URL: resp.URL.String(), Title: l.title, Host: l.host,
	}, resp.Body, output)
	if err != nil {
		return fallback(reasonReadingFailed, l), nil
	}
	runErr := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return outcome{}, errReaderStopped
	case errors.Is(readCtx.Err(), context.DeadlineExceeded):
		return fallback(reasonTimeout, l), nil
	case cmd.ProcessState == nil:
		return outcome{}, fmt.Errorf("%w: %w", errReaderUnavailable, runErr)
	case cmd.ProcessState.ExitCode() == childExitMemory:
		return fallback(reasonTooLarge, l), nil
	case runErr != nil || output.overflow:
		return fallback(reasonReadingFailed, l), nil
	}
	var result childResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !childReasons[result.Mode][result.Reason] {
		return fallback(reasonReadingFailed, l), nil
	}
	return outcome{Reason: result.Reason, Extraction: store.Extraction{
		Mode: result.Mode, Title: result.Title, SiteName: result.SiteName, ContentText: result.ContentText,
		ContentTruncated: result.ContentTruncated, ReadingMinutes: result.ReadingMinutes,
	}}, nil
}

// cappedBuffer keeps the first limit bytes written and drops the rest, so that a child that
// writes too much neither fills the memory of the service nor blocks on its pipe.
type cappedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); len(p) > room {
		b.overflow = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
