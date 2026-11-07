package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	sdk "github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/ghislain95/hippo/backend/internal/analysis"
)

// MaxResultLine bounds a line of a results file, in bytes (4 MiB, design D3 of
// apply-batch-results): a response of 8 000 output tokens is about 32 KB, a few hundred KB with
// its JSON escapes. A longer line is skipped as unreadable, so that memory stays bounded.
const MaxResultLine = 4 << 20

// readerSize is the size of the buffer the results file is read through.
const readerSize = 64 << 10

// errClosed: Next after Close.
var errClosed = errors.New("anthropic: results reader closed")

// GetBatch returns the processing state of the batch anthropicID (design D2): its processing
// status, and its end once ended. An archived batch (archived_at set: « its results became
// unavailable ») and a 404 answer analysis.ErrResultsUnavailable; any other error status an
// *analysis.QueryError (status and error type only); a done context its error. A read: the
// SDK's default retries apply, within the caller's deadline.
func (c *Client) GetBatch(ctx context.Context, anthropicID string) (analysis.BatchState, error) {
	var raw *http.Response
	b, err := c.sdk.Messages.Batches.Get(ctx, anthropicID, sdk.MessageBatchGetParams{}, option.WithResponseInto(&raw))
	if err != nil {
		return analysis.BatchState{}, fmt.Errorf("get batch: %w", queryOutcome(ctx, err, raw))
	}
	if !b.ArchivedAt.IsZero() {
		return analysis.BatchState{}, fmt.Errorf("get batch: archived: %w", analysis.ErrResultsUnavailable)
	}
	state := analysis.BatchState{Processing: string(b.ProcessingStatus)}
	if !b.EndedAt.IsZero() {
		state.EndedAt = b.EndedAt.UTC()
	}
	return state, nil
}

// Results opens the results file of the ended batch anthropicID (design D2, D3): the path and
// the Accept header of the SDK's ResultsStreaming, but with a raw *http.Response destination,
// which the SDK hands over unread, so that Hippo reads the lines itself and skips one it cannot
// decode instead of stopping there. A 404 answers analysis.ErrResultsUnavailable, any other
// error status an *analysis.QueryError. The caller closes the reader, which closes the body.
func (c *Client) Results(ctx context.Context, anthropicID string) (analysis.ResultReader, error) {
	var raw *http.Response
	path := batchesPath + "/" + url.PathEscape(anthropicID) + "/results"
	err := c.sdk.Get(ctx, path, nil, &raw, option.WithHeader("Accept", "application/x-jsonl"))
	if err != nil {
		if raw != nil && raw.Body != nil {
			_ = raw.Body.Close() // read by the SDK already, to build its error
		}
		return nil, fmt.Errorf("read batch results: %w", queryOutcome(ctx, err, raw))
	}
	if raw == nil || raw.Body == nil {
		return nil, fmt.Errorf("read batch results: %w", errUnknown)
	}
	return NewResultReader(raw.Body), nil
}

// maxErrorType is the length of an API error type kept as it is (ErrorTypeClass).
const maxErrorType = 64

// ErrorTypeClass returns an API error type as it is logged and kept: the type when it is 1 to
// 64 lower-case ASCII letters and underscores, as every type of the API is
// (invalid_request_error, overloaded_error…), empty when the answer has none (a gateway's
// page), and other otherwise. The type comes from the answer of the service: bounded, it never
// carries a quoted request nor an unbounded string into a log line or the database.
func ErrorTypeClass(errorType string) string {
	if errorType == "" {
		return ""
	}
	if len(errorType) > maxErrorType {
		return "other"
	}
	for i := range len(errorType) {
		if c := errorType[i]; (c < 'a' || c > 'z') && c != '_' {
			return "other"
		}
	}
	return errorType
}

// queryOutcome turns an error of a query into Hippo's (design D2): ErrResultsUnavailable for a
// 404, an *analysis.QueryError for another error status, the context's error when it is done,
// errUnknown otherwise. raw is the response the SDK saw, when any: an error status whose body
// is not the API's JSON gives no *sdk.Error but still has its status. The SDK's errors are never
// wrapped: they print the URL and the API's answer.
func queryOutcome(ctx context.Context, err error, raw *http.Response) error {
	status, errorType := 0, ""
	var apiErr *sdk.Error
	switch {
	case errors.As(err, &apiErr):
		status, errorType = apiErr.StatusCode, ErrorTypeClass(string(apiErr.Type()))
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errRedirect):
		return errRedirect
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case raw != nil && raw.StatusCode >= 400:
		status = raw.StatusCode
	default:
		return errUnknown
	}
	if status == http.StatusNotFound {
		return analysis.ErrResultsUnavailable
	}
	return &analysis.QueryError{Status: status, Type: errorType}
}

// ResultReader reads a results file one line at a time (design D3): a line is read in pieces
// up to MaxResultLine, beyond which the rest of the line is discarded and the line is
// unreadable. Only the fields Hippo needs are decoded; an API error message is never kept.
type ResultReader struct {
	body   io.ReadCloser
	r      *bufio.Reader
	line   []byte
	closed bool
}

// NewResultReader returns the reader of the results file body; Close closes body.
func NewResultReader(body io.ReadCloser) *ResultReader {
	return &ResultReader{body: body, r: bufio.NewReaderSize(body, readerSize)}
}

