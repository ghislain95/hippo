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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata" // the Paris zone of the capture without the machine's time zone database

	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// waitLimit bounds every wait on the worker: a test that reaches it has failed.
const waitLimit = 20 * time.Second

// web is a fictional web served over TLS by one httptest server: every host of example.com
// resolves to it through the test resolver, so that no test touches the network.
type web struct {
	server   *httptest.Server
	addr     netip.AddrPort
	roots    *x509.CertPool
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc // host + path
	requests map[string]int
	headers  map[string]http.Header
	conns    atomic.Int64
	lookups  atomic.Int64
	failDNS  atomic.Bool
	failing  map[string]bool // hosts whose resolution fails as on a NAS outage
}

func newWeb(t *testing.T) *web {
	t.Helper()
	w := &web{handlers: map[string]http.HandlerFunc{}, requests: map[string]int{}, headers: map[string]http.Header{}, failing: map[string]bool{}}
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
	if host == "example.com" || strings.HasSuffix(host, ".example.com") {
		return []netip.AddrPort{w.addr}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

// testCertificate is a self-signed certificate of example.com and its subdomains.
func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "example.com"},
		DNSNames:              []string{"example.com", "*.example.com"},
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
	logs     *syncBuffer
	relay    *relay
	passes   chan struct{}
	pause    chan time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st := storetest.Open(t)
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
		t: t, store: st, owner: owner, other: other, web: newWeb(t),
		policy: &policyRecorder{policy: testPolicy}, logs: &syncBuffer{}, relay: &relay{},
		passes: make(chan struct{}, 1), pause: make(chan time.Time),
	}
	h.resolver = h.web
	h.reader = childArgv(extraction.ChildCommand)
	h.capture = capture.NewService(st, h.relay, paris, slog.New(slog.NewJSONHandler(h.logs, nil)))
	t.Cleanup(func() {
		// Whatever the test, the logs never name a site or an address (RG-90, ADR-0027).
		for _, secret := range []string{"example.com", "youtube.com", "youtu.be", "127.0.0.1", "localhost", "192.168.", "10.0.0.5", "Tuning SQLite"} {
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
		Version: "test", Timeout: h.timeout, ReaderCommand: h.reader, ReadTimeout: h.readFor,
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
		if headers.Get("Accept") != "text/html,application/xhtml+xml" || headers.Get("Accept-Language") != "fr,en" {
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

	// Requirement: YouTube and PDF links wait for their own extraction.

	t.Run("YouTube link", func(t *testing.T) {
		h := newHarness(t)
		h.start()
		var ids []string
		for _, link := range []string{"https://www.youtube.com/watch?v=abc", "https://youtu.be/abc", "https://YouTube.com/shorts/xyz"} {
			ids = append(ids, h.captureLink(link, false).ID)
			h.waitPass()
		}
		for _, id := range ids {
			if item := h.item(h.owner, id); item.Status != "queued" || item.ExtractionMode != nil {
				t.Errorf("item %s: status %q mode %q, want queued", id, item.Status, mode(item))
			}
		}
		if asked, lookups := len(h.policy.addresses()), h.web.lookups.Load(); asked+int(lookups) != 0 {
			t.Errorf("dials %d, lookups %d: want no request", asked, lookups)
		}
	})

	t.Run("PDF link", func(t *testing.T) {
		h := newHarness(t)
		const link = "https://docs.example.com/paper.pdf"
		h.web.page(link, "application/pdf", []byte("%PDF-1.7\nfictional\n"))
		stop := h.start()
		captured := h.captureLink(link, false)
		h.waitPass()
		stop()
		h.start() // the backend restarts
		item := h.item(h.owner, captured.ID)
		if item.Status != "queued" || item.ExtractionMode != nil || h.content(item.ID) != nil {
			t.Errorf("status %q mode %q, want queued without content", item.Status, mode(item))
		}
		if got := h.web.requestsTo(link); got != 1 {
			t.Errorf("URL requested %d times, want once", got)
		}
		if len(h.queued(h.owner)) != 0 {
			t.Error("the deferred PDF is still listed for extraction")
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
