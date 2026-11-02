// Package extraction is the domain of the extraction: what happens to a captured item between
// queued and extracted, in the background and without any AI call (RG-20 to RG-27). A link's
// page is downloaded once, through a client of its own that never reaches a local or private
// address, and read locally with Readability; a note or an image has nothing to extract. It is
// the only domain that imports net/http, and the only package that imports Readability and
// golang.org/x/net/html (docs/dev/conventions-backend.md).
//
// Private items are extracted like the others: the extraction asks the captured URL from its
// own site and sends nothing else (RG-92 exception); nothing here talks to the AI (RG-12).
package extraction

import (
	"context"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
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
		fetcher:     newFetcher(opts.Policy, resolver, opts.RootCAs, opts.Version, timeout),
		reader:      &pageReader{argv: opts.ReaderCommand, timeout: readTimeout},
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
	stepNext   step = iota // go on with the next item
	stepOutage             // the link met a network outage of the NAS: pause after the pass
	stepStop               // the worker is stopping
	stepPause              // the NAS cannot read pages: end the pass and pause
)

// pass extracts the queued items of every user, each user's on their own (RG-90). A network
// outage does not stop it at once: it still tries outageProbes links of other hosts, which tell
// an outage of the NAS (they fail too) from a site that fails alone (they succeed), skipping
// the links of the hosts that failed; then it returns stepOutage, so that the worker pauses.
func (w *Worker) pass(ctx context.Context) (result step) {
	outage := false
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
				case stepOutage:
					outage = true
					failedHosts[host] = true
				case stepNext:
				}
			}
		}
	}
	if outage {
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
	if isYouTube(target.Hostname()) {
		// E2-06 extracts YouTube links; until then they wait, untouched (RG-63).
		w.logger.DebugContext(ctx, "item extraction deferred", slog.String("item_id", item.ID),
			slog.String("reason", reasonDeferredYouTube))
		return stepNext
	}
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
	resp, err := w.fetcher.fetch(ctx, target)
	if err != nil {
		switch reason := classify(ctx, err); reason {
		case reasonStopped:
			w.releaseAttempt(ctx, userID, item.ID)
			return stepStop
		case reasonNetwork:
			return w.networkOutage(ctx, userID, item, l, start)
		default:
			// A refusal of the address policy never reached the site.
			w.write(ctx, userID, item.ID, fallback(reason, l), start, reason != reasonBlocked)
			return stepNext
		}
	}

	out := readBody(resp, l)
	if out.needsPage {
		out, err = w.reader.read(ctx, resp, l)
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
	if out.Deferred {
		// E2-08 extracts PDFs from the attempt row; the item waits, and is not downloaded again.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
		defer cancel()
		if err := w.store.DeferExtraction(writeCtx, userID, item.ID); err != nil {
			w.logStoreError(ctx, "defer extraction", item.ID, err)
			return stepNext
		}
		w.siteAnswers++ // the site answered
		delete(w.postponedAt, item.ID)
		w.logger.InfoContext(ctx, "item extraction deferred", slog.String("item_id", item.ID),
			slog.String("reason", out.Reason), slog.Int("http_status", out.Status), w.duration(start))
		return stepNext
	}
	w.write(ctx, userID, item.ID, out, start, true)
	return stepNext
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
	if out.Status != 0 {
		attrs = append(attrs, slog.Int("http_status", out.Status))
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
// extraction makes no request: a note, an image, a URL without host, a YouTube link, a link
// whose previous attempt never finished.
func downloadHost(item store.QueuedItem) string {
	if item.Kind != "link" || item.URL == nil || item.AttemptStarted {
		return ""
	}
	target, err := url.Parse(strings.TrimSpace(*item.URL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(target.Hostname())
	if isYouTube(host) {
		return ""
	}
	return host
}

// isYouTube tells whether host is youtube.com, one of its subdomains, or youtu.be.
func isYouTube(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "youtube.com" || strings.HasSuffix(host, ".youtube.com") || host == "youtu.be"
}
