// Package extraction is the domain of the extraction: what happens to a captured item between
// queued and extracted, in the background and without any AI call (RG-20 to RG-28). A link is
// downloaded once, through a client of its own that never reaches a local or private address;
// its page is read locally with Readability, or its PDF, streamed to a temporary file without
// name, with github.com/ledongthuc/pdf and a layout layer of its own; both are read in a child
// process bounded in memory and time. A YouTube video is read from its canonical page, for its
// title, channel, description and duration, in the same child, else from its oEmbed answer;
// it has no transcript in V1 (RG-22, ADR-0037). A note or an image has nothing to extract. It
// is the only domain that imports net/http, and the only package that imports Readability,
// golang.org/x/net/html and the PDF reader (docs/dev/conventions-backend.md).
//
// Private items are extracted like the others: the extraction asks the captured URL from its
// own site and sends nothing else; for a video, the canonical page and oEmbed of YouTube, which
// carry the video id only (RG-92 exception). Nothing here talks to the AI (RG-12).
package extraction

import (
	"context"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
)

const (
	// SafetyInterval is how often the worker looks for queued items without being woken.
	SafetyInterval = 5 * time.Minute
	// OutagePause is how long the worker waits after a network outage of the NAS before it
	// resumes.
	OutagePause = 5 * time.Minute
	// outageProbes is the number of links of other hosts a pass still tries after a network
	// outage: enough to tell an outage of the NAS from a site that fails alone.
	outageProbes = 2
	// pageSize is the number of queued items read at a time.
	pageSize = 50
	// MaxNetworkRetries is the number of counted network outages after which a link falls back,
	// reason network: an error read as an outage of the NAS but due to the site cannot hold the
	// link queued forever. An outage counts only when another link reached its site since the
	// previous one, so that a real outage of the NAS never makes a link fall back.
	MaxNetworkRetries = 3
	// writeTimeout bounds the writes that must happen even when the worker is stopping.
	writeTimeout = 10 * time.Second
	// reasonStopped: the backend is stopping; the item stays queued. Never logged.
	reasonStopped = "stopped"
)

// Options configures a Worker.
type Options struct {
	Store *store.Store
	// Policy tells whether an address may be dialled (RG-27): PublicAddress in production.
	Policy func(netip.AddrPort) bool
	// Resolver resolves host names; nil uses the resolver of the system.
	Resolver Resolver
	// RootCAs are the certificate authorities trusted; nil uses those of the system.
	RootCAs *x509.CertPool
	// Logger receives one line per extraction, with identifiers, mode, reason and duration
	// only: never a URL, a host, a title, a content nor a download error.
	Logger *slog.Logger
	// Version is the version of the backend, sent in the User-Agent.
	Version string
	// Timeout bounds a whole download; 0 means DownloadTimeout.
	Timeout time.Duration
	// ReaderCommand runs the child process that reads a page: the binary of the service and
	// ChildCommand in production; in tests, the test binary. Required.
	ReaderCommand []string
	// ReadTimeout bounds the reading of a page in the child; 0 means ReadTimeout.
	ReadTimeout time.Duration
	// TempDir is where PDFs are downloaded, DATA_DIR/tmp in production. Required.
	TempDir *TempDir
	// After is time.After when nil; tests control the safety net and the outage pause with it.
	After func(time.Duration) <-chan time.Time
	// Now is time.Now when nil; it measures the durations logged.
	Now func() time.Time

	afterPass func() // called after each pass; tests wait on it instead of sleeping
}

// Worker extracts queued items, one at a time, oldest first, in a single goroutine (Run).
type Worker struct {
	store     *store.Store
	fetcher   *fetcher
	reader    *pageReader
	timeout   time.Duration // a whole download, a video's two requests together (RG-23)
	logger    *slog.Logger
	after     func(time.Duration) <-chan time.Time
	now       func() time.Time
	wake      chan struct{}
	afterPass func()

	// Owned by the goroutine of Run. siteAnswers counts the links whose download reached their
	// site since start-up; postponedAt holds its value at the last outage of each link.
	siteAnswers uint64
	postponedAt map[string]uint64
}

