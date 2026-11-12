package extraction

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/ghislain95/hippo/backend/internal/childproc"
)

// Response exposes response to the tests of extraction_test.
type Response = response

// Outcome exposes outcome to the tests of extraction_test.
type Outcome = outcome

// ReadResponse reads the answer of a site as the worker does, but reads an HTML page or a PDF
// body in the test process, so that the tests of the reading need no child. provisional is both the title
// and the host of the link, as for a link just captured.
func ReadResponse(resp Response, provisional string) Outcome {
	l := link{title: provisional, host: provisional}
	if resp.Format == "" {
		resp.Format = sniffFormat(resp.ContentType, resp.Body)
	}
	out := readBody(resp, l)
	switch {
	case out.needsPage:
		out = readPage(resp.Body, resp.ContentType, resp.URL, l)
	case out.needsPDF:
		reading, finished := readPDFWithin(bytes.NewReader(resp.Body), int64(len(resp.Body)), nil, &pdfProgress{})
		out = pdfOutcome(reading, finished, resp.URL, l)
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
	spec, ok := (&pageReader{argv: argv, timeout: time.Second}).spec(childRequest{}, nil, nil)
	if !ok {
		panic("request not encoded")
	}
	return childproc.Command(ctx, spec, io.Discard)
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
	Mode      string // the mode it answered
	Reason    string // the reason it answered
}

// MeasureChild reads the HTML page of resp in a child run by argv and reports its memory.
func MeasureChild(argv []string, resp Response) (ChildMeasure, error) {
	return measure(argv, childRequest{ContentType: resp.ContentType, URL: resp.URL.String(), Title: "t", Host: "h"}, resp.Body, nil)
}

// measure runs the child of req as the worker does, but without time limit, and reports its
// memory.
func measure(argv []string, req childRequest, body []byte, pdf *os.File) (ChildMeasure, error) {
	spec, ok := (&pageReader{argv: argv, timeout: ReadTimeout}).spec(req, body, pdf)
	if !ok {
		return ChildMeasure{}, errors.New("request not encoded")
	}
	spec.Timeout = 0
	res, err := childproc.Run(context.Background(), spec)
	if errors.Is(err, childproc.ErrUnavailable) {
		return ChildMeasure{}, err
	}
	// Any other outcome is measured: the exit code is the measure.
	m := ChildMeasure{ExitCode: res.ExitCode, MaxRSS: res.MaxRSS}
	var result childResult
	if json.Unmarshal(res.Output, &result) == nil {
		m.PeakBytes = result.PeakBytes
		m.Mode, m.Reason = result.Mode, result.Reason
	}
	return m, nil
}

// PDFTitle exposes pdfTitle to the tests of extraction_test.
var PDFTitle = pdfTitle

// ReadPDF reads a PDF in the test process, as the child does but without time limit, for the
// link whose provisional title and host is provisional; it also returns the words counted.
func ReadPDF(pdf []byte, final *url.URL, provisional string) (Outcome, int64) {
	reading, finished := readPDFWithin(bytes.NewReader(pdf), int64(len(pdf)), nil, &pdfProgress{})
	return pdfOutcome(reading, finished, final, link{title: provisional, host: provisional}), reading.words
}

// RunChildStoppedAfterPages runs the child as RunChild does, with a clock that passes the
// deadline of a PDF reading once pages pages are committed.
func RunChildStoppedAfterPages(stdin io.Reader, stdout io.Writer, pages int) int {
	return runChild(stdin, stdout, childOptions{stopAfterPages: pages})
}

// ReadPDFInChild reads pdf as the worker does: written to a file of dir without name, read by
// a child run by argv, from the URL final, for a link whose provisional title and host is
// provisional.
func ReadPDFInChild(ctx context.Context, argv []string, timeout time.Duration, dir *TempDir, pdf []byte, final *url.URL, provisional string) (Outcome, error) {
	file, err := dir.create()
	if err != nil {
		return Outcome{}, err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(pdf); err != nil {
		return Outcome{}, err
	}
	r := &pageReader{argv: argv, timeout: timeout}
	return r.readPDF(ctx, file.r, final, link{title: provisional, host: provisional})
}

// ChildPDFCommandForTest builds the child of one PDF as the worker does, for its environment
// and descriptors.
func ChildPDFCommandForTest(ctx context.Context, argv []string, pdf *os.File) *exec.Cmd {
	spec, ok := (&pageReader{argv: argv, timeout: time.Second}).spec(childRequest{Format: formatPDF}, nil, pdf)
	if !ok {
		panic("request not encoded")
	}
	return childproc.Command(ctx, spec, io.Discard)
}

// MeasurePDFChild reads the PDF file in a child run by argv, as the worker does, and reports
// its memory.
func MeasurePDFChild(argv []string, file *os.File) (ChildMeasure, error) {
	req := childRequest{
		URL: "https://example.com/a.pdf", Title: "t", Host: "h", Format: formatPDF,
		DeadlineMS: int64(float64(ReadTimeout)*deadlineShare) / int64(time.Millisecond),
	}
	return measure(argv, req, nil, file)
}

// CreateTempFile creates a file without name in dir as the fetcher does, and returns its
// writing and reading descriptors and the function that closes both.
func CreateTempFile(dir *TempDir) (w io.Writer, r *os.File, closeBoth func() error, err error) {
	file, err := dir.create()
	if err != nil {
		return nil, nil, nil, err
	}
	return file, file.r, file.Close, nil
}

// WriterFile is the descriptor a temporary file is written through, for its mode.
func WriterFile(w io.Writer) *os.File {
	return w.(*tempFile).w
}

// Fetcher downloads as the worker does, with the address policy, resolver and certificate
// authorities given, PDFs written to dir.
func Fetcher(policy func(netip.AddrPort) bool, resolver Resolver, roots *x509.CertPool, timeout time.Duration, dir *TempDir) func(context.Context, *url.URL) (Response, error) {
	f := newFetcher(policy, resolver, roots, "test", dir)
	return func(ctx context.Context, target *url.URL) (Response, error) {
		return f.fetch(ctx, target, pageRequest(timeout))
	}
}

// FetchRequest exposes fetchRequest to the tests of extraction_test.
type FetchRequest = fetchRequest

// Reading modes of a fetch.
const (
	ModePage = modePage
	ModeHTML = modeHTML
	ModeJSON = modeJSON
)

// MaxJSONSize exposes maxJSONSize to the tests of extraction_test.
const MaxJSONSize = maxJSONSize

// FetcherWithRequest downloads as Fetcher does, with the options of each call.
func FetcherWithRequest(policy func(netip.AddrPort) bool, resolver Resolver, roots *x509.CertPool, dir *TempDir) func(context.Context, *url.URL, FetchRequest) (Response, error) {
	return newFetcher(policy, resolver, roots, "test", dir).fetch
}

// FetcherWithConn downloads as FetcherWithRequest does, every connection it dials wrapped by
// wrap: a test controls when a connection closes.
func FetcherWithConn(policy func(netip.AddrPort) bool, resolver Resolver, roots *x509.CertPool, dir *TempDir, wrap func(net.Conn) net.Conn) func(context.Context, *url.URL, FetchRequest) (Response, error) {
	f := newFetcher(policy, resolver, roots, "test", dir)
	f.transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := f.dial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		return wrap(conn), nil
	}
	return f.fetch
}

// FailWrites makes every temporary file of dir fail its writes, as on a full disk.
func FailWrites(dir *TempDir) { dir.failWrites = true }

// Classify exposes classify to the tests of extraction_test.
var Classify = classify

// PDFBytes reads the PDF a response holds, through the descriptor the child would get, and
// closes it.
func PDFBytes(resp Response) ([]byte, error) {
	if resp.PDF == nil {
		return nil, errors.New("no PDF file")
	}
	defer func() { _ = resp.PDF.Close() }()
	return io.ReadAll(io.NewSectionReader(resp.PDF.r, 0, MaxPDFSize+1))
}

// FreeSpaceMargin exposes freeSpaceMargin to the tests of extraction_test.
const FreeSpaceMargin = freeSpaceMargin

// SetFreeSpace makes dir measure its free space with free.
func SetFreeSpace(dir *TempDir, free func() (uint64, bool, error)) { dir.freeSpace = free }

// VideoID exposes videoID to the tests of extraction_test.
var VideoID = videoID

// CanonicalPage exposes canonicalPage to the tests of extraction_test.
var CanonicalPage = canonicalPage

// OEmbedURL exposes oembedURL to the tests of extraction_test.
var OEmbedURL = oembedURL

// ReadYouTube reads the player data of a video page in the test process, as the child does,
// for the video id and a link whose provisional title and host is provisional.
func ReadYouTube(body []byte, id, provisional string) Outcome {
	return readYouTube(body, id, link{title: provisional, host: provisional})
}

// ReadVideoInChild reads a video page as the worker does, in a child run by argv.
func ReadVideoInChild(ctx context.Context, argv []string, timeout time.Duration, body []byte, id, provisional string) (Outcome, error) {
	r := &pageReader{argv: argv, timeout: timeout}
	resp := response{Status: 200, ContentType: "text/html; charset=utf-8", Body: body, URL: canonicalPage(id)}
	return r.readYouTube(ctx, resp, id, link{title: provisional, host: provisional})
}

// ChildExitMemory exposes childExitMemory to the tests of extraction_test.
const ChildExitMemory = childExitMemory

// MaxChildOutput exposes maxChildOutput to the tests of extraction_test.
const MaxChildOutput = maxChildOutput

// MeasureVideoChild reads the video page body for the video id in a child run by argv, as the
// worker does, and reports its memory.
func MeasureVideoChild(argv []string, body []byte, id string) (ChildMeasure, error) {
	req := childRequest{
		ContentType: "text/html; charset=utf-8", URL: canonicalPage(id).String(), Title: "t", Host: "h",
		Format: formatYouTube, VideoID: id,
	}
	return measure(argv, req, body, nil)
}
