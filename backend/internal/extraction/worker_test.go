package extraction_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	_ "time/tzdata" // the Paris zone of the capture without the machine's time zone database

	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
	"github.com/ghislain95/hippo/backend/internal/extraction/pdftest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// waitLimit bounds every wait on the worker: a test that reaches it has failed.
const waitLimit = 20 * time.Second

// web is a fictional web served over TLS by one httptest server: every host of example.com,
// youtube.com and youtu.be resolves to it through the test resolver, so that no test touches
// the network.
type web struct {
	server   *httptest.Server
	addr     netip.AddrPort
	roots    *x509.CertPool
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc // host + path
	requests map[string]int
	headers  map[string]http.Header
	asked    []string                    // host + path + "?" + raw query of every request, in order
	videos   map[string]http.HandlerFunc // the watch page of each video id
	oembeds  map[string]http.HandlerFunc // the oEmbed answer of each video id
	conns    atomic.Int64
	lookups  atomic.Int64
	failDNS  atomic.Bool
	failing  map[string]bool // hosts whose resolution fails as on a NAS outage
}

func newWeb(t *testing.T) *web {
	t.Helper()
	w := &web{
		handlers: map[string]http.HandlerFunc{}, requests: map[string]int{}, headers: map[string]http.Header{}, failing: map[string]bool{},
		videos: map[string]http.HandlerFunc{}, oembeds: map[string]http.HandlerFunc{},
	}
	// The watch page and oEmbed of YouTube dispatch on their query: v for a page, the v of the
	// page in url for oEmbed.
	w.handle("https://www.youtube.com/watch", func(rw http.ResponseWriter, r *http.Request) {
		w.dispatch(w.videos, r.URL.Query().Get("v"), rw, r)
	})
	w.handle("https://www.youtube.com/oembed", func(rw http.ResponseWriter, r *http.Request) {
		page, err := url.Parse(r.URL.Query().Get("url"))
		if err != nil || r.URL.Query().Get("format") != "json" {
			http.Error(rw, "Bad Request", http.StatusBadRequest)
			return
		}
		w.dispatch(w.oembeds, page.Query().Get("v"), rw, r)
	})
	cert, roots := testCertificate(t)
	w.roots = roots
	w.server = httptest.NewUnstartedServer(http.HandlerFunc(w.serve))
	w.server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	w.server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			w.conns.Add(1)
		}
	}
	w.server.StartTLS()
	t.Cleanup(w.server.Close)
	w.addr = netip.MustParseAddrPort(w.server.Listener.Addr().String())
	return w
}

func (w *web) handle(rawURL string, handler http.HandlerFunc) {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[u.Host+u.Path] = handler
}

func (w *web) page(rawURL, contentType string, body []byte) {
	w.handle(rawURL, func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", contentType)
		_, _ = rw.Write(body)
	})
}

func (w *web) serve(rw http.ResponseWriter, r *http.Request) {
	key := r.Host + r.URL.Path
	w.mu.Lock()
	w.asked = append(w.asked, key+"?"+r.URL.RawQuery)
	w.requests[key]++
	w.headers[key] = r.Header.Clone()
	handler := w.handlers[key]
	w.mu.Unlock()
	if handler == nil {
		http.NotFound(rw, r)
		return
	}
	handler(rw, r)
}

// dispatch serves the handler of id among handlers, 404 when there is none.
func (w *web) dispatch(handlers map[string]http.HandlerFunc, id string, rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	handler := handlers[id]
	w.mu.Unlock()
	if handler == nil {
		http.NotFound(rw, r)
		return
	}
	handler(rw, r)
}

// video serves handler as the watch page of the video id.
func (w *web) video(id string, handler http.HandlerFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.videos[id] = handler
}

// videoPage serves body as the watch page of the video id, as YouTube does: HTML, 200.
func (w *web) videoPage(id string, body []byte) {
	w.video(id, func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = rw.Write(body)
	})
}

// oembed serves handler as the oEmbed answer of the video id.
func (w *web) oembed(id string, handler http.HandlerFunc) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.oembeds[id] = handler
}

// oembedAnswer serves an oEmbed answer of the video id with status; a success holds title and
// author.
func (w *web) oembedAnswer(id string, status int, title, author string) {
	w.oembed(id, func(rw http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			http.Error(rw, "Bad Request", status)
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_, _ = rw.Write(oembedJSON(id, title, author))
	})
}

// requested is every request the web received, host + path + "?" + raw query, in order.
func (w *web) requested() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.asked...)
}

// failHost makes the resolution of host fail as on a NAS outage, while the others succeed.
func (w *web) failHost(host string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failing[host] = true
}

func (w *web) requestsTo(rawURL string) int {
	u, _ := url.Parse(rawURL)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.requests[u.Host+u.Path]
}

func (w *web) headersOf(rawURL string) http.Header {
	u, _ := url.Parse(rawURL)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.headers[u.Host+u.Path]
}

// Resolve resolves every host of example.com to the server; others do not exist. failDNS makes
// it fail as a NAS whose resolver cannot be reached.
func (w *web) Resolve(_ context.Context, host string, _ uint16) ([]netip.AddrPort, error) {
	w.lookups.Add(1)
	w.mu.Lock()
	failing := w.failing[host]
	w.mu.Unlock()
	if w.failDNS.Load() || failing {
		return nil, &net.DNSError{Err: "dial udp: connect: network is unreachable", Name: host, IsTemporary: true}
	}
	for _, domain := range []string{"example.com", "youtube.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return []netip.AddrPort{w.addr}, nil
		}
	}
	if host == "youtu.be" {
		return []netip.AddrPort{w.addr}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// testCertificate is a self-signed certificate of example.com, youtube.com, their subdomains,
// and youtu.be.
func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "example.com"},
		DNSNames:              []string{"example.com", "*.example.com", "youtube.com", "*.youtube.com", "youtu.be"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, roots
}

// syncBuffer collects the logs of the worker and of the capture.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// policyRecorder wraps an address policy and records the addresses it was asked about.
type policyRecorder struct {
	mu     sync.Mutex
	policy func(netip.AddrPort) bool
	asked  []netip.AddrPort
}

func (p *policyRecorder) allow(addr netip.AddrPort) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, addr)
	return p.policy(addr)
}

func (p *policyRecorder) addresses() []netip.AddrPort {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]netip.AddrPort(nil), p.asked...)
}

// testPolicy is the production policy that also allows loopback, for the httptest servers.
func testPolicy(addr netip.AddrPort) bool {
	return addr.Addr().Unmap().IsLoopback() || extraction.PublicAddress(addr)
}

// relay forwards Wake to the worker currently running, so that the capture service outlives a
// restart of the worker; with no worker running, a capture wakes nothing.
type relay struct {
	mu     sync.Mutex
	worker *extraction.Worker
	muted  bool // captures wake nothing, so that a test decides when the next pass starts
}

func (r *relay) Wake() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.worker != nil && !r.muted {
		r.worker.Wake()
	}
}

func (r *relay) mute(muted bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.muted = muted
}

// wakeNow wakes the running worker, even when captures are muted.
func (r *relay) wakeNow() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.worker.Wake()
}

func (r *relay) set(w *extraction.Worker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.worker = w
}

// harness is a backend reduced to the capture and the extraction, on a real SQLite file.
type harness struct {
	t        *testing.T
	store    *store.Store
	capture  *capture.Service
	owner    string
	other    string
	web      *web
	policy   *policyRecorder
	resolver extraction.Resolver
	timeout  time.Duration
	reader   []string      // the child that reads pages: the test binary as extract-page
	readFor  time.Duration // the time limit of a reading
	dbPath   string
	tmp      *extraction.TempDir
	tmpPath  string // the temporary directory of the PDFs
	logs     *syncBuffer
	relay    *relay
	passes   chan struct{}
	pause    chan time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st, dbPath := storetest.OpenWithPath(t)
	ctx := context.Background()
	owner, err := st.CreateUser(ctx, "owner", "hash-owner")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	other, err := st.CreateUser(ctx, "other", "hash-other")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatalf("LoadParis: %v", err)
	}
	h := &harness{
		t: t, store: st, dbPath: dbPath, owner: owner, other: other, web: newWeb(t),
		policy: &policyRecorder{policy: testPolicy}, logs: &syncBuffer{}, relay: &relay{},
		passes: make(chan struct{}, 1), pause: make(chan time.Time),
	}
	h.resolver = h.web
	h.reader = childArgv(extraction.ChildCommand)
	h.tmp, h.tmpPath = tempDir(t)
	h.capture = capture.NewService(st, h.relay, paris, slog.New(slog.NewJSONHandler(h.logs, nil)))
	t.Cleanup(func() {
		// Whatever the test, the logs never name a site or an address (RG-90, ADR-0027).
		for _, secret := range []string{"example.com", "youtube.com", "youtu.be", "127.0.0.1", "localhost", "192.168.", "10.0.0.5", "Tuning SQLite", h.tmpPath, ".pdf"} {
			if strings.Contains(h.logs.String(), secret) {
				t.Errorf("logs contain %q:\n%s", secret, h.logs.String())
			}
		}
	})
	return h
}