// NewWorker returns a worker configured by opts. Opts.Store, Policy and Logger are required.
func NewWorker(opts Options) *Worker {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = systemResolver{resolver: &net.Resolver{}}
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DownloadTimeout
	}
	after := opts.After
	if after == nil {
		after = time.After
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	readTimeout := opts.ReadTimeout
	if readTimeout == 0 {
		readTimeout = ReadTimeout
	}
	afterPass := opts.afterPass
	if afterPass == nil {
		afterPass = func() {}
	}
	return &Worker{
		store:       opts.Store,
		fetcher:     newFetcher(opts.Policy, resolver, opts.RootCAs, opts.Version, opts.TempDir),
		reader:      &pageReader{argv: opts.ReaderCommand, timeout: readTimeout},
		timeout:     timeout,
		logger:      opts.Logger,
		after:       after,
		now:         now,
		wake:        make(chan struct{}, 1),
		afterPass:   afterPass,
		postponedAt: map[string]uint64{},
	}
}

// Wake asks the worker for a pass; it never blocks. A capture calls it after creating an item,
// and the backend at start-up.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default: // a pass is already pending
	}
}

// Run extracts queued items each time the worker is woken, and every SafetyInterval, until ctx
// is done. A download under way when ctx is done is abandoned and its item stays queued. Run
// returns once nothing is being written any more, so that the store can be closed.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-w.after(SafetyInterval):
		}
		for w.pass(ctx) == stepOutage {
			select {
			case <-ctx.Done():
				return
			case <-w.after(OutagePause):
			}
		}
	}
}

// step is what the extraction of one item tells the pass.
type step int

const (
	stepNext    step = iota // go on with the next item
	stepOutage              // the link met a network outage of the NAS: pause after the pass
	stepStop                // the worker is stopping
	stepPause               // the NAS cannot read pages: end the pass and pause
	stepStorage             // the NAS cannot store a PDF: go on with the items that need no file, pause after the pass
)

// pass extracts the queued items of every user, each user's on their own (RG-90). A network
// outage does not stop it at once: it still tries outageProbes links of other hosts, which tell
// an outage of the NAS (they fail too) from a site that fails alone (they succeed), skipping
// the links of the hosts that failed; then it returns stepOutage, so that the worker pauses.
func (w *Worker) pass(ctx context.Context) (result step) {
	outage := false
	// storageFull: a PDF could not be stored. The pass goes on, notes, images and pages need no
	// file, and a later PDF is refused by the free space check after its headers; the worker
	// pauses after the pass.
	storageFull := false
	failedHosts := map[string]bool{}
	probes := 0
	defer w.afterPass()
	users, err := w.store.UserIDs(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return stepStop
		}
		w.logStoreError(ctx, "list users for extraction", "", err)
		return stepNext
	}
	for _, userID := range users {
		after := ""
		for {
			items, err := w.store.QueuedItems(ctx, userID, after, pageSize)
			if err != nil {
				if ctx.Err() != nil {
					return stepStop
				}
				w.logStoreError(ctx, "list queued items", "", err)
				return stepNext
			}
			if len(items) == 0 {
				break
			}
			for _, item := range items {
				after = item.ID
				if ctx.Err() != nil {
					return stepStop
				}
				host := downloadHost(item)
				if outage && host != "" {
					if failedHosts[host] {
						continue // its host just failed: left queued for the next pass
					}
					if probes == outageProbes {
						return stepOutage
					}
					probes++
				}
				switch w.extract(ctx, userID, item) {
				case stepStop:
					return stepStop
				case stepPause:
					return stepOutage
				case stepStorage:
					storageFull = true
				case stepOutage:
					outage = true
					failedHosts[host] = true
				case stepNext:
				}
			}
		}
	}
	if outage || storageFull {
		return stepOutage
	}
	return stepNext
}

