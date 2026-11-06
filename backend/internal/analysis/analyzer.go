package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Analyzer is the AI service, behind which the Anthropic SDK hides (ADR-0005, design.md D9):
// internal/analysis/anthropic implements it, analysistest fakes it. E3-02 and E3-06 add their
// methods.
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