// start runs a new worker, as the backend does at start-up, and waits for its first pass. It
// returns the function that stops it and waits for Run to return.
func (h *harness) start() (stop func()) {
	h.t.Helper()
	opts := extraction.Options{
		Store: h.store, Policy: h.policy.allow, Resolver: h.resolver, RootCAs: h.web.roots,
		Logger:  slog.New(slog.NewJSONHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Version: "test", Timeout: h.timeout, ReaderCommand: h.reader, ReadTimeout: h.readFor, TempDir: h.tmp,
		// The safety net never fires; the outage pause ends when the test says so.
		After: func(d time.Duration) <-chan time.Time {
			if d == extraction.OutagePause {
				return h.pause
			}
			return nil
		},
	}
	worker := extraction.NewWorker(extraction.WithAfterPass(opts, func() {
		select {
		case h.passes <- struct{}{}:
		default:
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx)
	}()
	// The pass that a stopped worker ended with is not a pass of this one.
	select {
	case <-h.passes:
	default:
	}
	h.relay.set(worker)
	worker.Wake()
	h.waitPass()
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		h.relay.set(nil)
		cancel()
		select {
		case <-done:
		case <-time.After(waitLimit):
			h.t.Fatal("the worker did not stop")
		}
	}
	h.t.Cleanup(stop)
	return stop
}

// endPause ends the outage pause of the worker, which then makes a pass.
func (h *harness) endPause() {
	h.t.Helper()
	select {
	case h.pause <- time.Now():
	case <-time.After(waitLimit):
		h.t.Fatal("the worker is not pausing after an outage")
	}
}

// waitPass waits for the end of the next pass of the worker.
func (h *harness) waitPass() {
	h.t.Helper()
	select {
	case <-h.passes:
	case <-time.After(waitLimit):
		h.t.Fatal("the worker made no pass")
	}
}

// captureLink captures rawURL for the owner; a running worker is woken.
func (h *harness) captureLink(rawURL string, private bool) store.Item {
	h.t.Helper()
	return h.captureFor(h.owner, rawURL, private)
}

func (h *harness) captureFor(userID, rawURL string, private bool) store.Item {
	h.t.Helper()
	result, err := h.capture.Capture(context.Background(), userID, capture.Request{URL: &rawURL, IsPrivate: &private})
	if err != nil {
		h.t.Fatalf("Capture: %v", err)
	}
	if !result.Created || result.Item.Status != "queued" {
		h.t.Fatalf("capture created %v status %q, want a new queued item", result.Created, result.Item.Status)
	}
	return result.Item
}

// extractLink captures rawURL with a running worker and returns the item after the pass.
func (h *harness) extractLink(rawURL string) store.Item {
	h.t.Helper()
	h.start()
	item := h.captureLink(rawURL, false)
	h.waitPass()
	return h.item(h.owner, item.ID)
}

func (h *harness) item(userID, id string) store.Item {
	h.t.Helper()
	item, err := h.store.Item(context.Background(), userID, id)
	if err != nil {
		h.t.Fatalf("Item: %v", err)
	}
	return item
}

func (h *harness) content(id string) *string {
	h.t.Helper()
	content, err := h.store.ItemContent(context.Background(), h.owner, id)
	if err != nil {
		h.t.Fatalf("ItemContent: %v", err)
	}
	return content
}

func (h *harness) queued(userID string) []store.QueuedItem {
	h.t.Helper()
	items, err := h.store.QueuedItems(context.Background(), userID, "", 100)
	if err != nil {
		h.t.Fatalf("QueuedItems: %v", err)
	}
	return items
}

func mode(item store.Item) string {
	if item.ExtractionMode == nil {
		return ""
	}
	return *item.ExtractionMode
}

func assertExtracted(t *testing.T, item store.Item, wantMode string) {
	t.Helper()
	if item.Status != "extracted" || mode(item) != wantMode || item.ExtractedAt == nil {
		t.Fatalf("item status %q mode %q extracted_at %v, want extracted, %s and a date", item.Status, mode(item), item.ExtractedAt, wantMode)
	}
}

func assertFallback(t *testing.T, h *harness, item store.Item, wantTitle string) {
	t.Helper()
	assertExtracted(t, item, "fallback")
	if item.Title != wantTitle {
		t.Errorf("title = %q, want %q", item.Title, wantTitle)
	}
	if item.ReadingMinutes != nil || item.ContentTruncated {
		t.Errorf("reading %v truncated %v, want none on a fallback", item.ReadingMinutes, item.ContentTruncated)
	}
}

// memImage is the file of an image capture, in memory.
type memImage struct{ *bytes.Reader }

func (memImage) Commit(string) error { return nil }
func (memImage) Keep()               {}
func (memImage) Remove() error       { return nil }

func TestExtractionWorker(t *testing.T) {
	ctx := context.Background()

	// Requirement: Every captured item reaches extracted without blocking.

	t.Run("A captured link becomes extracted", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://blog.example.com/tuning"
		h.web.page(link, "text/html; charset=utf-8", fixture(t, "article.html"))
		h.start()
		captured := h.captureLink(link, false) // Created and queued: the 201 of the API
		h.waitPass()
		item := h.item(h.owner, captured.ID)
		assertExtracted(t, item, "full")
		if !item.UpdatedAt.After(captured.UpdatedAt) && !item.UpdatedAt.Equal(captured.UpdatedAt) {
			t.Errorf("updated_at %v, want at or after the capture's %v", item.UpdatedAt, captured.UpdatedAt)
		}
		if item.ChangeSeq != captured.ChangeSeq+1 {
			t.Errorf("change_seq %d, want %d: one write for the extraction", item.ChangeSeq, captured.ChangeSeq+1)
		}
		if got := h.web.requestsTo(link); got != 1 {
			t.Errorf("page requested %d times, want once", got)
		}
		headers := h.web.headersOf(link)
		if ua := headers.Get("User-Agent"); ua != "Hippo/test (personal read-later)" {
			t.Errorf("User-Agent = %q", ua)
		}
		if headers.Get("Accept") != "text/html,application/xhtml+xml,application/pdf;q=0.9" || headers.Get("Accept-Language") != "fr,en" {
			t.Errorf("headers = %v", headers)
		}
	})

	t.Run("Items left queued are extracted at start-up", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://blog.example.com/left"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		captured := h.captureLink(link, false) // no worker running: the backend is stopped
		if got := h.item(h.owner, captured.ID); got.Status != "queued" {
			t.Fatalf("status before start-up = %q, want queued", got.Status)
		}
		h.start()
		assertExtracted(t, h.item(h.owner, captured.ID), "full")
	})

	t.Run("A stop during a download leaves the item queued", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://slow.example.com/page"
		arrived := make(chan struct{}, 1)
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		var slow atomic.Bool
		slow.Store(true)
		body := fixture(t, "article.html")
		h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			if !slow.Load() {
				rw.Header().Set("Content-Type", "text/html")
				_, _ = rw.Write(body)
				return
			}
			arrived <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-release:
			}
		})
		stop := h.start()
		captured := h.captureLink(link, false)
		select {
		case <-arrived:
		case <-time.After(waitLimit):
			t.Fatal("the page was never requested")
		}
		stop()
		item := h.item(h.owner, captured.ID)
		if item.Status != "queued" || item.ExtractionMode != nil {
			t.Fatalf("after the stop: status %q mode %q, want queued without mode", item.Status, mode(item))
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Fatalf("queued = %+v, want the item without an unfinished attempt", queued)
		}

		slow.Store(false)
		h.start()
		assertExtracted(t, h.item(h.owner, captured.ID), "full")
	})

	t.Run("A NAS network outage leaves items queued", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://news.example.com/a"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		h.web.failDNS.Store(true)
		h.start()
		captured := h.captureLink(link, false)
		h.waitPass()
		if item := h.item(h.owner, captured.ID); item.Status != "queued" || item.ExtractionMode != nil {
			t.Fatalf("during the outage: status %q mode %q, want queued", item.Status, mode(item))
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Fatalf("queued = %+v, want the item without an unfinished attempt", queued)
		}
		if !strings.Contains(h.logs.String(), `"reason":"network"`) {
			t.Errorf("logs do not tell the outage:\n%s", h.logs.String())
		}

		h.web.failDNS.Store(false)
		select {
		case h.pause <- time.Now(): // the outage pause ends
		case <-time.After(waitLimit):
			t.Fatal("the worker did not pause after the outage")
		}
		h.waitPass()
		assertExtracted(t, h.item(h.owner, captured.ID), "full")
	})

	t.Run("A link that keeps failing on the network falls back", func(t *testing.T) {
		h := newHarness(t)
		const dead = "https://unresolved.example.com/a"
		h.web.page(dead, "text/html", fixture(t, "article.html"))
		h.web.failHost("unresolved.example.com")
		for i := 1; i <= 4; i++ {
			h.web.page(fmt.Sprintf("https://blog.example.com/%d", i), "text/html", fixture(t, "article.html"))
		}
		h.start()
		first := h.captureLink("https://blog.example.com/1", false) // the network works
		h.waitPass()
		assertExtracted(t, h.item(h.owner, first.ID), "full")

		// Each pass meets the dead link first, then another link that succeeds: each outage of
		// the dead link follows a success, so each one counts.
		h.relay.mute(true)
		deadItem := h.captureLink(dead, false)
		var others []store.Item
		for pass := 1; pass <= 3; pass++ {
			others = append(others, h.captureLink(fmt.Sprintf("https://blog.example.com/%d", pass+1), false))
			if pass == 1 {
				h.relay.wakeNow()
			} else {
				if item := h.item(h.owner, deadItem.ID); item.Status != "queued" {
					t.Fatalf("before pass %d: status %q, want queued", pass, item.Status)
				}
				h.endPause()
			}
			h.waitPass()
		}
		assertFallback(t, h, h.item(h.owner, deadItem.ID), "unresolved.example.com")
		for _, other := range others {
			assertExtracted(t, h.item(h.owner, other.ID), "full")
		}
		if got := h.web.requestsTo(dead); got != 0 {
			t.Errorf("page requested %d times, want never", got)
		}
		if !strings.Contains(h.logs.String(), `"mode":"fallback","reason":"network"`) {
			t.Errorf("logs do not tell the fallback after the outages:\n%s", h.logs.String())
		}
	})

	t.Run("after a network outage a pass tries two links of other hosts, then pauses", func(t *testing.T) {
		h := newHarness(t)
		const dead, sameHost = "https://down.example.com/a", "https://down.example.com/b"
		h.web.failHost("down.example.com")
		for _, link := range []string{dead, sameHost} {
			h.web.page(link, "text/html", fixture(t, "article.html"))
		}
		var others []string
		for i := 1; i <= 4; i++ {
			others = append(others, fmt.Sprintf("https://site%d.example.com/post", i))
			h.web.page(others[i-1], "text/html", fixture(t, "article.html"))
		}
		h.start()
		h.relay.mute(true)
		deadItem := h.captureLink(dead, false)
		sameItem := h.captureLink(sameHost, false)
		var otherItems []store.Item
		for _, link := range others {
			otherItems = append(otherItems, h.captureLink(link, false))
		}
		textNote := "a note after the outage"
		noteResult, err := h.capture.Capture(ctx, h.owner, capture.Request{Note: &textNote})
		if err != nil {
			t.Fatalf("Capture note: %v", err)
		}
		h.relay.wakeNow()
		h.waitPass()

		for _, id := range []string{deadItem.ID, sameItem.ID, otherItems[2].ID, otherItems[3].ID, noteResult.Item.ID} {
			if item := h.item(h.owner, id); item.Status != "queued" {
				t.Errorf("item %s: status %q, want queued until the next pass", id, item.Status)
			}
		}
		for _, other := range otherItems[:2] {
			assertExtracted(t, h.item(h.owner, other.ID), "full")
		}
		if got := h.web.requestsTo(others[2]) + h.web.requestsTo(others[3]); got != 0 {
			t.Errorf("%d requests to the links after the two tries, want none", got)
		}
		if got := h.web.lookups.Load(); got != 3 {
			t.Errorf("%d host lookups, want 3: the dead link and two links of other hosts, not its other link", got)
		}

		h.endPause() // the next pass goes on with the others
		h.waitPass()
		for _, other := range otherItems[2:] {
			assertExtracted(t, h.item(h.owner, other.ID), "full")
		}
	})

	t.Run("A NAS outage never makes a link fall back", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://news.example.com/outage"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		h.web.page("https://blog.example.com/before", "text/html", fixture(t, "article.html"))
		h.start()
		h.captureLink("https://blog.example.com/before", false) // a success before the outage
		h.waitPass()

		h.web.failDNS.Store(true) // no link can succeed any more
		captured := h.captureLink(link, false)
		h.waitPass()
		for pass := 2; pass <= 5; pass++ {
			h.endPause()
			h.waitPass()
		}
		if item := h.item(h.owner, captured.ID); item.Status != "queued" || item.ExtractionMode != nil {
			t.Fatalf("after 5 passes of outage: status %q mode %q, want queued", item.Status, mode(item))
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].NetworkRetries > 1 {
			t.Errorf("queued = %+v, want at most the one outage that followed the last success counted", queued)
		}
	})

	t.Run("A page that crashed the backend is not fetched again", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://crash.example.com/page"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		captured := h.captureLink(link, false)
		// The previous run started this attempt and died while reading the page.
		if err := h.store.StartExtractionAttempt(ctx, h.owner, captured.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		h.start()
		assertFallback(t, h, h.item(h.owner, captured.ID), "crash.example.com")
		if got := h.web.requestsTo(link); got != 0 {
			t.Errorf("page requested %d times, want never", got)
		}
		if !strings.Contains(h.logs.String(), `"reason":"crashed_before"`) {
			t.Errorf("logs do not tell the earlier crash:\n%s", h.logs.String())
		}
	})

	t.Run("Private links are extracted too", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://private.example.com/post"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		h.start()
		captured := h.captureLink(link, true)
		h.waitPass()
		item := h.item(h.owner, captured.ID)
		assertExtracted(t, item, "full")
		if !item.IsPrivate {
			t.Error("the item is no longer private")
		}
	})

	t.Run("Notes and images are extracted at once", func(t *testing.T) {
		h := newHarness(t)
		h.start()
		text := "buy milk"
		noteResult, err := h.capture.Capture(ctx, h.owner, capture.Request{Note: &text})
		if err != nil {
			t.Fatalf("Capture note: %v", err)
		}
		h.waitPass()
		imageResult, err := h.capture.CaptureImage(ctx, h.owner, capture.ImageRequest{
			Image: memImage{bytes.NewReader(imagetest.PNG(8, 8))},
		})
		if err != nil {
			t.Fatalf("CaptureImage: %v", err)
		}
		h.waitPass()
		for _, id := range []string{noteResult.Item.ID, imageResult.Item.ID} {
			item := h.item(h.owner, id)
			assertExtracted(t, item, "none")
			if item.ReadingMinutes != nil || h.content(id) != nil {
				t.Errorf("item %s has a reading time or a content", id)
			}
		}
		if got := h.item(h.owner, noteResult.Item.ID).Title; got != "buy milk" {
			t.Errorf("note title = %q, want its provisional title", got)
		}
		if asked, lookups, conns := len(h.policy.addresses()), h.web.lookups.Load(), h.web.conns.Load(); asked+int(lookups)+int(conns) != 0 {
			t.Errorf("dials %d, lookups %d, connections %d: want no network request", asked, lookups, conns)
		}
	})

	t.Run("each user's items are written for that user", func(t *testing.T) {
		h := newHarness(t)
		const ownerLink, otherLink = "https://blog.example.com/mine", "https://blog.example.com/theirs"
		h.web.page(ownerLink, "text/html", fixture(t, "article.html"))
		h.web.page(otherLink, "text/html", fixture(t, "javascript-only.html"))
		mine := h.captureFor(h.owner, ownerLink, false)
		theirs := h.captureFor(h.other, otherLink, false)
		h.start()
		assertExtracted(t, h.item(h.owner, mine.ID), "full")
		other := h.item(h.other, theirs.ID)
		assertExtracted(t, other, "fallback")
		if other.Title != "App" {
			t.Errorf("other user's title = %q, want App", other.Title)
		}
		if _, err := h.store.Item(ctx, h.other, mine.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the owner's item is visible to the other user: %v", err)
		}
	})

	// Requirement: A web page is read with its main content.

	t.Run("Article page", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://blog.example.com/tuning-sqlite"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if item.Title != "Tuning SQLite" || item.SiteName == nil || *item.SiteName != "Example Blog" {
			t.Errorf("title %q site %v, want Tuning SQLite and Example Blog", item.Title, item.SiteName)
		}
		if item.ReadingMinutes == nil || *item.ReadingMinutes != 4 {
			t.Errorf("reading minutes = %v, want 4", item.ReadingMinutes)
		}
		content := *h.content(item.ID)
		if !strings.HasPrefix(content, "SQLite is a fine engine") || len(strings.Fields(content)) != 920 {
			t.Errorf("content does not hold the 920 words of the article")
		}
		for _, unwanted := range []string{"menu entry", "Great post", "Comment section"} {
			if strings.Contains(content, unwanted) {
				t.Errorf("content holds %q", unwanted)
			}
		}
	})

	t.Run("Page without site name", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://blog.example.com/post"
		h.web.page(link, "text/html", fixture(t, "no-site-name.html"))
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if item.SiteName == nil || *item.SiteName != "blog.example.com" {
			t.Errorf("site_name = %v, want blog.example.com", item.SiteName)
		}
	})

	t.Run("Page in another charset", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://cuisine.example.com/creme"
		h.web.page(link, "text/html", fixture(t, "latin1.html"))
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if item.Title != "Crème brûlée" {
			t.Errorf("title = %q, want Crème brûlée", item.Title)
		}
	})

	t.Run("a compressed page is read", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://gzip.example.com/post"
		body := fixture(t, "article.html")
		h.web.handle(link, func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "text/html")
			rw.Header().Set("Content-Encoding", "gzip")
			gz := gzip.NewWriter(rw)
			_, _ = gz.Write(body)
			_ = gz.Close()
		})
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if item.Title != "Tuning SQLite" {
			t.Errorf("title = %q", item.Title)
		}
	})

	t.Run("a redirect sends no Referer", func(t *testing.T) {
		h := newHarness(t)
		const link, landing = "https://blog.example.com/moved", "https://blog.example.com/landed"
		h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			http.Redirect(rw, r, landing, http.StatusFound)
		})
		h.web.page(landing, "text/html", fixture(t, "article.html"))
		assertExtracted(t, h.extractLink(link), "full")
		if h.web.requestsTo(landing) != 1 {
			t.Fatal("the redirect was not followed")
		}
		if got := h.web.headersOf(landing).Get("Referer"); got != "" {
			t.Errorf("Referer = %q, want none (RG-92)", got)
		}
	})

	t.Run("the user info of a URL is never sent", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://blog.example.com/auth"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		item := h.extractLink("https://someone:secret@blog.example.com/auth")
		assertExtracted(t, item, "full")
		if got := h.web.headersOf(link).Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none", got)
		}
	})

	// Requirement: Long content is truncated and marked.

	t.Run("Very long article", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://long.example.com/article"
		body, _ := pagestest.LongArticle(200_000)
		h.web.page(link, "text/html", body)
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if !item.ContentTruncated {
			t.Error("content_truncated = false, want true")
		}
		content := *h.content(item.ID)
		fields := strings.Fields(content)
		if n := len([]rune(content)); n > extraction.MaxContentLength || !pagestest.IsWord(fields[len(fields)-1]) {
			t.Errorf("content of %d code points ending with %q, want at most %d and a whole word", n, fields[len(fields)-1], extraction.MaxContentLength)
		}
	})

	t.Run("Normal article", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://blog.example.com/normal"
		h.web.page(link, "text/html", fixture(t, "normal-article.html"))
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if item.ContentTruncated {
			t.Error("content_truncated = true, want false")
		}
		content := *h.content(item.ID)
		if n := len(content); n < 5000 || len(strings.Fields(content)) != 887 {
			t.Errorf("content of %d bytes and %d words, want the whole article of 887 words", n, len(strings.Fields(content)))
		}
	})

	t.Run("Very long description in a fallback", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://summary.example.com/long"
		description, _ := pagestest.Words(2_000_000) // a description of 2 MB, read in the child
		h.web.page(link, "text/html", pagestest.MetaOnly("A page with a long summary", description))
		item := h.extractLink(link)
		assertExtracted(t, item, "fallback")
		if !item.ContentTruncated || item.ReadingMinutes != nil {
			t.Errorf("truncated %v reading %v, want truncated and no reading time", item.ContentTruncated, item.ReadingMinutes)
		}
		content := h.content(item.ID)
		if content == nil {
			t.Fatal("no content, want the description cut")
		}
		fields := strings.Fields(*content)
		if n := len([]rune(*content)); n > extraction.MaxContentLength || !pagestest.IsWord(fields[len(fields)-1]) {
			t.Errorf("content of %d code points, want at most %d ending on a whole word", n, extraction.MaxContentLength)
		}
	})

	// Requirement: A failed extraction falls back.

	t.Run("Page that does not answer in time", func(t *testing.T) {
		if extraction.DownloadTimeout != 15*time.Second {
			t.Fatalf("DownloadTimeout = %v, want 15 s (RG-23)", extraction.DownloadTimeout)
		}
		h := newHarness(t)
		h.timeout = 300 * time.Millisecond // the 15 s of production, shortened
		const link = "https://late.example.com/page"
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			// The headers come at once; the body never ends: the deadline covers the body too.
			rw.Header().Set("Content-Type", "text/html")
			_, _ = rw.Write([]byte("<html><head><title>Late</title></head><body>"))
			rw.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-release:
			}
		})
		begin := time.Now()
		item := h.extractLink(link)
		assertFallback(t, h, item, "late.example.com")
		if elapsed := time.Since(begin); elapsed > 10*time.Second {
			t.Errorf("fallback after %v, want about the timeout", elapsed)
		}
		if !strings.Contains(h.logs.String(), `"reason":"timeout"`) {
			t.Errorf("logs do not tell the timeout:\n%s", h.logs.String())
		}
	})

	t.Run("Error status ignores the page metadata", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://example.com/gone"
		h.web.handle(link, func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "text/html")
			rw.WriteHeader(http.StatusNotFound)
			_, _ = rw.Write(fixture(t, "not-found.html"))
		})
		item := h.extractLink(link)
		assertFallback(t, h, item, "example.com")
		if h.content(item.ID) != nil {
			t.Error("content kept from an error page")
		}
		if !strings.Contains(h.logs.String(), `"http_status":404`) {
			t.Errorf("logs do not tell the status:\n%s", h.logs.String())
		}
	})

	t.Run("Page whose content needs JavaScript", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://app.example.com/"
		h.web.page(link, "text/html", fixture(t, "javascript-only.html"))
		assertFallback(t, h, h.extractLink(link), "App")
	})

	t.Run("Fallback keeps the description as content", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://summary.example.com/page"
		h.web.page(link, "text/html", fixture(t, "og-only.html"))
		item := h.extractLink(link)
		assertFallback(t, h, item, "summary.example.com")
		if content := h.content(item.ID); content == nil || *content != "A short summary" {
			t.Errorf("content = %v, want A short summary", content)
		}
	})

	t.Run("Page too heavy to read", func(t *testing.T) {
		h := newHarness(t)
		const heavy, next = "https://heavy.example.com/page", "https://blog.example.com/after"
		body := pagestest.SelectEnds(17_000) // about 200 KB, hundreds of thousands of elements once parsed
		if len(body) < 150_000 || len(body) > 250_000 {
			t.Fatalf("page of %d bytes, want about 200 KB", len(body))
		}
		h.web.page(heavy, "text/html", body)
		h.web.page(next, "text/html", fixture(t, "article.html"))
		item := h.extractLink(heavy)
		assertFallback(t, h, item, "heavy.example.com")
		if h.content(item.ID) != nil {
			t.Error("content kept from a page too heavy to read")
		}
		if !strings.Contains(h.logs.String(), `"reason":"too_large"`) {
			t.Errorf("logs do not tell the size:\n%s", h.logs.String())
		}
		// The backend keeps answering: the same worker extracts the next link.
		after := h.captureLink(next, false)
		h.waitPass()
		assertExtracted(t, h.item(h.owner, after.ID), "full")
	})

	t.Run("a reading over its time limit falls back", func(t *testing.T) {
		h := newHarness(t)
		fifo, _ := startedFIFO(t)
		h.reader = childArgv(hangChild, fifo)
		h.readFor = 300 * time.Millisecond
		const link = "https://slowread.example.com/page"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		item := h.extractLink(link)
		assertFallback(t, h, item, "slowread.example.com")
		if !strings.Contains(h.logs.String(), `"reason":"timeout"`) {
			t.Errorf("logs do not tell the time limit:\n%s", h.logs.String())
		}
	})

	t.Run("a page reader that cannot start leaves the item queued and pauses", func(t *testing.T) {
		h := newHarness(t)
		h.reader = []string{filepath.Join(t.TempDir(), "missing-reader"), extraction.ChildCommand}
		const first, second = "https://blog.example.com/first", "https://blog.example.com/second"
		for _, link := range []string{first, second} {
			h.web.page(link, "text/html", fixture(t, "article.html"))
		}
		stop := h.start()
		h.relay.mute(true)
		firstItem := h.captureLink(first, false)
		secondItem := h.captureLink(second, false)
		h.relay.wakeNow()
		h.waitPass()
		for _, id := range []string{firstItem.ID, secondItem.ID} {
			if item := h.item(h.owner, id); item.Status != "queued" {
				t.Errorf("item %s: status %q, want queued", id, item.Status)
			}
		}
		if queued := h.queued(h.owner); len(queued) != 2 || queued[0].AttemptStarted {
			t.Errorf("queued = %+v, want both, without an unfinished attempt", queued)
		}
		if got := h.web.requestsTo(second); got != 0 {
			t.Errorf("second link requested %d times, want the pass stopped at the first", got)
		}
		if !strings.Contains(h.logs.String(), `"reason":"reader_unavailable"`) {
			t.Errorf("logs do not tell the missing reader:\n%s", h.logs.String())
		}
		h.endPause() // the worker pauses as for an outage, instead of looping
		h.waitPass()
		stop()

		h.reader = childArgv(extraction.ChildCommand)
		h.start()
		assertExtracted(t, h.item(h.owner, firstItem.ID), "full")
		assertExtracted(t, h.item(h.owner, secondItem.ID), "full")
	})

	t.Run("a stop during the reading leaves the item queued", func(t *testing.T) {
		h := newHarness(t)
		fifo, started := startedFIFO(t)
		h.reader = childArgv(hangChild, fifo)
		const link = "https://reading.example.com/page"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		stop := h.start()
		captured := h.captureLink(link, false)
		select {
		case <-started:
		case <-time.After(waitLimit):
			t.Fatal("the page was never handed to a reader")
		}
		stop()
		item := h.item(h.owner, captured.ID)
		if item.Status != "queued" || item.ExtractionMode != nil {
			t.Fatalf("after the stop: status %q mode %q, want queued", item.Status, mode(item))
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Fatalf("queued = %+v, want the item without an unfinished attempt", queued)
		}
		h.reader = childArgv(extraction.ChildCommand)
		h.start()
		assertExtracted(t, h.item(h.owner, captured.ID), "full")
	})

	t.Run("Oversized page", func(t *testing.T) {
		chunk := bytes.Repeat([]byte("<p>filler text of an oversized fictional page</p>\n"), 1300) // about 64 KB
		const serverCap = 64 << 20
		for _, compressed := range []bool{false, true} {
			h := newHarness(t)
			link := fmt.Sprintf("https://big.example.com/page-%v", compressed)
			var written atomic.Int64
			var stopped atomic.Bool
			h.web.handle(link, func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Content-Type", "text/html")
				var out io.Writer = rw
				if compressed {
					rw.Header().Set("Content-Encoding", "gzip")
					gz := gzip.NewWriter(rw)
					defer func() { _ = gz.Close() }()
					out = gz
				}
				_, _ = out.Write([]byte("<html><head><title>Big</title></head><body>"))
				for written.Load() < serverCap {
					if _, err := out.Write(chunk); err != nil {
						stopped.Store(true)
						return
					}
					written.Add(int64(len(chunk)))
				}
			})
			item := h.extractLink(link)
			assertFallback(t, h, item, "big.example.com")
			if !strings.Contains(h.logs.String(), `"reason":"too_large"`) {
				t.Errorf("compressed %v: logs do not tell the size:\n%s", compressed, h.logs.String())
			}
			if written.Load() >= serverCap {
				t.Errorf("compressed %v: the whole body of %d bytes was read, want the reading stopped at the limit", compressed, written.Load())
			}
		}
	})

	// Requirement: Local and private addresses are never fetched.

	t.Run("Link to the home network", func(t *testing.T) {
		h := newHarness(t)
		h.policy.policy = extraction.PublicAddress
		item := h.extractLink("http://192.168.1.1/admin")
		assertFallback(t, h, item, "192.168.1.1")
		want := netip.MustParseAddrPort("192.168.1.1:80")
		if asked := h.policy.addresses(); len(asked) != 1 || asked[0] != want {
			t.Errorf("policy asked about %v, want only %v, refused before connecting", asked, want)
		}
		if !strings.Contains(h.logs.String(), `"reason":"blocked"`) {
			t.Errorf("logs do not tell the refusal:\n%s", h.logs.String())
		}
	})

	t.Run("Host resolving to a local address", func(t *testing.T) {
		h := newHarness(t)
		h.policy.policy = extraction.PublicAddress // the test web is on 127.0.0.1
		const link = "https://local.example.com/page"
		h.web.page(link, "text/html", fixture(t, "article.html"))
		item := h.extractLink(link)
		assertFallback(t, h, item, "local.example.com")
		if conns := h.web.conns.Load(); conns != 0 {
			t.Errorf("%d connections opened, want none", conns)
		}
	})

	t.Run("loopback and localhost are refused by the production policy", func(t *testing.T) {
		var conns atomic.Int64
		server := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = rw.Write(fixture(t, "article.html"))
		}))
		server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				conns.Add(1)
			}
		}
		server.Start()
		t.Cleanup(server.Close)
		port := server.Listener.Addr().(*net.TCPAddr).Port
		for _, link := range []string{
			fmt.Sprintf("http://127.0.0.1:%d/page", port),
			fmt.Sprintf("http://localhost:%d/page", port),
		} {
			h := newHarness(t)
			h.policy.policy = extraction.PublicAddress
			h.resolver = nil // the system resolver: localhost is answered locally
			item := h.extractLink(link)
			assertExtracted(t, item, "fallback")
		}
		if got := conns.Load(); got != 0 {
			t.Errorf("%d connections reached the local server, want none", got)
		}
	})

	t.Run("Redirect to a private address", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://public.example.com/moved"
		h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			http.Redirect(rw, r, "http://10.0.0.5/", http.StatusFound)
		})
		item := h.extractLink(link)
		assertFallback(t, h, item, "public.example.com")
		private := netip.MustParseAddrPort("10.0.0.5:80")
		if asked := h.policy.addresses(); len(asked) != 2 || asked[0] != h.web.addr || asked[1] != private {
			t.Errorf("policy asked about %v, want the public page then %v, refused before connecting", asked, private)
		}
		if !strings.Contains(h.logs.String(), `"reason":"blocked"`) {
			t.Errorf("logs do not tell the refusal:\n%s", h.logs.String())
		}
	})

	t.Run("a redirect to a refused server sends it nothing", func(t *testing.T) {
		var conns atomic.Int64
		refused := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = rw.Write(fixture(t, "article.html"))
		}))
		refused.Config.ConnState = func(_ net.Conn, state http.ConnState) {
			if state == http.StateNew {
				conns.Add(1)
			}
		}
		refused.Start()
		t.Cleanup(refused.Close)
		h := newHarness(t)
		allowed := h.web.addr
		h.policy.policy = func(addr netip.AddrPort) bool { return addr == allowed } // only the web is allowed
		const link = "https://public.example.com/to-local"
		h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			http.Redirect(rw, r, refused.URL+"/page", http.StatusFound)
		})
		item := h.extractLink(link)
		assertFallback(t, h, item, "public.example.com")
		if got := conns.Load(); got != 0 {
			t.Errorf("%d connections reached the refused server, want none", got)
		}
	})

	// Requirement: Extraction keeps content out of the logs.

	t.Run("Extraction logs no content", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://secret.example.com/a"
		h.web.page(link, "text/html", pagestest.Article("Private thought", strings.Repeat("a private sentence ", 40)))
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if item.Title != "Private thought" {
			t.Fatalf("title = %q, want the page title", item.Title)
		}
		logs := h.logs.String()
		for _, secret := range []string{"secret.example.com", "Private thought", "private sentence"} {
			if strings.Contains(logs, secret) {
				t.Errorf("logs contain %q", secret)
			}
		}
		if !strings.Contains(logs, item.ID) || !strings.Contains(logs, `"mode":"full"`) || !strings.Contains(logs, `"duration_ms"`) {
			t.Errorf("logs lack the item id, mode or duration:\n%s", logs)
		}
	})
}

