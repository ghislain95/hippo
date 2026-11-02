package extraction

import (
	"bytes"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// Extraction modes (docs/glossary.md).
const (
	ModeFull     = "full"
	ModeFallback = "fallback"
	ModeNone     = "none"
)

// Reason classes of the logs: why an item got its mode, or why it waits.
const (
	reasonOK              = "ok"
	reasonTimeout         = "timeout"
	reasonStatus          = "status"
	reasonNotHTML         = "not_html"
	reasonTooLarge        = "too_large"
	reasonBlocked         = "blocked"
	reasonShort           = "short"
	reasonCrashedBefore   = "crashed_before"
	reasonDeferredYouTube = "deferred_youtube"
	reasonNetwork         = "network"
	// reasonUnreachable: the site failed before answering (refused connection, TLS error, host
	// that does not exist, redirect loop).
	reasonUnreachable = "unreachable"
	// reasonReadingFailed: the process reading the page failed (a panic, an abnormal exit, an
	// unreadable answer); the item falls back instead of crashing the service.
	reasonReadingFailed = "reading_failed"
	// reasonReaderUnavailable: the process that reads pages could not start; the item stays
	// queued.
	reasonReaderUnavailable = "reader_unavailable"
	// reasonStorageUnavailable: the NAS cannot store a PDF in its temporary file; the item
	// stays queued.
	reasonStorageUnavailable = "storage_unavailable"
)

// MaxElements is the largest document Readability reads, in elements of its tree: it bounds
// the time of the reading. The memory is bounded by the process that reads (child.go).
const MaxElements = 20_000

// response is what the fetcher got from the site.
type response struct {
	Status      int
	ContentType string
	Format      string // formatHTML, formatPDF or formatOther; "" before the headers
	Body        []byte // nil when TooLarge or a PDF, the first bytes only of formatOther
	PDF         *tempFile
	TooLarge    bool
	URL         *url.URL // the final URL, after redirects
}

// Formats of an answer, as the fetcher recognises it before reading its body.
const (
	formatHTML  = "html"  // HTML, or no declared type: read in memory, up to MaxBodySize
	formatPDF   = "pdf"   // a PDF: streamed to a temporary file, up to MaxPDFSize
	formatOther = "other" // anything else: not read beyond its first bytes
)

// outcome is what an extraction decided for a link: the item is written with Extraction.
type outcome struct {
	Reason     string
	Status     int    // the HTTP status, 0 when there was no answer
	Format     string // formatPDF for a PDF, logged; "" otherwise
	Extraction store.Extraction

	needsPage bool // the answer is an HTML page: readPage decides, in the reading process
	needsPDF  bool // the answer is a PDF: the reading process reads its file
}

// link is what an extraction keeps of a link whose page gives nothing better: its current
// title, the provisional one (RG-26), and its host name in the same form (lower case, no
// port), "" when the URL has none.
type link struct {
	title, host string
}

// fallback is the outcome of a link whose page cannot be read: its title kept, its host as site
// name, no content (RG-23).
func fallback(reason string, l link) outcome {
	return outcome{Reason: reason, Extraction: store.Extraction{
		Mode: ModeFallback, Title: &l.title, SiteName: optional(l.host),
	}}
}

// readBody decides what the answer of a site is for the link l, from what is cheap to read:
// the status, the format, the size. An HTML page comes back with needsPage, for readPage; a
// PDF with needsPDF, for the reading process. The metadata of the page is read only on a success status: the
// og:title of a 404 page would be "Page not found".
func readBody(resp response, l link) outcome {
	if resp.Status < 200 || resp.Status > 299 {
		return fallback(reasonStatus, l)
	}
	switch resp.Format {
	case formatPDF:
		if resp.TooLarge {
			return pdfFallback(reasonTooLarge, resp.URL, l)
		}
		return outcome{Format: formatPDF, needsPDF: true}
	case formatOther:
		return fallback(reasonNotHTML, l)
	}
	if resp.TooLarge {
		return fallback(reasonTooLarge, l)
	}
	if mediaTypeOf(resp.ContentType) == "" && !strings.HasPrefix(http.DetectContentType(resp.Body), "text/html") {
		return fallback(reasonNotHTML, l)
	}
	return outcome{needsPage: true}
}

// sniffFormat tells the format of an answer from its Content-Type and its first bytes: a PDF
// whatever its declared type when it starts as one (RG-28), HTML when declared so or not
// declared, else another format.
func sniffFormat(contentType string, head []byte) string {
	mediaType := mediaTypeOf(contentType)
	switch {
	case mediaType == "application/pdf" || bytes.HasPrefix(head, []byte("%PDF-")):
		return formatPDF
	case mediaType == "" || mediaType == "text/html" || mediaType == "application/xhtml+xml":
		return formatHTML
	default:
		return formatOther
	}
}

// mediaTypeOf is the media type of a Content-Type header, in lower case, "" when absent.
func mediaTypeOf(contentType string) string {
	if mediaType, _, err := mime.ParseMediaType(contentType); err == nil {
		return mediaType
	}
	before, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(before))
}

