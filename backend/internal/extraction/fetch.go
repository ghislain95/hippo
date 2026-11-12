package extraction

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"
)

const (
	// DownloadTimeout bounds a whole download: DNS, connection, TLS, redirects, headers and body
	// (RG-23).
	DownloadTimeout = 15 * time.Second
	// MaxBodySize is the largest body read, decompressed, in bytes; a longer page falls back.
	// 2 MB rather than 5: Readability peaked at 59 MB of heap on a 5 MB article, 23 MB on a 2 MB
	// one, and the child that reads a page has 48 MiB (design.md, task 3.3 and 3.4).
	MaxBodySize = 2 << 20
	// maxRedirects is the number of redirects followed before the link falls back.
	maxRedirects = 10
	// sniffLength is what is read of a body before its format is decided: enough to recognise
	// a PDF.
	sniffLength = 512
	// sniffBuffer is the buffer the first bytes are peeked through.
	sniffBuffer = 4 << 10
	// acceptHeader asks for HTML first, and accepts a PDF: a site that negotiates the type
	// answers a PDF link instead of 406.
	acceptHeader = "text/html,application/xhtml+xml,application/pdf;q=0.9"
	// maxJSONSize is the largest JSON body read in modeJSON, decompressed, in bytes: an oEmbed
	// answer is a few hundred bytes.
	maxJSONSize = 64 << 10
)

// fetchMode is how fetch reads the body of a success status.
type fetchMode int

const (
	// modePage reads a page as a link is read: HTML in memory up to MaxBodySize, a PDF streamed
	// to a temporary file up to MaxPDFSize, any other format to its first bytes.
	modePage fetchMode = iota
	// modeHTML reads HTML in memory up to MaxBodySize and any other format, PDF included, to
	// its first bytes: never a temporary file. The page of a video.
	modeHTML
	// modeJSON reads the body in memory up to maxJSONSize whatever its type, TooLarge beyond.
	// An oEmbed answer.
	modeJSON
)

// fetchRequest are the options of one download.
type fetchRequest struct {
	// Timeout bounds the whole download, derived inside fetch from the worker's context, so
	// that classify with that context tells a deadline (timeout) from a stop.
	Timeout time.Duration
	Accept  string
	Mode    fetchMode
}

// pageRequest is the download of a captured link: the page path, PDF included.
func pageRequest(timeout time.Duration) fetchRequest {
	return fetchRequest{Timeout: timeout, Accept: acceptHeader, Mode: modePage}
}

var (
	// errBlocked: the address is local or private (RG-27). It never names the address.
	errBlocked = errors.New("address refused by the address policy")
	// errNoAddress: the host resolved to no address.
	errNoAddress = errors.New("host has no address")
	errRedirects = errors.New("too many redirects")
	errScheme    = errors.New("redirect to a scheme other than http or https")
)

// Resolver gives the addresses to dial for a host name and a port.
type Resolver interface {
	Resolve(ctx context.Context, host string, port uint16) ([]netip.AddrPort, error)
}

// systemResolver resolves through the resolver of the system.
type systemResolver struct{ resolver *net.Resolver }

func (r systemResolver) Resolve(ctx context.Context, host string, port uint16) ([]netip.AddrPort, error) {
	addrs, err := r.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		// Unwrapped on purpose: classify reads its type; it names the host, so it is never logged.
		return nil, err
	}
	targets := make([]netip.AddrPort, 0, len(addrs))
	for _, addr := range addrs {
		targets = append(targets, netip.AddrPortFrom(addr, port))
	}
	return targets, nil
}

// fetcher downloads pages with its own HTTP client: no proxy, no persistent cookie, the address
// policy checked on every connection, redirects included (RG-27).
type fetcher struct {
	transport *http.Transport
	resolver  Resolver
	dialer    *net.Dialer
	userAgent string
	limit     int64
	temp      *TempDir // where PDFs are written
}