// Generated PDFs that take seconds to build are built once.
var (
	enormousPagePDF = sync.OnceValue(func() []byte {
		return pdftest.Doc{Compress: true, Pages: []pdftest.Page{pdftest.EnormousPage(100 << 20)}}.Bytes()
	})
	hugeStringPDF = sync.OnceValue(func() []byte {
		return pdftest.Doc{Compress: true, Pages: []pdftest.Page{pdftest.HugeString(100 << 20)}}.Bytes()
	})
)

// tuningPDF is the fictional article of the scenarios: 10 pages, 1 951 words, titled Tuning
// SQLite in its metadata.
func tuningPDF() ([]byte, []string) {
	doc, sentences := pdftest.Article("Tuning SQLite", 10, 1951)
	return doc.Bytes(), sentences
}

// textPDF is a document whose first page starts with lines, followed by enough text to be read
// in full.
func textPDF(doc pdftest.Doc, lines ...string) []byte {
	doc.Pages = append([]pdftest.Page{withFiller(lines...)}, doc.Pages...)
	return doc.Bytes()
}

// extractPDF serves pdf at rawURL as application/pdf and extracts it with a running worker.
func (h *harness) extractPDF(rawURL string, pdf []byte) store.Item {
	h.t.Helper()
	h.web.page(rawURL, "application/pdf", pdf)
	return h.extractLink(rawURL)
}

