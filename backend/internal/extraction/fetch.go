package extraction

import (
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
	// sniffLength is what is read of a body whose type is not HTML: enough to recognise a PDF.
	sniffLength = 512
)

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
	timeout   time.Duration
	limit     int64
}

func newFetcher(policy func(netip.AddrPort) bool, resolver Resolver, rootCAs *x509.CertPool, version string, timeout time.Duration) *fetcher {
	f := &fetcher{
		resolver: resolver,
		dialer: &net.Dialer{
			Timeout: timeout,
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
		timeout:   timeout,
		limit:     MaxBodySize,
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

// fetch downloads target within f.timeout of ctx. The error it returns names the URL: it is
// classified, never logged nor stored.
func (f *fetcher) fetch(ctx context.Context, target *url.URL) (response, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	defer f.transport.CloseIdleConnections()

	clean := *target
	clean.User = nil // net/http would send it as Basic Auth
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clean.String(), nil)
	if err != nil {
		return response{}, fmt.Errorf("fetch: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "fr,en")
	// No Accept-Encoding: the transport asks for gzip itself and decompresses transparently.

	// A jar for this item only: consent redirects that set a cookie do not loop, and nothing is
	// kept from one site to the next.
	jar, err := cookiejar.New(nil)
	if err != nil {
		return response{}, fmt.Errorf("fetch: %w", err)
	}
	client := &http.Client{Transport: f.transport, Jar: jar, CheckRedirect: checkRedirect}
	resp, err := client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // read only

	out := response{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), URL: resp.Request.URL}
	if out.Status < 200 || out.Status > 299 {
		return out, nil
	}
	switch mediaTypeOf(out.ContentType) {
	case "application/pdf":
		return out, nil
	case "", "text/html", "application/xhtml+xml":
		// The limit applies to the decompressed stream: a gzip bomb or a lying Content-Length
		// stops at the limit.
		body, err := io.ReadAll(io.LimitReader(resp.Body, f.limit+1))
		if err != nil {
			return response{}, fmt.Errorf("fetch: read body: %w", err)
		}
		if int64(len(body)) > f.limit {
			out.TooLarge = true
			return out, nil
		}
		out.Body = body
	default:
		head := make([]byte, sniffLength)
		n, err := io.ReadFull(resp.Body, head)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return response{}, fmt.Errorf("fetch: read body: %w", err)
		}
		out.Body = head[:n]
	}
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
