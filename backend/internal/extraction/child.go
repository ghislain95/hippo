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
// few hundred kilobytes can build a tree of a hundred megabytes. A PDF is read there too, from
// its temporary file, handed to the child as descriptor 3. The child is bounded in memory and
// in time; whatever happens to it, the service keeps running (design.md).
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
	// childPDFDescriptor is the descriptor on which the child reads a PDF: the first of
	// exec.Cmd.ExtraFiles.
	childPDFDescriptor = 3
	// deadlineShare is the share of ReadTimeout after which the child answers with what it
	// read of a PDF, before the parent kills it.
	deadlineShare = 0.8
)

// childEnv is the whole environment of the child: never the service's, which holds the API
// token and the Anthropic key. Two processors at least, so that the watchdog runs while the
// HTML parser loops.
func childEnv() []string {
	return []string{"GOMAXPROCS=2"}
}

// childRequest is the first line the child reads; the body of a page follows, to the end. A
// PDF is not on stdin: it is the file of descriptor 3.
type childRequest struct {
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Host        string `json:"host"`
	// Format is formatPDF for a PDF, formatYouTube for a video page; "" for a page.
	Format string `json:"format,omitempty"`
	// VideoID is the video whose player data a video page is read for.
	VideoID string `json:"video_id,omitempty"`
	// DeadlineMS is when the child answers with what it read of a PDF, in milliseconds.
	DeadlineMS int64 `json:"deadline_ms,omitempty"`
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
// readPage or the PDF of descriptor 3 with readPDFWithin, writes the outcome as JSON to stdout
// and returns the exit code. It loads no configuration, opens no database and logs nothing.
// Above ChildMemoryLimit it exits at once with childExitMemory.
func RunChild(stdin io.Reader, stdout io.Writer) int {
	return runChild(stdin, stdout, childOptions{})
}

// childOptions are the test modes of the child.
type childOptions struct {
	// stopAfterPages, when set, is a clock that passes the deadline of a PDF reading once that
	// many pages are committed, instead of the time.
	stopAfterPages int
}

func runChild(stdin io.Reader, stdout io.Writer, opts childOptions) int {
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
	pageURL, err := url.Parse(req.URL)
	if err != nil {
		return childExitInput
	}
	l := link{title: req.Title, host: req.Host}

	var out outcome
	if req.Format == formatPDF {
		if out, err = readPDFChild(pageURL, l, time.Duration(req.DeadlineMS)*time.Millisecond, opts); err != nil {
			return childExitInput
		}
	} else {
		body, err := io.ReadAll(input)
		if err != nil {
			return childExitInput
		}
		if req.Format == formatYouTube {
			out = readYouTube(body, req.VideoID, l)
		} else {
			out = readPage(body, req.ContentType, pageURL, l)
		}
	}
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

// errNoPDF: the child was asked for a PDF without a file on descriptor 3.
var errNoPDF = errors.New("no PDF file on descriptor 3")

// readPDFChild reads the PDF of descriptor 3 for the link l served from pageURL, and answers
// with what it read at deadline (RG-23, RG-28). The library never sees an error message of
// its own reach stdout: readPDF maps them to reasons.
func readPDFChild(pageURL *url.URL, l link, deadline time.Duration, opts childOptions) (outcome, error) {
	file := os.NewFile(childPDFDescriptor, "pdf")
	if file == nil {
		return outcome{}, errNoPDF
	}
	defer func() { _ = file.Close() }() // read only
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return outcome{}, errNoPDF
	}
	if deadline <= 0 {
		deadline = time.Duration(float64(ReadTimeout) * deadlineShare)
	}
	stop := make(chan struct{})
	progress := &pdfProgress{}
	if opts.stopAfterPages > 0 {
		var once sync.Once
		progress.afterCommit = func(r pdfReading) {
			if r.pagesStarted >= opts.stopAfterPages {
				once.Do(func() { close(stop) })
				select {} // the reading never goes further: the deadline took what it committed
			}
		}
	} else {
		timer := time.AfterFunc(deadline, func() { close(stop) })
		defer timer.Stop()
	}
	reading, finished := readPDFWithin(file, info.Size(), stop, progress)
	return pdfOutcome(reading, finished, pageURL, l), nil
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

// childReasons are the reasons the child gives, by format and mode: an answer with another one
// is not the child's.
var childReasons = map[string]map[string]map[string]bool{
	formatHTML: {
		ModeFull:     {reasonOK: true},
		ModeFallback: {reasonShort: true, reasonTooLarge: true, reasonNotHTML: true, reasonReadingFailed: true},
	},
	formatPDF: {
		ModeFull: {reasonOK: true},
		ModeFallback: {
			reasonShort: true, reasonEncrypted: true, reasonDamaged: true, reasonTimeout: true, reasonReadingFailed: true,
		},
	},
	formatYouTube: {
		ModeFallback: {reasonOK: true, reasonNoMetadata: true},
	},
}

// command builds the child for one page, or for one PDF given as descriptor 3; its
// environment is childEnv only.
func (r *pageReader) command(ctx context.Context, req childRequest, body []byte, pdf *os.File, stdout io.Writer) (*exec.Cmd, error) {
	header, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}
	cmd := exec.CommandContext(ctx, r.argv[0], r.argv[1:]...)
	cmd.Env = childEnv()
	cmd.Stdin = io.MultiReader(bytes.NewReader(append(header, '\n')), bytes.NewReader(body))
	if pdf != nil {
		// Read-only, and the only descriptor the child inherits: Go opens every other one
		// close-on-exec.
		cmd.ExtraFiles = []*os.File{pdf}
	}
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
	req := childRequest{ContentType: resp.ContentType, URL: resp.URL.String(), Title: l.title, Host: l.host}
	return r.run(ctx, req, resp.Body, nil, func(reason string) outcome { return fallback(reason, l) })
}

// readYouTube reads the video page of resp for the player data of the video id, for the link
// l, in a child process. Its errors are those of read; a child that fails gives the fallback of
// l with the reason of its failure.
func (r *pageReader) readYouTube(ctx context.Context, resp response, id string, l link) (outcome, error) {
	req := childRequest{
		ContentType: resp.ContentType, URL: resp.URL.String(), Title: l.title, Host: l.host,
		Format: formatYouTube, VideoID: id,
	}
	out, err := r.run(ctx, req, resp.Body, nil, func(reason string) outcome { return fallback(reason, l) })
	out.Format = formatYouTube
	return out, err
}

// readPDF reads the PDF of file, served from final, for the link l in a child process, which
// answers before the parent's limit with what it read (deadlineShare). Its errors are those of
// read.
func (r *pageReader) readPDF(ctx context.Context, file *os.File, final *url.URL, l link) (outcome, error) {
	req := childRequest{
		URL: final.String(), Title: l.title, Host: l.host, Format: formatPDF,
		DeadlineMS: max(1, int64(float64(r.timeout)*deadlineShare)/int64(time.Millisecond)),
	}
	return r.run(ctx, req, nil, file, func(reason string) outcome { return pdfFallback(reason, final, l) })
}

// run runs the child for req and turns its answer into an outcome; fallbackFor builds the
// fallback of a child that did not answer.
func (r *pageReader) run(ctx context.Context, req childRequest, body []byte, pdf *os.File, fallbackFor func(string) outcome) (outcome, error) {
	readCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	output := &cappedBuffer{limit: maxChildOutput}
	cmd, err := r.command(readCtx, req, body, pdf, output)
	if err != nil {
		return fallbackFor(reasonReadingFailed), nil
	}
	runErr := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return outcome{}, errReaderStopped
	case errors.Is(readCtx.Err(), context.DeadlineExceeded):
		return fallbackFor(reasonTimeout), nil
	case cmd.ProcessState == nil:
		return outcome{}, fmt.Errorf("%w: %w", errReaderUnavailable, runErr)
	case cmd.ProcessState.ExitCode() == childExitMemory:
		return fallbackFor(reasonTooLarge), nil
	case runErr != nil || output.overflow:
		return fallbackFor(reasonReadingFailed), nil
	}
	format := req.Format
	if format == "" {
		format = formatHTML
	}
	var result childResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !childReasons[format][result.Mode][result.Reason] {
		return fallbackFor(reasonReadingFailed), nil
	}
	out := outcome{Reason: result.Reason, Extraction: store.Extraction{
		Mode: result.Mode, Title: result.Title, SiteName: result.SiteName, ContentText: result.ContentText,
		ContentTruncated: result.ContentTruncated, ReadingMinutes: result.ReadingMinutes,
	}}
	if format == formatPDF {
		out.Format = formatPDF
	}
	return out, nil
}

// cappedBuffer keeps the first limit bytes written and drops the rest, so that a child that
// writes too much neither fills the memory of the service nor blocks on its pipe. The buffer is
// a field, not embedded: an embedded bytes.Buffer would lend its ReadFrom, which io.Copy (and
// exec.Cmd with it) prefers to Write, and the limit would never apply.
type cappedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.overflow = true
		if room > 0 {
			b.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

// Bytes returns what was kept.
func (b *cappedBuffer) Bytes() []byte { return b.buf.Bytes() }