// extract extracts one queued item of userID.
func (w *Worker) extract(ctx context.Context, userID string, item store.QueuedItem) step {
	start := w.now()
	if item.Kind != "link" {
		// A note or an image has nothing to extract.
		w.write(ctx, userID, item.ID, outcome{Reason: reasonOK, Extraction: store.Extraction{Mode: ModeNone}}, start, false)
		return stepNext
	}

	var rawURL string
	if item.URL != nil {
		rawURL = strings.TrimSpace(*item.URL)
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		w.write(ctx, userID, item.ID, fallback(reasonUnreachable, link{title: item.Title}), start, false)
		return stepNext
	}
	// The host in the form of the provisional title (RG-26): the site name of a fallback.
	l := link{title: item.Title, host: strings.ToLower(target.Hostname())}
	if item.AttemptStarted {
		// The previous attempt never finished: the page may have crashed the backend. It is not
		// downloaded again.
		w.write(ctx, userID, item.ID, fallback(reasonCrashedBefore, l), start, false)
		return stepNext
	}

	if err := w.store.StartExtractionAttempt(ctx, userID, item.ID); err != nil {
		if ctx.Err() != nil {
			return stepStop
		}
		w.logStoreError(ctx, "start extraction attempt", item.ID, err)
		return stepNext
	}
	if id, ok := videoID(target); ok {
		return w.extractVideo(ctx, userID, item, id, l, start)
	}
	resp, err := w.fetcher.fetch(ctx, target, pageRequest(w.timeout))
	if resp.PDF != nil {
		// Both descriptors of the file are closed on every path; its data goes with them.
		defer func() { _ = resp.PDF.Close() }() // no name, nothing to flush: a failure loses nothing
	}
	if err != nil {
		switch reason := classify(ctx, err); reason {
		case reasonStopped:
			w.releaseAttempt(ctx, userID, item.ID)
			return stepStop
		case reasonStorageUnavailable:
			// The NAS cannot store the PDF (no room, read-only volume): the item waits, the pass
			// goes on with the others, then pauses. The error names no file nor URL.
			w.releaseAttempt(ctx, userID, item.ID)
			w.logger.ErrorContext(ctx, "temporary file unavailable", slog.String("item_id", item.ID),
				slog.String("reason", reason), slog.String("format", formatPDF), slog.String("error", err.Error()),
				w.duration(start))
			return stepStorage
		case reasonNetwork:
			return w.networkOutage(ctx, userID, item, l, start)
		default:
			out := fallback(reason, l)
			if resp.Format == formatPDF {
				// The body of a PDF failed after its headers: its file name is its title (RG-28).
				out = pdfFallback(reason, resp.URL, l)
			}
			// A refusal of the address policy never reached the site.
			w.write(ctx, userID, item.ID, out, start, reason != reasonBlocked)
			return stepNext
		}
	}

	out := readBody(resp, l)
	if out.needsPage || out.needsPDF {
		if out.needsPDF {
			out, err = w.reader.readPDF(ctx, resp.PDF.r, resp.URL, l)
			_ = resp.PDF.Close() // read: the disk space is released before the write
		} else {
			out, err = w.reader.read(ctx, resp, l)
		}
		switch {
		case errors.Is(err, errReaderStopped):
			w.releaseAttempt(ctx, userID, item.ID)
			return stepStop
		case errors.Is(err, errReaderUnavailable):
			// The NAS is at fault (binary missing, fork refused): the item waits, and the pass
			// pauses rather than downloading every page for nothing. The error names the binary
			// only, never the page.
			w.releaseAttempt(ctx, userID, item.ID)
			w.logger.ErrorContext(ctx, "page reader unavailable", slog.String("item_id", item.ID),
				slog.String("reason", reasonReaderUnavailable), slog.String("error", err.Error()), w.duration(start))
			return stepPause
		}
	}
	out.Status = resp.Status
	w.write(ctx, userID, item.ID, out, start, true)
	return stepNext
}