func newFetcher(policy func(netip.AddrPort) bool, resolver Resolver, rootCAs *x509.CertPool, version string, temp *TempDir) *fetcher {
	f := &fetcher{
		resolver: resolver,
		dialer: &net.Dialer{
			// The deadline of each fetch bounds a connection first; this is only a ceiling.
			Timeout: DownloadTimeout,
			// Control sees the address actually dialled, after resolution: a host that resolves
			// to a local address, a DNS rebinding or a redirect to one are all refused here.
			Control: func(_, address string, _ syscall.RawConn) error {
				addr, err := netip.ParseAddrPort(address)
				if err != nil || !policy(addr) {
					return errBlocked
				}
				return nil
			},
		},
		userAgent: "Hippo/" + version + " (personal read-later)", // ASCII only: some servers refuse other bytes
		limit:     MaxBodySize,
		temp:      temp,
	}
	f.transport = &http.Transport{
		Proxy:                  nil,
		DialContext:            f.dial,
		TLSClientConfig:        &tls.Config{RootCAs: rootCAs, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    10 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
		IdleConnTimeout:        30 * time.Second,
	}
	return f
}

// dial connects to addr ("host:port"), trying each address of the host in turn.
func (f *fetcher) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("dial: port: %w", err)
	}
	var targets []netip.AddrPort
	if ip, err := netip.ParseAddr(host); err == nil {
		targets = []netip.AddrPort{netip.AddrPortFrom(ip, uint16(port))}
	} else if targets, err = f.resolver.Resolve(ctx, host, uint16(port)); err != nil {
		return nil, fmt.Errorf("dial: resolve: %w", err)
	}
	if len(targets) == 0 {
		return nil, errNoAddress
	}
	var errs []error
	for _, target := range targets {
		conn, err := f.dialer.DialContext(ctx, network, target.String())
		if err == nil {
			return conn, nil
		}
		errs = append(errs, err)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.Join(errs...)
}

// fetch downloads target within req.Timeout of ctx, reading its body as req.Mode says. In
// modePage, an HTML page is read in memory, up to MaxBodySize; a PDF is streamed to a temporary
// file without name, up to MaxPDFSize, which the caller closes (response.PDF). The response
// keeps its format and final URL along with an error of the body, so that a PDF that fails
// falls back with its file name. A body that ends once the call is over is such an error, of
// the context: none of it is kept. The error names the URL: it is classified, never logged nor
// stored.
func (f *fetcher) fetch(ctx context.Context, target *url.URL, req fetchRequest) (response, error) {
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()
	defer f.transport.CloseIdleConnections()

	clean := *target
	clean.User = nil // net/http would send it as Basic Auth
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, clean.String(), nil)
	if err != nil {
		return response{}, fmt.Errorf("fetch: %w", err)
	}
	httpReq.Header.Set("User-Agent", f.userAgent)
	httpReq.Header.Set("Accept", req.Accept)
	httpReq.Header.Set("Accept-Language", "fr,en")
	// No Accept-Encoding: the transport asks for gzip itself and decompresses transparently.

	// A jar for this item only: consent redirects that set a cookie do not loop, and nothing is
	// kept from one site to the next.
	jar, err := cookiejar.New(nil)
	if err != nil {
		return response{}, fmt.Errorf("fetch: %w", err)
	}
	client := &http.Client{Transport: f.transport, Jar: jar, CheckRedirect: checkRedirect}
	resp, err := client.Do(httpReq)
	if err != nil {
		return response{}, fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read only

	out := response{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), URL: resp.Request.URL}
	if out.Status < 200 || out.Status > 299 {
		return out, nil
	}
	out, err = f.readBody(resp, req.Mode, out)
	if err == nil && !out.TooLarge && ctx.Err() != nil {
		// A body that ends cleanly once the call is over may be cut: at the deadline the
		// transport sends close_notify then closes the connection, and a server that ends a
		// chunked or unsized body on seeing close_notify gets its last bytes in before the close;
		// the read then ends on io.EOF instead of the deadline. Nothing read is kept, and the
		// error wraps the context's, so that classify tells a timeout from a stop. Too large
		// stands: more than the limit was read.
		out.Body = nil
		if out.PDF != nil {
			_ = out.PDF.Close() // no name: closing frees the data, there is nothing to flush
			out.PDF = nil
		}
		return out, fmt.Errorf("fetch: read body: %w", ctx.Err())
	}
	return out, err
}