func (h *harness) contentOf(item store.Item) string {
	h.t.Helper()
	content := h.content(item.ID)
	if content == nil {
		h.t.Fatalf("item %s has no content", item.ID)
	}
	return *content
}

// assertPDFFallback checks a PDF that fell back: its title, the host as site name, nothing else.
func assertPDFFallback(t *testing.T, h *harness, item store.Item, wantTitle, wantSite string) {
	t.Helper()
	assertFallback(t, h, item, wantTitle)
	if item.SiteName == nil || *item.SiteName != wantSite {
		t.Errorf("site_name = %v, want %s", item.SiteName, wantSite)
	}
	if h.content(item.ID) != nil {
		t.Error("a PDF that falls back keeps a content")
	}
}

// assertNoTempFile checks that the temporary directory is empty and that the test process, the
// service, holds no descriptor on a file without name.
func assertNoTempFile(t *testing.T, h *harness) {
	t.Helper()
	if names := entries(t, h.tmpPath); len(names) != 0 {
		t.Errorf("temporary directory holds %v", names)
	}
	if n := unlinkedFiles(t); n != 0 {
		t.Errorf("%d descriptors open on a file without name", n)
	}
}

// unlinkedFiles counts the descriptors of the process open on a regular file without name.
func unlinkedFiles(t *testing.T) int {
	t.Helper()
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list descriptors: %v", err)
	}
	n := 0
	for _, e := range list {
		fd, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		var st syscall.Stat_t
		if syscall.Fstat(fd, &st) == nil && st.Mode&syscall.S_IFMT == syscall.S_IFREG && st.Nlink == 0 {
			n++
		}
	}
	return n
}