// extractVideo extracts the YouTube video id of item, whose attempt has started: its
// canonical page, read in the child for its player data, else its oEmbed answer, else the
// fallback of l (RG-22, RG-23, RG-26). The two requests share the timeout of a download, 2/3
// for the page and 1/3 for oEmbed. A stop or a network outage of the NAS on either request
// leaves the item queued, as for a page.
func (w *Worker) extractVideo(ctx context.Context, userID string, item store.QueuedItem, id string, l link, start time.Time) step {
	itemID := item.ID
	reachedSite := false
	var out outcome
	page, err := w.fetcher.fetch(ctx, canonicalPage(id), fetchRequest{Timeout: w.timeout * 2 / 3, Accept: acceptHeader, Mode: modeHTML})
	reason := ""
	if err != nil {
		reason = classify(ctx, err)
		switch reason {
		case reasonStopped:
			w.releaseAttempt(ctx, userID, itemID)
			return stepStop
		case reasonNetwork:
			// oEmbed is not asked: it is the same site, behind the same network.
			return w.networkOutage(ctx, userID, item, l, start)
		}
		reachedSite = reason != reasonBlocked // a refusal of the address policy never reached the site
	} else {
		reachedSite = true
		reason = videoPageReason(page)
		if reason == "" {
			out, err = w.reader.readYouTube(ctx, page, id, l)
			switch {
			case errors.Is(err, errReaderStopped):
				w.releaseAttempt(ctx, userID, itemID)
				return stepStop
			case errors.Is(err, errReaderUnavailable):
				w.releaseAttempt(ctx, userID, itemID)
				w.logger.ErrorContext(ctx, "page reader unavailable", slog.String("item_id", itemID),
					slog.String("reason", reasonReaderUnavailable), slog.String("error", err.Error()), w.duration(start))
				return stepPause
			}
			reason = out.Reason
		}
	}

	oembedStatus := 0
	if reason != reasonOK {
		answer, err := w.fetcher.fetch(ctx, oembedURL(id), fetchRequest{Timeout: w.timeout / 3, Accept: jsonAccept, Mode: modeJSON})
		if err != nil {
			switch oembedReason := classify(ctx, err); oembedReason {
			case reasonStopped:
				w.releaseAttempt(ctx, userID, itemID)
				return stepStop
			case reasonNetwork:
				return w.networkOutage(ctx, userID, item, l, start)
			default:
				reachedSite = reachedSite || oembedReason != reasonBlocked
			}
		} else {
			reachedSite = true
			oembedStatus = answer.Status
		}
		var ok bool
		if err == nil && isSuccess(answer.Status) && isYouTube(answer.URL.Hostname()) && !answer.TooLarge {
			out, ok = oembedOutcome(answer.Body, l)
		}
		if !ok {
			out = fallback(reason, l)
		}
	}
	out.Format = formatYouTube
	out.Status = page.Status
	out.OEmbedStatus = oembedStatus
	w.write(ctx, userID, itemID, out, start, reachedSite)
	return stepNext
}

// videoPageReason tells why the answer to a video's page cannot hold its player data, "" when
// it can: a success status, a final URL on YouTube, an HTML body within MaxBodySize.
func videoPageReason(page response) string {
	switch {
	case !isSuccess(page.Status):
		return reasonStatus
	case page.URL == nil || !isYouTube(page.URL.Hostname()):
		// A redirect off YouTube (a check for robots): its body is not YouTube's.
		return reasonNoMetadata
	case page.TooLarge:
		return reasonTooLarge
	case page.Format != formatHTML:
		return reasonNotHTML
	case mediaTypeOf(page.ContentType) == "" && !strings.HasPrefix(http.DetectContentType(page.Body), "text/html"):
		return reasonNotHTML
	}
	return ""
}

// isSuccess tells whether status is 2xx.
func isSuccess(status int) bool {
	return status >= 200 && status <= 299
}

