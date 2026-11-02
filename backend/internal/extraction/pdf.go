package extraction

import (
	"bytes"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	lpdf "github.com/ledongthuc/pdf"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// A PDF is read in the child process (child.go), from the file the service downloaded it to
// (tempfile.go), through github.com/ledongthuc/pdf and the layout layer of pdflayout.go
// (RG-28, design.md). The library panics on malformed input and loops on some hostile
// structures: the opening, the title and every page are read under recover, the page tree is
// walked by this code within bounds, and the reading runs in a goroutine that the child leaves
// at its deadline with what was committed.
const (
	// MaxPDFSize is the largest PDF downloaded, in bytes (the 50 MB of RG-28 design); a larger
	// one falls back.
	MaxPDFSize = 50 << 20
	// MaxPDFPages is the number of pages read at most; the pages beyond are ignored.
	MaxPDFPages = 20_000
	// maxPageTreeDepth and maxPageTreeNodes bound the walk of the page tree: a tree beyond them
	// (one that contains itself) is damaged.
	maxPageTreeDepth = 32
	maxPageTreeNodes = 40_000
	// wordCommitInterval is how often, in words, the reading commits its count within a page,
	// so that a deadline in an enormous page still counts what was read.
	wordCommitInterval = 100_000
)

// Reasons of a PDF's fallback, besides short, too_large, timeout and reading_failed.
const (
	// reasonEncrypted: the PDF needs a password, or uses an encryption the reader refuses
	// (AES-256).
	reasonEncrypted = "encrypted"
	// reasonDamaged: the PDF cannot be opened (truncated, broken cross-reference table, not a
	// PDF despite its type) or its page tree is out of bounds.
	reasonDamaged = "damaged"
)

// pdfReading is what the reading of a PDF committed. text shares the array of the sink that
// keeps it: the sink only appends, so the bytes committed are never written again.
type pdfReading struct {
	opened       bool
	failure      string // reasonEncrypted or reasonDamaged
	failed       bool   // the reading panicked outside its recovered parts
	title        string // the metadata title, raw
	text         []byte
	words        int64
	pagesFound   int
	pagesStarted int
}

// pdfProgress is what the reading goroutine commits and the child takes at its deadline.
type pdfProgress struct {
	mu          sync.Mutex
	committed   pdfReading
	stopped     bool
	afterCommit func(pdfReading) // tests: called after each commit, outside the lock
}

func (p *pdfProgress) commit(r pdfReading) {
	p.mu.Lock()
	if !p.stopped {
		p.committed = r
	}
	hook := p.afterCommit
	p.mu.Unlock()
	if hook != nil {
		hook(r)
	}
}

// stop returns what was committed; later commits are ignored.
func (p *pdfProgress) stop() pdfReading {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	return p.committed
}

func (p *pdfProgress) fail() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.committed.failed = true
}

// readPDFWithin reads the PDF r of size bytes in a goroutine. It returns what was read and
// true once the reading ends, or what was committed and false when deadline comes first; a
// nil deadline never comes. A reading left at its deadline goes on until the child exits.
func readPDFWithin(r io.ReaderAt, size int64, deadline <-chan struct{}, progress *pdfProgress) (pdfReading, bool) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				progress.fail()
			}
		}()
		readPDF(r, size, progress)
	}()
	select {
	case <-done:
		return progress.stop(), true
	case <-deadline:
		return progress.stop(), false
	}
}

// readPDF opens the PDF, reads its title, counts its pages, then reads them in order,
// committing to progress after each page and when the kept text is full.
func readPDF(r io.ReaderAt, size int64, progress *pdfProgress) {
	doc, failure := openPDF(r, size)
	if doc == nil {
		progress.commit(pdfReading{failure: failure})
		return
	}
	state := pdfReading{opened: true, title: pdfMetadataTitle(doc)}
	progress.commit(state)

	root, ok := recovered(func() lpdf.Value { return doc.Trailer().Key("Root").Key("Pages") })
	if !ok {
		state.failure = reasonDamaged
		progress.commit(state)
		return
	}
	found := 0
	if err := walkPageTree(root, func(lpdf.Value, lpdf.Value) bool {
		found++
		return found < MaxPDFPages
	}); err != nil {
		state.failure = reasonDamaged
		progress.commit(state)
		return
	}
	state.pagesFound = found

	sink := &textSink{keepLimit: MaxContentLength + 1}
	started := 0
	commit := func() {
		state.text, state.words, state.pagesStarted = sink.kept, sink.words, started
		progress.commit(state)
	}
	sink.commit = commit
	lay := &layout{sink: sink}
	// The walk was bounded once: it is the same tree.
	_ = walkPageTree(root, func(page, resources lpdf.Value) bool {
		started++
		readPDFPage(lay, page, resources)
		commit()
		return started < found
	})
	commit()
}