// Next returns the next result: io.EOF at the end, analysis.ErrUnreadableLine for a line that
// is not a result Hippo reads (keep reading), any other error when the body is cut (stop).
// Blank lines are no lines.
func (r *ResultReader) Next() (analysis.Result, error) {
	for {
		if r.closed {
			return analysis.Result{}, errClosed
		}
		line, tooLong, err := r.readLine()
		if err != nil {
			return analysis.Result{}, err
		}
		if tooLong {
			return analysis.Result{}, fmt.Errorf("line over %d bytes: %w", MaxResultLine, analysis.ErrUnreadableLine)
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		return decodeLine(line)
	}
}

// Close closes the body.
func (r *ResultReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	r.line = nil
	if err := r.body.Close(); err != nil {
		return fmt.Errorf("close batch results: %w", err)
	}
	return nil
}

// readLine reads up to the next line break. A line over MaxResultLine is read to its end
// without being kept (tooLong). io.EOF comes only when nothing is left; the last line may lack
// its line break. A transport error is returned as it is, never as an unreadable line.
func (r *ResultReader) readLine() (line []byte, tooLong bool, err error) {
	if cap(r.line) > readerSize {
		r.line = nil // a long line is not kept in memory for the next ones
	}
	r.line = r.line[:0]
	for {
		piece, err := r.r.ReadSlice('\n')
		if !tooLong {
			if len(r.line)+len(bytes.TrimSuffix(piece, []byte("\n"))) > MaxResultLine {
				tooLong, r.line = true, r.line[:0]
			} else {
				r.line = append(r.line, piece...)
			}
		}
		switch {
		case err == nil:
			return bytes.TrimSuffix(r.line, []byte("\n")), tooLong, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if len(r.line) == 0 && !tooLong && len(piece) == 0 {
				return nil, false, io.EOF
			}
			return r.line, tooLong, nil
		default:
			return nil, false, fmt.Errorf("read batch results: %w", transportError(err))
		}
	}
}

// transportError keeps of a read error only what names no URL: the context's errors and an
// unexpected end of the body.
func transportError(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, io.ErrUnexpectedEOF):
		return io.ErrUnexpectedEOF
	}
	return errUnknown
}

// resultLine holds the fields of a results line Hippo reads (field names of
// MessageBatchIndividualResponse and its nested types, anthropic-sdk-go v2.7.0, design D3).
// Everything else, the text blocks and the API's error message included, is skipped by the
// decoder.
type resultLine struct {
	CustomID string `json:"custom_id"`
	Result   struct {
		Type    string `json:"type"`
		Message *struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
			Usage struct {
				InputTokens              int64 `json:"input_tokens"`
				OutputTokens             int64 `json:"output_tokens"`
				CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
				CacheCreation            *struct {
					Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
					Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
				} `json:"cache_creation"`
			} `json:"usage"`
		} `json:"message"`
		Error *struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		} `json:"error"`
	} `json:"result"`
}

// decodeLine decodes one line into a Result, or answers analysis.ErrUnreadableLine: not JSON,
// no custom_id, a result type outside the four, a succeeded result without its message. The
// error never quotes the line.
func decodeLine(raw []byte) (analysis.Result, error) {
	var line resultLine
	if err := json.Unmarshal(raw, &line); err != nil {
		return analysis.Result{}, fmt.Errorf("not a result: %w", analysis.ErrUnreadableLine)
	}
	if line.CustomID == "" {
		return analysis.Result{}, fmt.Errorf("no custom_id: %w", analysis.ErrUnreadableLine)
	}
	result := analysis.Result{CustomID: line.CustomID, Kind: line.Result.Type}
	switch line.Result.Type {
	case analysis.ResultSucceeded:
		m := line.Result.Message
		if m == nil {
			return analysis.Result{}, fmt.Errorf("succeeded without message: %w", analysis.ErrUnreadableLine)
		}
		result.StopReason = m.StopReason
		for _, block := range m.Content {
			if block.Type == "tool_use" {
				result.ToolCalls = append(result.ToolCalls, analysis.ToolCall{Name: block.Name, Input: block.Input})
			}
		}
		u := m.Usage
		result.Usage = analysis.Usage{Input: u.InputTokens, Output: u.OutputTokens, CacheRead: u.CacheReadInputTokens}
		// Without a breakdown, every cache write is a 5-minute one, the only duration the
		// requests set (E3-01 D4); a breakdown short of the total counts the rest the same way.
		if b := u.CacheCreation; b != nil {
			result.Usage.CacheWrite5m, result.Usage.CacheWrite1h = b.Ephemeral5m, b.Ephemeral1h
		}
		if rest := u.CacheCreationInputTokens - result.Usage.CacheWrite5m - result.Usage.CacheWrite1h; rest > 0 {
			result.Usage.CacheWrite5m += rest
		}
	case analysis.ResultErrored:
		if line.Result.Error != nil {
			result.ErrorType = ErrorTypeClass(line.Result.Error.Error.Type)
		}
	case analysis.ResultCanceled, analysis.ResultExpired:
	default:
		return analysis.Result{}, fmt.Errorf("unknown result type: %w", analysis.ErrUnreadableLine)
	}
	return result, nil
}