// insertDeferredPDF writes, through its own connection, the attempt row E2-05 left for a link
// answered as a PDF: no function of the store writes it any more.
func insertDeferredPDF(t *testing.T, dbPath, userID, itemID string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO extraction_attempts (item_id, user_id, outcome) VALUES (?, ?, 'deferred_pdf')`, itemID, userID); err != nil {
		t.Fatalf("insert deferred attempt: %v", err)
	}
}

func TestPDFExtractionWorker(t *testing.T) {
	// Requirement: Every captured item reaches extracted without blocking.

	t.Run("No room on the NAS for a PDF leaves the item queued", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://docs.example.com/papers/nas.pdf"
		pdf, _ := tuningPDF()
		h.web.page(link, "application/pdf", pdf)
		readOnly(t, h.tmpPath)
		h.start()
		captured := h.captureLink(link, false)
		h.waitPass()
		if item := h.item(h.owner, captured.ID); item.Status != "queued" || item.ExtractionMode != nil {
			t.Fatalf("status %q mode %q, want queued without fallback", item.Status, mode(item))
		}
		if queued := h.queued(h.owner); len(queued) != 1 || queued[0].AttemptStarted {
			t.Fatalf("queued = %+v, want the item without an unfinished attempt", queued)
		}
		if logs := h.logs.String(); !strings.Contains(logs, `"level":"ERROR"`) || !strings.Contains(logs, `"reason":"storage_unavailable"`) {
			t.Errorf("logs do not tell the storage failure as an error:\n%s", logs)
		}
		if err := os.Chmod(h.tmpPath, 0o700); err != nil { // the NAS can write again
			t.Fatal(err)
		}
		h.endPause()
		h.waitPass()
		assertExtracted(t, h.item(h.owner, captured.ID), "full")
	})

	t.Run("a NAS without room for a PDF does not hold back the items behind it", func(t *testing.T) {
		h := newHarness(t)
		const pdfLink, pageLink = "https://docs.example.com/papers/full-disk.pdf", "https://blog.example.com/after-the-pdf"
		pdf, _ := tuningPDF()
		h.web.page(pdfLink, "application/pdf", pdf)
		h.web.page(pageLink, "text/html", fixture(t, "article.html"))
		var room atomic.Bool
		extraction.SetFreeSpace(h.tmp, func() (uint64, bool, error) {
			if room.Load() {
				return 1 << 40, true, nil
			}
			return 0, true, nil
		})
		h.start()
		h.relay.mute(true)
		pdfItem := h.captureLink(pdfLink, false)
		text := "a note captured after the PDF"
		noteResult, err := h.capture.Capture(context.Background(), h.owner, capture.Request{Note: &text})
		if err != nil {
			t.Fatalf("Capture note: %v", err)
		}
		pageItem := h.captureLink(pageLink, false)
		h.relay.wakeNow()
		h.waitPass()
		if item := h.item(h.owner, pdfItem.ID); item.Status != "queued" {
			t.Errorf("PDF: status %q, want queued", item.Status)
		}
		assertExtracted(t, h.item(h.owner, noteResult.Item.ID), "none")
		assertExtracted(t, h.item(h.owner, pageItem.ID), "full")
		room.Store(true)
		h.endPause() // the worker pauses after the pass, as for an outage
		h.waitPass()
		assertExtracted(t, h.item(h.owner, pdfItem.ID), "full")
	})

	// Requirement: Extraction keeps content out of the logs.

	t.Run("PDF extraction logs no content", func(t *testing.T) {
		h := newHarness(t)
		const titled, broken = "https://secret.example.com/private-report.pdf", "https://secret.example.com/broken-plan.pdf"
		h.web.page(titled, "application/pdf", textPDF(pdftest.Doc{Title: []byte("Private thought")}, "A private sentence"))
		h.web.page(broken, "application/pdf", pdftest.Truncated(textPDF(pdftest.Doc{}, "Another private sentence")))
		h.start()
		first := h.captureLink(titled, false)
		h.waitPass()
		second := h.captureLink(broken, false)
		h.waitPass()
		assertExtracted(t, h.item(h.owner, first.ID), "full")
		assertExtracted(t, h.item(h.owner, second.ID), "fallback")
		logs := h.logs.String()
		for _, secret := range []string{"secret.example.com", "private-report", "broken-plan", "Private thought", "private sentence", h.tmpPath} {
			if strings.Contains(logs, secret) {
				t.Errorf("logs contain %q", secret)
			}
		}
		if !strings.Contains(logs, `"format":"pdf"`) || !strings.Contains(logs, `"reason":"damaged"`) {
			t.Errorf("logs lack the format or the reason:\n%s", logs)
		}
	})

	// Requirement: A PDF is read with its text.

	t.Run("PDF article", func(t *testing.T) {
		h := newHarness(t)
		pdf, sentences := tuningPDF()
		item := h.extractPDF("https://docs.example.com/papers/tuning.pdf", pdf)
		assertExtracted(t, item, "full")
		if item.Title != "Tuning SQLite" || item.SiteName == nil || *item.SiteName != "docs.example.com" {
			t.Errorf("title %q site %v, want Tuning SQLite and docs.example.com", item.Title, item.SiteName)
		}
		if item.ReadingMinutes == nil || *item.ReadingMinutes != 9 || item.ContentTruncated {
			t.Errorf("reading %v truncated %v, want 9 and false", item.ReadingMinutes, item.ContentTruncated)
		}
		flat := strings.Join(strings.Fields(h.contentOf(item)), " ")
		if !strings.HasPrefix(flat, sentences[0]) || !strings.HasSuffix(flat, sentences[len(sentences)-1]) {
			t.Error("content does not hold the first and the last sentence")
		}
	})

	t.Run("PDF served under another type", func(t *testing.T) {
		pdf, _ := tuningPDF()
		for _, contentType := range []string{"application/octet-stream", "text/html"} {
			h := newHarness(t)
			const link = "https://docs.example.com/papers/typed"
			h.web.page(link, contentType, pdf)
			item := h.extractLink(link)
			assertExtracted(t, item, "full")
			if item.Title != "Tuning SQLite" {
				t.Errorf("%s: title %q, want Tuning SQLite", contentType, item.Title)
			}
		}
	})

	t.Run("Words placed without spaces", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://docs.example.com/glyphs.pdf",
			textPDF(pdftest.Doc{Font: pdftest.TwoByte, Placement: pdftest.Glyphs}, "Le moteur lit chaque page"))
		assertExtracted(t, item, "full")
		if !strings.Contains(h.contentOf(item), "Le moteur lit chaque page") {
			t.Errorf("content = %q", h.contentOf(item)[:60])
		}
	})

	t.Run("Text in a two-byte font", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://docs.example.com/cuisine.pdf",
			textPDF(pdftest.Doc{Font: pdftest.TwoByte, Placement: pdftest.Glyphs}, "Crème brûlée à l'œuf"))
		assertExtracted(t, item, "full")
		if !strings.Contains(h.contentOf(item), "Crème brûlée à l'œuf") {
			t.Errorf("content = %q", h.contentOf(item)[:60])
		}
	})

	t.Run("Ligatures and hyphenated line ends", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://docs.example.com/ligatures.pdf",
			textPDF(pdftest.Doc{Font: pdftest.TwoByte}, "Une \ufb01ne réfle-", "xion sur le porte\u00ad", "monnaie"))
		assertExtracted(t, item, "full")
		if !strings.Contains(h.contentOf(item), "Une fine réfle-xion sur le portemonnaie") {
			t.Errorf("content = %q", h.contentOf(item)[:60])
		}
	})

	t.Run("Encrypted PDF that opens without a password", func(t *testing.T) {
		h := newHarness(t)
		article, _ := pdftest.Article("Copie fictive", 2, 300)
		article.Encryption = pdftest.AES128
		item := h.extractPDF("https://docs.example.com/copie.pdf", article.Bytes())
		assertExtracted(t, item, "full")
		if item.Title != "Copie fictive" {
			t.Errorf("title = %q, want Copie fictive without trailing control character", item.Title)
		}
		if got := len(strings.Fields(h.contentOf(item))); got != 300 {
			t.Errorf("content holds %d words, want the 300 of the document", got)
		}
	})

	t.Run("PDF of version 2.0", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://docs.example.com/v2.pdf", textPDF(pdftest.Doc{Version: "2.0"}, "Un document de version deux"))
		assertExtracted(t, item, "full")
		if !strings.HasPrefix(h.contentOf(item), "Un document de version deux") {
			t.Errorf("content = %q", h.contentOf(item)[:60])
		}
	})

	t.Run("A page that cannot be read is skipped", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://docs.example.com/skipped.pdf", pdftest.Doc{Pages: []pdftest.Page{
			withFiller("Texte de la page un."), {Broken: true}, withFiller("Texte de la page trois."),
		}}.Bytes())
		assertExtracted(t, item, "full")
		content := h.contentOf(item)
		if !strings.HasPrefix(content, "Texte de la page un.") || !strings.Contains(content, "Texte de la page trois.") {
			t.Errorf("content = %q", content)
		}
	})

	t.Run("Private PDF links are read too", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://private.example.com/notes.pdf"
		pdf, _ := tuningPDF()
		h.web.page(link, "application/pdf", pdf)
		h.start()
		captured := h.captureLink(link, true)
		h.waitPass()
		item := h.item(h.owner, captured.ID)
		assertExtracted(t, item, "full")
		if !item.IsPrivate {
			t.Error("the item is no longer private")
		}
	})

	// Requirement: The title of a PDF follows its metadata, then its file name.

	t.Run("Junk metadata title", func(t *testing.T) {
		for _, junk := range [][]byte{[]byte("Microsoft Word - rapport.docx"), []byte("page.html"), []byte("Untitled"), []byte("\t\t\t")} {
			h := newHarness(t)
			item := h.extractPDF("https://example.com/docs/rapport-annuel-2025.pdf", textPDF(pdftest.Doc{Title: junk}, "Rapport fictif"))
			assertExtracted(t, item, "full")
			if item.Title != "rapport-annuel-2025" {
				t.Errorf("metadata title %q: title %q, want rapport-annuel-2025", junk, item.Title)
			}
		}
	})

	t.Run("Metadata title kept when it only looks like a file name", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://example.com/docs/node.pdf", textPDF(pdftest.Doc{Title: []byte("Node.js en production")}, "Un guide fictif"))
		if item.Title != "Node.js en production" {
			t.Errorf("title = %q, want Node.js en production", item.Title)
		}
	})

	t.Run("File name of the URL is decoded", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://example.com/files/Rapport%20d%27activit%C3%A9.PDF" //nolint:misspell // percent-encoded "activité"
		item := h.extractPDF(link, textPDF(pdftest.Doc{}, "Un bilan fictif"))
		assertExtracted(t, item, "full")
		if item.Title != "Rapport d'activité" {
			t.Errorf("title = %q, want Rapport d'activité", item.Title)
		}
	})

	t.Run("File name after a redirect", func(t *testing.T) {
		h := newHarness(t)
		const link, landing = "https://example.com/download?id=7", "https://cdn.example.com/files/guide-installation.pdf"
		h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, landing, http.StatusFound) })
		h.web.page(landing, "application/pdf", textPDF(pdftest.Doc{}, "Un guide fictif"))
		item := h.extractLink(link)
		assertExtracted(t, item, "full")
		if item.Title != "guide-installation" || item.SiteName == nil || *item.SiteName != "example.com" {
			t.Errorf("title %q site %v, want guide-installation and example.com", item.Title, item.SiteName)
		}
	})

	t.Run("PDF without title nor file name", func(t *testing.T) {
		h := newHarness(t)
		h.web.page("https://example.com/download", "application/pdf", textPDF(pdftest.Doc{}, "Un document fictif"))
		item := h.extractLink("https://example.com/download?id=7")
		assertExtracted(t, item, "full")
		if item.Title != "example.com" {
			t.Errorf("title = %q, want example.com", item.Title)
		}
	})

	t.Run("Broken metadata does not lose the text", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://example.com/docs/notice.pdf", textPDF(pdftest.Doc{Title: []byte("Notice"), BrokenInfo: true}, "Une notice fictive"))
		assertExtracted(t, item, "full")
		if item.Title != "notice" || !strings.HasPrefix(h.contentOf(item), "Une notice fictive") {
			t.Errorf("title %q, want notice and the document's text", item.Title)
		}
	})

	// Requirement: Long PDF text is cut and its reading time counts the whole document.

	t.Run("Very long PDF", func(t *testing.T) {
		h := newHarness(t)
		doc, _ := pdftest.Article("Long", 400, 78_000)
		item := h.extractPDF("https://docs.example.com/long.pdf", doc.Bytes())
		assertExtracted(t, item, "full")
		content := h.contentOf(item)
		fields := strings.Fields(content)
		if n := len([]rune(content)); !item.ContentTruncated || n > extraction.MaxContentLength || !pdftest.IsWord(fields[len(fields)-1]) {
			t.Errorf("truncated %v, content of %d code points ending with %q", item.ContentTruncated, n, fields[len(fields)-1])
		}
		if item.ReadingMinutes == nil || *item.ReadingMinutes != 340 {
			t.Errorf("reading minutes = %v, want 340", item.ReadingMinutes)
		}
	})

	t.Run("Page with an enormous text", func(t *testing.T) {
		h := newHarness(t)
		h.readFor = 4 * time.Second // under the race detector, the reading may reach its deadline: it answers then
		const enormous, next = "https://docs.example.com/enormous.pdf", "https://blog.example.com/after"
		h.web.page(next, "text/html", fixture(t, "article.html"))
		item := h.extractPDF(enormous, enormousPagePDF())
		assertExtracted(t, item, "full")
		if n := len([]rune(h.contentOf(item))); !item.ContentTruncated || n > extraction.MaxContentLength || item.ReadingMinutes == nil {
			t.Errorf("truncated %v, content of %d code points, reading %v", item.ContentTruncated, n, item.ReadingMinutes)
		}
		after := h.captureLink(next, false)
		h.waitPass()
		assertExtracted(t, h.item(h.owner, after.ID), "full")
	})

	t.Run("PDF whose reading reaches the time limit", func(t *testing.T) {
		h := newHarness(t)
		h.reader = childArgv(stoppedChild)
		doc, _ := pdftest.Article("Lecture fictive", 400, 80_000)
		item := h.extractPDF("https://docs.example.com/interrupted.pdf", doc.Bytes())
		assertExtracted(t, item, "full")
		content := h.contentOf(item)
		// The pages hold 200 words each: 10 000 words are the first 50 pages, and only them.
		lastRead := doc.Pages[49].Lines
		if !strings.HasSuffix(content, lastRead[len(lastRead)-1]) || len(strings.Fields(content)) != 10_000 {
			t.Errorf("content of %d words ending with %q, want page 50 last", len(strings.Fields(content)), content[len(content)-40:])
		}
		if !item.ContentTruncated || item.ReadingMinutes == nil || *item.ReadingMinutes != 348 {
			t.Errorf("truncated %v reading %v, want true and 348", item.ContentTruncated, item.ReadingMinutes)
		}
	})

	// Requirement: A PDF that cannot be read falls back.

	t.Run("Scanned PDF", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://docs.example.com/scan.pdf",
			pdftest.Doc{Title: []byte("Facture fictive"), Pages: []pdftest.Page{{Image: true}, {Image: true}, {Image: true}}}.Bytes())
		assertPDFFallback(t, h, item, "Facture fictive", "docs.example.com")
	})

	article, _ := pdftest.Article("", 2, 300)

	t.Run("Password-protected PDF", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://example.com/docs/plan-fictif.pdf",
			pdftest.Doc{Title: []byte("Plan"), Encryption: pdftest.AES128, UserPassword: "ouvrir", Pages: article.Pages}.Bytes())
		assertPDFFallback(t, h, item, "plan-fictif", "example.com")
		if !strings.Contains(h.logs.String(), `"reason":"encrypted"`) {
			t.Errorf("logs do not tell the encryption:\n%s", h.logs.String())
		}
	})

	t.Run("PDF encrypted with AES-256", func(t *testing.T) {
		h := newHarness(t)
		item := h.extractPDF("https://example.com/docs/notice-fictive.pdf",
			pdftest.Doc{Encryption: pdftest.AES256, Pages: article.Pages}.Bytes())
		assertPDFFallback(t, h, item, "notice-fictive", "example.com")
	})

	t.Run("Damaged PDF", func(t *testing.T) {
		for name, body := range map[string][]byte{
			"truncated":       pdftest.Truncated(article.Bytes()),
			"wrong startxref": pdftest.WrongStartxref(article.Bytes()),
			"HTML page":       fixture(t, "article.html"),
		} {
			h := newHarness(t)
			item := h.extractPDF("https://example.com/docs/rapport-fictif.pdf", body)
			assertPDFFallback(t, h, item, "rapport-fictif", "example.com")
			if !strings.Contains(h.logs.String(), `"reason":"damaged"`) {
				t.Errorf("%s: logs do not tell the damage:\n%s", name, h.logs.String())
			}
		}
	})

	t.Run("Oversized PDF", func(t *testing.T) {
		for _, variant := range []string{"exact", "chunked", "gzip", "announced"} {
			h := newHarness(t)
			link := "https://example.com/docs/big-" + variant + ".pdf"
			var served atomic.Int64
			h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
				rw.Header().Set("Content-Type", "application/pdf")
				var out io.Writer = rw
				switch variant {
				case "exact":
					rw.Header().Set("Content-Length", strconv.Itoa(extraction.MaxPDFSize+1))
				case "gzip":
					rw.Header().Set("Content-Encoding", "gzip")
					gz := gzip.NewWriter(rw)
					defer func() { _ = gz.Close() }()
					out = gz
				case "announced":
					rw.Header().Set("Content-Length", strconv.Itoa(60<<20))
					rw.WriteHeader(http.StatusOK)
					rw.(http.Flusher).Flush()
					<-r.Context().Done() // reading the body would wait for the time limit
					return
				}
				_ = pdftest.Filler(writerFunc(func(p []byte) (int, error) {
					n, err := out.Write(p)
					served.Add(int64(n))
					return n, err
				}), extraction.MaxPDFSize+1)
			})
			item := h.extractLink(link)
			assertPDFFallback(t, h, item, "big-"+variant, "example.com")
			if logs := h.logs.String(); !strings.Contains(logs, `"reason":"too_large"`) {
				t.Errorf("%s: logs do not tell the size:\n%s", variant, logs)
			}
			if served.Load() > extraction.MaxPDFSize+1 {
				t.Errorf("%s: %d bytes served, want at most the limit and one byte", variant, served.Load())
			}
		}
	})

	t.Run("PDF that does not download in time", func(t *testing.T) {
		pdf, _ := tuningPDF()
		// The body stalls before any byte, before the 512 bytes that tell a PDF, and after them.
		for _, sent := range []int{0, 100, 2000} {
			h := newHarness(t)
			h.timeout = 300 * time.Millisecond // the 15 s of production, shortened
			const link = "https://example.com/docs/lent.pdf"
			h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
				rw.Header().Set("Content-Type", "application/pdf")
				rw.WriteHeader(http.StatusOK)
				_, _ = rw.Write(pdf[:sent])
				rw.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			begin := time.Now()
			item := h.extractLink(link)
			assertPDFFallback(t, h, item, "lent", "example.com")
			if elapsed := time.Since(begin); elapsed > 10*time.Second {
				t.Errorf("%d bytes sent: fallback after %v, want about the timeout", sent, elapsed)
			}
			if !strings.Contains(h.logs.String(), `"reason":"timeout","format":"pdf"`) {
				t.Errorf("%d bytes sent: logs do not tell a PDF timeout:\n%s", sent, h.logs.String())
			}
		}
	})

	t.Run("PDF too heavy to read", func(t *testing.T) {
		h := newHarness(t)
		const heavy, next = "https://example.com/docs/lourd.pdf", "https://blog.example.com/after"
		h.web.page(next, "text/html", fixture(t, "article.html"))
		item := h.extractPDF(heavy, hugeStringPDF())
		assertPDFFallback(t, h, item, "lourd", "example.com")
		if !strings.Contains(h.logs.String(), `"reason":"too_large"`) {
			t.Errorf("logs do not tell the memory limit:\n%s", h.logs.String())
		}
		after := h.captureLink(next, false)
		h.waitPass()
		assertExtracted(t, h.item(h.owner, after.ID), "full")
	})

	t.Run("PDF whose structure loops", func(t *testing.T) {
		for name, doc := range map[string]pdftest.Doc{
			"page tree":            {TreeCycle: true, Pages: article.Pages},
			"cross-reference loop": {PrevLoop: true, Pages: article.Pages},
		} {
			h := newHarness(t)
			h.readFor = 2 * time.Second
			const looping, next = "https://example.com/docs/boucle.pdf", "https://blog.example.com/after"
			h.web.page(next, "text/html", fixture(t, "article.html"))
			begin := time.Now()
			item := h.extractPDF(looping, doc.Bytes())
			assertPDFFallback(t, h, item, "boucle", "example.com")
			if elapsed := time.Since(begin); elapsed > 2*time.Second+time.Second {
				t.Errorf("%s: fallback after %v, want within the reading time limit", name, elapsed)
			}
			after := h.captureLink(next, false)
			h.waitPass()
			assertExtracted(t, h.item(h.owner, after.ID), "full")
		}
	})

	// Requirement: A PDF leaves no copy on the NAS.

	t.Run("No file is left after a PDF", func(t *testing.T) {
		h := newHarness(t)
		pdf, _ := tuningPDF()
		const read, over, cut = "https://docs.example.com/read.pdf", "https://docs.example.com/over.pdf", "https://docs.example.com/cut.pdf"
		h.web.page(read, "application/pdf", pdf)
		during := make(chan []os.DirEntry, 1)
		h.web.handle(over, func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/pdf")
			var written int64
			_ = pdftest.Filler(writerFunc(func(p []byte) (int, error) {
				n, err := rw.Write(p)
				if before := written; written+int64(n) > 20<<20 && before <= 20<<20 {
					// The client has read megabytes: its file exists, without name.
					list, _ := os.ReadDir(h.tmpPath)
					during <- list
				}
				written += int64(n)
				return n, err
			}), extraction.MaxPDFSize+1)
		})
		arrived := make(chan struct{})
		h.web.handle(cut, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/pdf")
			_, _ = rw.Write(pdf[:len(pdf)/2])
			rw.(http.Flusher).Flush()
			close(arrived)
			<-r.Context().Done()
		})
		stop := h.start()
		readItem := h.captureLink(read, false)
		h.waitPass()
		assertExtracted(t, h.item(h.owner, readItem.ID), "full")
		assertNoTempFile(t, h)
		overItem := h.captureLink(over, false)
		h.waitPass()
		assertExtracted(t, h.item(h.owner, overItem.ID), "fallback")
		assertNoTempFile(t, h)
		select {
		case list := <-during:
			if len(list) != 0 {
				t.Errorf("temporary directory during the download holds %d files, want none", len(list))
			}
		default:
			t.Error("the directory was not listed during the download")
		}
		captured := h.captureLink(cut, false)
		select {
		case <-arrived:
		case <-time.After(waitLimit):
			t.Fatal("the PDF was never requested")
		}
		stop()
		if item := h.item(h.owner, captured.ID); item.Status != "queued" {
			t.Errorf("status %q after a stop, want queued", item.Status)
		}
		assertNoTempFile(t, h)
	})

	t.Run("A large PDF does not grow the memory of the service", func(t *testing.T) {
		h := newHarness(t)
		path := filepath.Join(t.TempDir(), "large")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pdftest.Large(45_000_000, 90).WriteTo(file); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		const link = "https://docs.example.com/large.pdf"
		h.web.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/pdf")
			http.ServeFile(rw, r, path) // streamed from the disk: the test server holds no copy
		})
		var item store.Item
		grown := heapPeak(func() { item = h.extractLink(link) })
		assertExtracted(t, item, "full")
		if grown > 8<<20 {
			t.Errorf("the heap of the service grew by %d MiB, want less than 8", grown>>20)
		}
	})

	// Requirement: Links left waiting as PDFs are picked up.

	t.Run("Link left waiting as a PDF is extracted", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://docs.example.com/papers/waiting.pdf"
		pdf, _ := tuningPDF()
		h.web.page(link, "application/pdf", pdf)
		captured := h.captureLink(link, false) // no worker: the previous version deferred it
		insertDeferredPDF(t, h.dbPath, h.owner, captured.ID)
		h.start()
		assertExtracted(t, h.item(h.owner, captured.ID), "full")
		if got := h.web.requestsTo(link); got != 1 {
			t.Errorf("URL requested %d times, want once", got)
		}
	})

	t.Run("Links left waiting as PDFs of two users", func(t *testing.T) {
		h := newHarness(t)
		const mine, theirs = "https://docs.example.com/mine.pdf", "https://docs.example.com/theirs.pdf"
		h.web.page(mine, "application/pdf", textPDF(pdftest.Doc{Title: []byte("Document du premier")}, "Texte du premier utilisateur"))
		h.web.page(theirs, "application/pdf", textPDF(pdftest.Doc{Title: []byte("Document du second")}, "Texte du second utilisateur"))
		first := h.captureFor(h.owner, mine, false)
		second := h.captureFor(h.other, theirs, false)
		insertDeferredPDF(t, h.dbPath, h.owner, first.ID)
		insertDeferredPDF(t, h.dbPath, h.other, second.ID)
		h.start()
		ownerItem, otherItem := h.item(h.owner, first.ID), h.item(h.other, second.ID)
		assertExtracted(t, ownerItem, "full")
		assertExtracted(t, otherItem, "full")
		if ownerItem.Title != "Document du premier" || !strings.HasPrefix(h.contentOf(ownerItem), "Texte du premier") {
			t.Errorf("owner's item: title %q", ownerItem.Title)
		}
		otherContent, err := h.store.ItemContent(context.Background(), h.other, second.ID)
		if otherItem.Title != "Document du second" || err != nil || otherContent == nil || !strings.HasPrefix(*otherContent, "Texte du second") {
			t.Errorf("other user's item: title %q, content %v", otherItem.Title, err)
		}
		if _, err := h.store.Item(context.Background(), h.other, first.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the owner's item is visible to the other user: %v", err)
		}
	})
}