// readBody reads the body of a success status as mode says, into out.
func (f *fetcher) readBody(resp *http.Response, mode fetchMode, out response) (response, error) {
	if mode == modeJSON {
		return readJSON(resp.Body, out)
	}
	if mode == modePage && mediaTypeOf(out.ContentType) == "application/pdf" && resp.ContentLength > MaxPDFSize {
		// Announced too large: not a byte of the body is read.
		out.Format, out.TooLarge = formatPDF, true
		return out, nil
	}
	body := bufio.NewReaderSize(resp.Body, sniffBuffer)
	head, err := body.Peek(sniffLength)
	// The format is known from the type and the bytes received even when the body stalls, so
	// that a PDF that does not download in time keeps the title order of a PDF.
	out.Format = sniffFormat(out.ContentType, head)
	if err != nil && !errors.Is(err, io.EOF) {
		return out, fmt.Errorf("fetch: read body: %w", err)
	}
	switch {
	case out.Format == formatPDF && mode == modePage:
		return f.downloadPDF(body, resp.ContentLength, out)
	case out.Format == formatHTML:
		// The limit applies to the decompressed stream: a gzip bomb or a lying Content-Length
		// stops at the limit.
		page, err := io.ReadAll(io.LimitReader(body, f.limit+1))
		if err != nil {
			return out, fmt.Errorf("fetch: read body: %w", err)
		}
		if int64(len(page)) > f.limit {
			out.TooLarge = true
			return out, nil
		}
		out.Body = page
	default:
		out.Body = bytes.Clone(head)
	}
	return out, nil
}

// readJSON reads a JSON body whole, up to maxJSONSize of the decompressed stream; beyond, out
// is TooLarge without body.
func readJSON(body io.Reader, out response) (response, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxJSONSize+1))
	if err != nil {
		return out, fmt.Errorf("fetch: read body: %w", err)
	}
	if len(data) > maxJSONSize {
		out.TooLarge = true
		return out, nil
	}
	out.Body = data
	return out, nil
}

// downloadPDF copies the body of a PDF to a temporary file without name, up to MaxPDFSize of
// the decompressed stream: the memory of the service does not grow with the PDF. A volume
// without room for it, or a failure to create or write the file, wraps errStorage; a failure
// to read the body is the site's.
func (f *fetcher) downloadPDF(body io.Reader, length int64, out response) (response, error) {
	if length > MaxPDFSize {
		out.TooLarge = true
		return out, nil
	}
	// A gzip body that the transport decompresses comes with no length (-1): its size is
	// unknown until read, MaxPDFSize at most.
	need := uint64(MaxPDFSize)
	if length >= 0 {
		need = uint64(length)
	}
	if err := f.temp.checkRoom(need); err != nil {
		return out, fmt.Errorf("fetch: %w", err)
	}
	file, err := f.temp.create()
	if err != nil {
		return out, fmt.Errorf("fetch: %w", err)
	}
	n, err := io.Copy(file, io.LimitReader(body, MaxPDFSize+1))
	if err == nil && n <= MaxPDFSize {
		out.PDF = file
		return out, nil
	}
	_ = file.Close() // no name: closing frees the data, there is nothing to flush
	if err != nil {
		return out, fmt.Errorf("fetch: read body: %w", err)
	}
	out.TooLarge = true
	return out, nil
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errRedirects
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return errScheme
	}
	req.URL.User = nil
	// net/http sets the page that redirected as Referer: a site learns nothing but the URL
	// asked for it (RG-92).
	req.Header.Del("Referer")
	return nil
}

// classify tells why a download failed. stopped is ctx, the context of the worker: when it is
// done, the backend is stopping and the item must stay queued.
func classify(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return reasonStopped
	}
	if errors.Is(err, errStorage) {
		return reasonStorageUnavailable
	}
	if errors.Is(err, errBlocked) {
		return reasonBlocked
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		// "No such host" is an answer: the resolver works and the site does not exist. Any
		// other DNS failure (no server reachable, timeout, server failure) is the NAS's.
		if dnsErr.IsNotFound {
			return reasonUnreachable
		}
		return reasonNetwork
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return reasonNetwork
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return reasonTimeout
	}
	return reasonUnreachable
}
