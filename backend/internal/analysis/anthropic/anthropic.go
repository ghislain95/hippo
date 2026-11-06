// Package anthropic is the analysis.Analyzer backed by the Anthropic API, through
// anthropic-sdk-go: the only package that imports the SDK (depguard rule
// anthropic-only-in-analysis, ADR-0005, design.md D9 of submit-nightly-batch).
//
// The body of a batch is Hippo's own bytes, written by internal/analysis to an unnamed file:
// this package streams it, with its Content-Length, and never retries its create, since a
// second create would be a second batch. Errors carry an HTTP status and an API error type
// at most: never the API's message (it may quote the request), the URL nor the key.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/ghislain95/hippo/backend/internal/analysis"
)

const (
	// ListPageSize is the number of batches per page of a list (the API allows 1 to 1000; its
	// default is 20).
	ListPageSize = 100
	// maxListPages bounds a list: the reconciliation needs the batches of a few minutes, the
	// newest come first (design.md D8).
	maxListPages = 10
	// batchesPath is the path of the Message Batches API, relative to the base URL.
	batchesPath = "v1/messages/batches"
	// dialTimeout bounds the connection to the API.
	dialTimeout = 30 * time.Second
	// responseHeaderTimeout bounds the wait for the answer once the request, body included, is
	// written.
	responseHeaderTimeout = 5 * time.Minute
)

// errUnknown marks a send whose outcome is unknown: no HTTP status came back.
var errUnknown = errors.New("anthropic: no answer")

// errListTruncated: the list reached its page limit while older pages still held recent
// batches. A partial list could make the reconciliation conclude « no candidate » and send
// the items again: it fails instead, and the batch stays pending (design.md D8).
var errListTruncated = errors.New("anthropic: batch list truncated at its page limit")

// Client talks to the Message Batches API with one key.
type Client struct {
	sdk sdk.Client
}

// Option configures a Client.
type Option func(*settings)

type settings struct {
	baseURL    string
	httpClient *http.Client
}

// WithBaseURL sends the requests to baseURL instead of https://api.anthropic.com/ (tests).
func WithBaseURL(baseURL string) Option {
	return func(s *settings) { s.baseURL = baseURL }
}

// WithHTTPClient sends the requests through a copy of client that follows no redirect (tests).
func WithHTTPClient(client *http.Client) Option {
	return func(s *settings) { s.httpClient = client }
}

// New returns a client for apiKey. The SDK's environment variables (ANTHROPIC_BASE_URL,
// ANTHROPIC_CUSTOM_HEADERS, other credentials…) are ignored: the configuration of Hippo is
// what it reads in config.Load.
func New(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, errors.New("anthropic: an API key is required")
	}
	s := settings{baseURL: "https://api.anthropic.com/"}
	for _, opt := range opts {
		opt(&s)
	}
	if s.httpClient == nil {
		s.httpClient = newHTTPClient()
	} else {
		// A given client never follows a redirect either.
		client := *s.httpClient
		client.CheckRedirect = noRedirect
		s.httpClient = &client
	}
	return &Client{sdk: sdk.NewClient(
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(s.baseURL),
		option.WithHTTPClient(s.httpClient),
		// The documented header of an API key; the SDK's WithAPIKey would send it as a bearer
		// token.
		option.WithHeader("x-api-key", apiKey),
		option.WithMiddleware(refuseRedirects),
	)}, nil
}

// newHTTPClient is the client of the API: never http.DefaultClient (docs/dev/conventions-backend.md
// « HTTP sortant »). The total time of a call comes from its context (10 minutes for a create,
// 2 for a list, analysis.Pass); the transport bounds each step, so that a stalled server
// cannot hold a call beyond them. The response header timeout counts from the end of the body
// (net/http), so it does not limit the upload of a create.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:    15 * time.Second,
			ResponseHeaderTimeout:  responseHeaderTimeout,
			MaxResponseHeaderBytes: 64 << 10,
			IdleConnTimeout:        90 * time.Second,
			ForceAttemptHTTP2:      true,
		},
		CheckRedirect: noRedirect,
	}
}

// noRedirect never follows a redirect: net/http would send x-api-key to another host (it only
// strips Authorization, Cookie and Proxy-* headers). The 3xx comes back to refuseRedirects.
func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// errRedirect: the API answered a redirect, which Hippo never follows.
var errRedirect = errors.New("anthropic: redirect not followed")