// readPage reads an HTML page: its declared encoding, its og:* metadata and <title>, then its
// main content through Readability (RG-21, RG-24, RG-25, RG-26). Its memory follows the page,
// not its size: it runs only in the reading process (RunChild), never in the service.
func readPage(body []byte, contentType string, pageURL *url.URL, l link) (out outcome) {
	defer func() {
		// Readability walks any document a site sends; a page that makes it panic falls back
		// rather than taking the service down.
		if recover() != nil {
			out = fallback(reasonReadingFailed, l)
		}
	}()

	decoded, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return fallback(reasonNotHTML, l)
	}
	doc, err := html.Parse(decoded)
	if err != nil {
		return fallback(reasonNotHTML, l)
	}
	meta := readMeta(doc)

	parser := readability.NewParser()
	parser.MaxElemsToParse = MaxElements
	// The document is ours: no need for Readability to clone it first.
	article, err := parser.ParseAndMutate(doc, pageURL)
	if err != nil {
		// The only error of ParseAndMutate: a document over MaxElements.
		return meta.fallback(reasonTooLarge, "", l)
	}
	siteName := cutTitle(firstOf(meta.siteName, clean(article.SiteName()), l.host))

	var text string
	if article.Node != nil {
		var b strings.Builder
		if err := article.RenderText(&b); err == nil {
			text = strings.TrimSpace(strings.ToValidUTF8(b.String(), "�"))
		}
	}
	if utf8.RuneCountInString(text) < minFullLength {
		return meta.fallback(reasonShort, siteName, l)
	}

	content, truncated := truncate(text)
	minutes := readingMinutes(text) // RG-25: the original, before truncation
	return outcome{Reason: reasonOK, Extraction: store.Extraction{
		Mode:             ModeFull,
		Title:            optional(cutTitle(firstOf(clean(article.Title()), meta.ogTitle, meta.title, l.title))),
		SiteName:         optional(siteName),
		ContentText:      &content,
		ContentTruncated: truncated,
		ReadingMinutes:   &minutes,
	}}
}

// pageMeta is the metadata of a page, cleaned; "" when absent.
type pageMeta struct {
	ogTitle, ogDescription, siteName, title string
}

// fallback is the outcome of a page read with a success status whose content is missing:
// og:title, else <title>, else the link's title; og:description as content (RG-23), truncated
// and marked as any content (RG-24). siteName "" means og:site_name, else the host.
func (m pageMeta) fallback(reason, siteName string, l link) outcome {
	if siteName == "" {
		siteName = cutTitle(firstOf(m.siteName, l.host))
	}
	description, truncated := truncate(m.ogDescription)
	return outcome{Reason: reason, Extraction: store.Extraction{
		Mode:             ModeFallback,
		Title:            optional(cutTitle(firstOf(m.ogTitle, m.title, l.title))),
		SiteName:         optional(siteName),
		ContentText:      optional(description),
		ContentTruncated: truncated,
	}}
}

// readMeta reads og:title, og:description, og:site_name and the first <title> of doc.
func readMeta(doc *html.Node) pageMeta {
	var meta pageMeta
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Namespace == "" {
			switch n.Data {
			case "meta":
				key, content := metaKeyContent(n)
				switch key {
				case "og:title":
					meta.ogTitle = firstOf(meta.ogTitle, clean(content))
				case "og:description":
					meta.ogDescription = firstOf(meta.ogDescription, clean(content))
				case "og:site_name":
					meta.siteName = firstOf(meta.siteName, clean(content))
				}
			case "title":
				if meta.title == "" {
					meta.title = clean(textOf(n))
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return meta
}

// metaKeyContent returns the property (or name) of a <meta> element, in lower case, and its
// content.
func metaKeyContent(n *html.Node) (key, content string) {
	for _, attr := range n.Attr {
		switch strings.ToLower(attr.Key) {
		case "property":
			key = strings.ToLower(strings.TrimSpace(attr.Val))
		case "name":
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(attr.Val))
			}
		case "content":
			content = attr.Val
		}
	}
	return key, content
}

func textOf(n *html.Node) string {
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode {
			b.WriteString(child.Data)
		}
	}
	return b.String()
}

// firstOf returns the first value that is not empty.
func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// optional is nil for "", so that an absent value is stored as NULL.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