// networkOutage handles a link whose download met a network outage of the NAS. The outage
// counts only when another link reached its site since this link's previous outage (since
// start-up for its first one): during a real outage of the NAS, nothing reaches a site and the
// link waits, queued; a site that fails alone while the others answer falls back at its
// MaxNetworkRetries-th counted outage.
func (w *Worker) networkOutage(ctx context.Context, userID string, item store.QueuedItem, l link, start time.Time) step {
	counted := w.siteAnswers > w.postponedAt[item.ID]
	retries := item.NetworkRetries
	if counted {
		retries++
	}
	if retries >= MaxNetworkRetries {
		w.write(ctx, userID, item.ID, fallback(reasonNetwork, l), start, false)
		return stepOutage
	}
	w.postponedAt[item.ID] = w.siteAnswers
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	if err := w.store.PostponeExtraction(writeCtx, userID, item.ID, counted); err != nil {
		w.logStoreError(ctx, "postpone extraction", item.ID, err)
	}
	w.logger.WarnContext(ctx, "item extraction postponed", slog.String("item_id", item.ID),
		slog.String("reason", reasonNetwork), slog.Int("network_retries", retries), w.duration(start))
	return stepOutage
}

// write stores the outcome of an item and logs it. The write happens even when the worker is
// stopping: a page already read is not lost, and its attempt row does not survive to make it
// fall back at the next start. reachedSite tells that the download got as far as the site
// (an answer, or a failure of the site itself): the network of the NAS works.
func (w *Worker) write(ctx context.Context, userID, itemID string, out outcome, start time.Time, reachedSite bool) {
	delete(w.postponedAt, itemID)
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	written, err := w.store.WriteExtraction(writeCtx, userID, itemID, out.Extraction)
	if err != nil {
		w.logStoreError(ctx, "write extraction", itemID, err)
		// With its attempt released, the item is extracted again rather than falling back.
		w.releaseAttempt(ctx, userID, itemID)
		return
	}
	if !written {
		return // deleted or changed meanwhile
	}
	if reachedSite {
		w.siteAnswers++
	}
	attrs := []slog.Attr{
		slog.String("item_id", itemID), slog.String("mode", out.Extraction.Mode), slog.String("reason", out.Reason),
	}
	if out.Format != "" {
		attrs = append(attrs, slog.String("format", out.Format))
	}
	if out.Status != 0 {
		attrs = append(attrs, slog.Int("http_status", out.Status))
	}
	if out.OEmbedStatus != 0 {
		attrs = append(attrs, slog.Int("oembed_status", out.OEmbedStatus))
	}
	attrs = append(attrs, w.duration(start))
	w.logger.LogAttrs(ctx, slog.LevelInfo, "item extracted", attrs...)
}

// releaseAttempt marks the attempt of an item left queued on purpose (stop, failed write) as no
// longer under way, so that it is not taken for a crash at the next start.
func (w *Worker) releaseAttempt(ctx context.Context, userID, itemID string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	if err := w.store.ReleaseExtractionAttempt(writeCtx, userID, itemID); err != nil {
		w.logStoreError(ctx, "release extraction attempt", itemID, err)
	}
}

func (w *Worker) duration(start time.Time) slog.Attr {
	return slog.Int64("duration_ms", w.now().Sub(start).Milliseconds())
}

// logStoreError logs a failure of the database; its message holds no URL nor content.
func (w *Worker) logStoreError(ctx context.Context, msg, itemID string, err error) {
	attrs := []slog.Attr{slog.String("error", err.Error())}
	if itemID != "" {
		attrs = append(attrs, slog.String("item_id", itemID))
	}
	w.logger.LogAttrs(context.WithoutCancel(ctx), slog.LevelError, msg, attrs...)
}

// downloadHost is the host a queued item would be downloaded from, in lower case, "" when its
// extraction makes no request: a note, an image, a URL without host, a link whose previous
// attempt never finished. A YouTube video is asked from www.youtube.com whatever its form.
func downloadHost(item store.QueuedItem) string {
	if item.Kind != "link" || item.URL == nil || item.AttemptStarted {
		return ""
	}
	target, err := url.Parse(strings.TrimSpace(*item.URL))
	if err != nil {
		return ""
	}
	if _, ok := videoID(target); ok {
		return youtubeHost
	}
	return strings.ToLower(target.Hostname())
}
