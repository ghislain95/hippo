package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Analyzer is the AI service, behind which the Anthropic SDK hides (ADR-0005, design.md D9):
// internal/analysis/anthropic implements it, analysistest fakes it. E3-06 adds its methods.
type Analyzer interface {
	// CreateBatch sends the Message Batches create body, size bytes read from body, once: never
	// retried, since a second create would be a second batch. It returns the batch Anthropic
	// accepted, an error wrapping ErrRejected when the service answered with a client error
	// status (4xx: the send definitely failed), or any other error when the outcome is unknown:
	// no answer, or a server error status (5xx, a *ServerError), which may follow a batch the
	// service did create.
	CreateBatch(ctx context.Context, body io.ReadSeeker, size int64) (BatchRef, error)
	// ListBatches returns the batches the key holds, at least those created at or after since
	// (bounded: older ones may be left out).
	ListBatches(ctx context.Context, since time.Time) ([]BatchInfo, error)
	// GetBatch returns the processing state of the batch anthropicID, ErrResultsUnavailable when
	// the service does not know it (404) or has archived it, a *QueryError for any other error
	// status (apply-batch-results design D2). A read: the client may retry it.
	GetBatch(ctx context.Context, anthropicID string) (BatchState, error)
	// Results opens the results file of the ended batch anthropicID, read one line at a time,
	// ErrResultsUnavailable on a 404, a *QueryError for any other error status. The caller
	// closes the reader.
	Results(ctx context.Context, anthropicID string) (ResultReader, error)
}

// Processing states of a batch, as the service reports them (BatchState.Processing).
const (
	ProcessingInProgress = "in_progress"
	ProcessingCanceling  = "canceling"
	ProcessingEnded      = "ended"
)

// Kinds of result of one request (Result.Kind).
const (
	ResultSucceeded = "succeeded"
	ResultErrored   = "errored"
	ResultCanceled  = "canceled"
	ResultExpired   = "expired"
)

// BatchState is the processing state of a batch.
type BatchState struct {
	// Processing is ProcessingInProgress, ProcessingCanceling or ProcessingEnded.
	Processing string
	// EndedAt is when the service ended the batch; zero until it ended.
	EndedAt time.Time
}

// ResultReader reads the results file of a batch one line at a time, so that one line at most
// is in memory (design D3).
type ResultReader interface {
	// Next returns the next result, io.EOF at the end of the file, ErrUnreadableLine for a
	// line skipped (the read goes on), or any other error when the read is cut (transport,
	// context): the read stops there.
	Next() (Result, error)
	Close() error
}

// Result is one line of a results file: the outcome of one request. It is Hippo's, built by
// the client from the line, never a type of the SDK (design D2).
type Result struct {
	// CustomID is the custom_id of the request: the item id.
	CustomID string
	// Kind is ResultSucceeded, ResultErrored, ResultCanceled or ResultExpired.
	Kind string
	// StopReason is the stop reason of a succeeded request.
	StopReason string
	// ToolCalls are the tool_use blocks of a succeeded response, in order; text blocks are not
	// kept.
	ToolCalls []ToolCall
	// Usage are the tokens of a succeeded request.
	Usage Usage
	// ErrorType is the API error type of an errored request, never its message, which may
	// quote the request.
	ErrorType string
}

// ToolCall is one tool_use block of a response.
type ToolCall struct {
	Name  string
	Input json.RawMessage
}

// Usage are the tokens of a request, the cache writes split by duration (RG-80, design D12).
type Usage struct {
	Input, Output, CacheWrite5m, CacheWrite1h, CacheRead int64
}

// Add returns the sum of u and v.
func (u Usage) Add(v Usage) Usage {
	return Usage{
		Input: u.Input + v.Input, Output: u.Output + v.Output, CacheWrite5m: u.CacheWrite5m + v.CacheWrite5m,
		CacheWrite1h: u.CacheWrite1h + v.CacheWrite1h, CacheRead: u.CacheRead + v.CacheRead,
	}
}

// ErrResultsUnavailable: the service no longer serves the results of the batch (not found,
// archived, past its retention). The batch is closed with the tokens already read (decision 1
// of apply-batch-results, design D8).
var ErrResultsUnavailable = errors.New("batch results no longer served")

// ErrUnreadableLine: a line of the results file is not a result Hippo can read (not JSON, over
// 4 MiB, no custom_id, unknown result type). It is skipped and counted; its item has no result.
var ErrUnreadableLine = errors.New("unreadable result line")

// QueryError is a query of a batch or of its results answered with an error status other than
// a 404: its HTTP status and the type of its error, never its message. Nothing is written; the
// batch is followed again at the next occasion.
type QueryError struct {
	Status int
	Type   string
}

func (e *QueryError) Error() string {
	if e.Type == "" {
		return fmt.Sprintf("query failed: status %d", e.Status)
	}
	return fmt.Sprintf("query failed: status %d, %s", e.Status, e.Type)
}

// BatchRef is a batch Anthropic accepted.
type BatchRef struct {
	ID        string
	CreatedAt time.Time
}

// BatchInfo is a batch the key holds, as the reconciliation matches it (design.md D8).
type BatchInfo struct {
	ID        string
	CreatedAt time.Time
	// RequestCount is the sum of the batch's request counts, whatever their state.
	RequestCount int
}

// ErrRejected: the AI service answered the send with a client error status (4xx); the batch
// does not exist, its items go back to extracted (RG-30). A RejectedError wraps it.
var ErrRejected = errors.New("batch rejected")

// RejectedError is a send the AI service refused: its HTTP status and the type of its error
// (rate_limit_error, invalid_request_error…), never its message, which may quote the request.
type RejectedError struct {
	Status int
	Type   string
}

func (e *RejectedError) Error() string {
	if e.Type == "" {
		return fmt.Sprintf("batch rejected: status %d", e.Status)
	}
	return fmt.Sprintf("batch rejected: status %d, %s", e.Status, e.Type)
}

// Unwrap makes a RejectedError match ErrRejected.
func (e *RejectedError) Unwrap() error { return ErrRejected }

// ServerError is a send answered with a server error status (5xx, 529 included): its outcome
// is unknown, since a gateway may answer so after the service created the batch. It does not
// match ErrRejected: the batch stays pending and is reconciled (design.md D8).
type ServerError struct {
	Status int
	Type   string
}

func (e *ServerError) Error() string {
	if e.Type == "" {
		return fmt.Sprintf("server error: status %d", e.Status)
	}
	return fmt.Sprintf("server error: status %d, %s", e.Status, e.Type)
}