// openPDF opens the PDF, or tells why it cannot: reasonEncrypted or reasonDamaged. The error of
// the library is dropped: it quotes bytes of the file.
func openPDF(r io.ReaderAt, size int64) (doc *lpdf.Reader, failure string) {
	defer func() {
		if recover() != nil {
			doc, failure = nil, reasonDamaged
		}
	}()
	doc, err := lpdf.NewReader(withPDFHeader(r), size)
	if err != nil {
		// The library refuses an encryption it does not support (AES-256, an unknown version
		// or handler) with a message about it, and a missing password with ErrInvalidPassword.
		if errors.Is(err, lpdf.ErrInvalidPassword) || strings.Contains(err.Error(), "encryption") {
			return nil, reasonEncrypted
		}
		return nil, reasonDamaged
	}
	return doc, ""
}

// pdfHeader is the header the library is shown: it refuses the versions after 1.7, and a
// header line not ended right after the version.
var pdfHeader = []byte("%PDF-1.7\n")

// withPDFHeader returns r, whose first bytes read as pdfHeader when it starts with a header
// "%PDF-d.d": offsets do not move, the header line is a comment. Version 2.0 shares the syntax
// of 1.7; what it does not share, AES-256, is refused.
func withPDFHeader(r io.ReaderAt) io.ReaderAt {
	head := make([]byte, 8)
	if n, _ := r.ReadAt(head, 0); n != len(head) || !bytes.HasPrefix(head, []byte("%PDF-")) ||
		!isDigit(head[5]) || head[6] != '.' || !isDigit(head[7]) {
		return r
	}
	return headerOverlay{r}
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

type headerOverlay struct{ io.ReaderAt }

func (h headerOverlay) ReadAt(p []byte, off int64) (int, error) {
	n, err := h.ReaderAt.ReadAt(p, off)
	if off >= 0 && off < int64(len(pdfHeader)) && n > 0 {
		copy(p[:n], pdfHeader[off:])
	}
	return n, err
}

// pdfMetadataTitle is /Info /Title, "" when absent or unreadable.
func pdfMetadataTitle(doc *lpdf.Reader) string {
	title, _ := recovered(func() string { return doc.Trailer().Key("Info").Key("Title").Text() })
	return title
}

// readPDFPage lays out one page; a page whose reading panics is skipped from where it failed.
func readPDFPage(lay *layout, page, resources lpdf.Value) {
	defer func() { _ = recover() }() // a malformed page: the next one is read
	contents := page.Key("Contents")
	if contents.Kind() != lpdf.Stream && contents.Kind() != lpdf.Array {
		return
	}
	lay.page(contents, resources)
}

// errPageTree: the page tree goes beyond its bounds.
var errPageTree = errors.New("page tree out of bounds")

// walkPageTree visits the pages of the tree under root, depth first in /Kids order, with the
// /Resources they inherit, until visit returns false. Neither Reader.Page, which walks the
// tree from its root for each page and loops on a cycle, nor Page.Resources, which follows
// /Parent without bound, is called. A kid that cannot be read is skipped. A node met twice (a
// cycle, or a page listed twice) makes the tree out of bounds: a page tree is a tree.
func walkPageTree(root lpdf.Value, visit func(page, resources lpdf.Value) bool) error {
	w := &treeWalk{visit: visit, seen: map[string]bool{}}
	return w.node(root, lpdf.Value{}, 0)
}

type treeWalk struct {
	visit   func(page, resources lpdf.Value) bool
	nodes   int
	stopped bool
	seen    map[string]bool // the references of the nodes met, "id gen"
}

// objectReference matches a reference as the library prints it ("3 0 R"); its lexer reads
// every "n g R" of a dictionary or an array as one. The library does not expose the reference
// of a resolved value.
var objectReference = regexp.MustCompile(`(\d+) (\d+) R`)

// references returns every reference written in the /Kids array, whatever else it holds.
func references(kids lpdf.Value) []string {
	matches := objectReference.FindAllStringSubmatch(kids.String(), -1)
	refs := make([]string, len(matches))
	for i, m := range matches {
		refs[i] = m[1] + " " + m[2]
	}
	return refs
}

func (w *treeWalk) node(v, inherited lpdf.Value, depth int) error {
	if depth > maxPageTreeDepth {
		return errPageTree
	}
	if w.nodes++; w.nodes > maxPageTreeNodes {
		return errPageTree
	}
	resources := inherited
	if own := v.Key("Resources"); own.Kind() == lpdf.Dict {
		resources = own
	}
	kids := v.Key("Kids")
	if kids.Kind() != lpdf.Array {
		if !w.visit(v, resources) {
			w.stopped = true
		}
		return nil
	}
	for _, ref := range references(kids) {
		if w.seen[ref] {
			return errPageTree
		}
		w.seen[ref] = true
	}
	for i := 0; i < kids.Len() && !w.stopped; i++ {
		var err error
		if _, ok := recovered(func() bool {
			kid := kids.Index(i)
			if kid.Kind() == lpdf.Dict {
				err = w.node(kid, resources, depth+1)
			}
			return true
		}); ok && err != nil {
			return err
		}
	}
	return nil
}

// recovered calls f, and tells false when it panicked.
func recovered[T any](f func() T) (value T, ok bool) {
	defer func() {
		if recover() != nil {
			var zero T
			value, ok = zero, false
		}
	}()
	return f(), true
}

// pdfOutcome is the outcome of a PDF read for the link l from the URL final, from what the
// reading committed; finished tells that it ended before its deadline (RG-23, RG-24, RG-25,
// RG-28).
func pdfOutcome(r pdfReading, finished bool, final *url.URL, l link) outcome {
	meta := ""
	if r.opened {
		meta = r.title
	}
	title := pdfTitle(meta, final, l.title)
	fallbackFor := func(reason string) outcome { return pdfFallbackTitled(reason, title, l) }
	switch {
	case r.failed:
		return fallbackFor(reasonReadingFailed)
	case r.failure != "":
		return fallbackFor(r.failure)
	case !r.opened:
		return fallbackFor(reasonTimeout) // the deadline came while opening
	}
	text := string(r.text)
	if utf8.RuneCountInString(text) < minFullLength {
		if finished {
			return fallbackFor(reasonShort)
		}
		return fallbackFor(reasonTimeout)
	}
	content, truncated := truncate(text)
	words := r.words
	if !finished && r.pagesStarted > 0 && r.pagesFound > r.pagesStarted {
		// The pages not read count as many words as the pages read, pro rata (RG-25).
		words = words * int64(r.pagesFound) / int64(r.pagesStarted)
	}
	minutes := max(1, (words+wordsPerMinute-1)/wordsPerMinute)
	return outcome{Reason: reasonOK, Format: formatPDF, Extraction: store.Extraction{
		Mode:        ModeFull,
		Title:       &title,
		SiteName:    optional(l.host),
		ContentText: &content,
		// A reading stopped by its deadline does not hold the whole document (RG-24).
		ContentTruncated: truncated || !finished,
		ReadingMinutes:   &minutes,
	}}
}

// pdfFallback is the fallback of a PDF whose document was not read: its title is the file name
// of final, else the provisional title (RG-28).
func pdfFallback(reason string, final *url.URL, l link) outcome {
	return pdfFallbackTitled(reason, pdfTitle("", final, l.title), l)
}

func pdfFallbackTitled(reason, title string, l link) outcome {
	return outcome{Reason: reason, Format: formatPDF, Extraction: store.Extraction{
		Mode: ModeFallback, Title: &title, SiteName: optional(l.host),
	}}
}