// refuseRedirects turns a 3xx answer into an error: a create then has an unknown outcome, a
// list fails. It also covers an HTTP client given by WithHTTPClient.
func refuseRedirects(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	res, err := next(req)
	if err == nil && res != nil && res.StatusCode >= 300 && res.StatusCode < 400 {
		_ = res.Body.Close() // unread: a redirect is not followed
		return nil, errRedirect
	}
	return res, err
}

// CreateBatch sends the body of a batch, size bytes of body, once (design.md D7, D9):
//
//   - the body is handed to the SDK as an io.Reader that is neither a *bytes.Buffer nor a
//     *bytes.Reader, so it is set as the request body as it is, streamed, without GetBody;
//   - its Content-Length is set from size by a middleware, on the request the SDK sends;
//   - no retry: zero retries, and a request without GetBody is never retried by the SDK.
//
// A client error status (4xx) gives an analysis.RejectedError (status and error type only); a
// server error status (5xx) an analysis.ServerError, an unknown outcome; any other failure (no
// answer, a redirect) is an unknown outcome too. No error names the URL nor the key.
func (c *Client) CreateBatch(ctx context.Context, body io.ReadSeeker, size int64) (analysis.BatchRef, error) {
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return analysis.BatchRef{}, fmt.Errorf("create batch: rewind body: %w", err)
	}
	// io.LimitReader hides any other method of body (WriterTo, Close…): the SDK sees a plain
	// reader, and the HTTP client cannot close the caller's file.
	reader := io.LimitReader(body, size)
	var res sdk.MessageBatch
	err := c.sdk.Post(ctx, batchesPath, reader, &res,
		option.WithMaxRetries(0),
		option.WithHeader("Content-Type", "application/json"),
		option.WithMiddleware(func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
			req.ContentLength = size
			req.GetBody = nil
			return next(req)
		}),
	)
	if err != nil {
		return analysis.BatchRef{}, fmt.Errorf("create batch: %w", outcome(ctx, err))
	}
	if res.ID == "" {
		// Accepted, but the id is unreadable: the reconciliation will find it.
		return analysis.BatchRef{}, fmt.Errorf("create batch: %w: answer without batch id", errUnknown)
	}
	return analysis.BatchRef{ID: res.ID, CreatedAt: res.CreatedAt.UTC()}, nil
}

// ListBatches pages through the key's batches, newest first (the API's order), ListPageSize a
// page, and stops at a page whose batches are all older than since or at the last page. The
// batches older than since are left out. Reaching maxListPages pages while there are more and
// the last one still held a recent batch is an error (errListTruncated), never a partial list.
// A list is a read: the SDK's default retries apply, within the caller's deadline.
func (c *Client) ListBatches(ctx context.Context, since time.Time) ([]analysis.BatchInfo, error) {
	page, err := c.sdk.Messages.Batches.List(ctx, sdk.MessageBatchListParams{Limit: sdk.Int(ListPageSize)})
	var out []analysis.BatchInfo
	for pages := 0; ; pages++ {
		if err != nil {
			return nil, fmt.Errorf("list batches: %w", outcome(ctx, err))
		}
		if page == nil || len(page.Data) == 0 {
			return out, nil
		}
		recent := false
		for _, b := range page.Data {
			if b.CreatedAt.Before(since) {
				continue
			}
			recent = true
			counts := b.RequestCounts
			out = append(out, analysis.BatchInfo{
				ID: b.ID, CreatedAt: b.CreatedAt.UTC(),
				RequestCount: int(counts.Processing + counts.Succeeded + counts.Errored + counts.Canceled + counts.Expired),
			})
		}
		if !recent || !page.HasMore {
			return out, nil
		}
		if pages+1 >= maxListPages {
			return nil, fmt.Errorf("list batches: %w", errListTruncated)
		}
		page, err = page.GetNextPage()
	}
}

// outcome turns an error of the SDK into Hippo's: a RejectedError for a client error status
// (4xx), a ServerError for a server error status (5xx), the context's error when it is done,
// errUnknown otherwise. The SDK's errors are never wrapped: an *sdk.Error prints the URL and the
// API's answer, a transport error the URL.
func outcome(ctx context.Context, err error) error {
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			return &analysis.RejectedError{Status: apiErr.StatusCode, Type: string(apiErr.Type())}
		}
		return fmt.Errorf("%w: %w", errUnknown, &analysis.ServerError{Status: apiErr.StatusCode, Type: string(apiErr.Type())})
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("%w: %w", errUnknown, ctxErr)
	}
	switch {
	case errors.Is(err, errRedirect):
		return fmt.Errorf("%w: %w", errUnknown, errRedirect)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", errUnknown, context.DeadlineExceeded)
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%w: %w", errUnknown, context.Canceled)
	}
	return errUnknown
}
