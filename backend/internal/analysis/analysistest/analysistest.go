// Package analysistest fakes the AI service for the tests of the analysis (docs/dev/testing.md,
// « Dépendances externes »): an analysis.Analyzer that records every body it is sent and plays
// scripted outcomes. It is the boundary with a third-party service, not a mock of the store.
// No test reaches the network.
package analysistest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
)

// Outcome is what the fake does with a create.
type Outcome int

const (
	// Accept holds the batch and answers its id.
	Accept Outcome = iota
	// Reject answers an error status: nothing is held.
	Reject
	// Unknown answers no outcome (a cut connection): nothing is held.
	Unknown
	// AcceptLost holds the batch, as Anthropic would, but its answer is lost: the caller gets
	// an unknown outcome, and a later list shows the batch.
	AcceptLost
	// ServerFail answers a server error status (503): an unknown outcome; nothing is held.
	ServerFail
)

// ErrLost is the error of an Unknown or AcceptLost create.
var ErrLost = errors.New("analysistest: answer lost")

// ErrList is the error of a list when the fake is told to fail it.
var ErrList = errors.New("analysistest: list failed")

// Body is a body the fake received.
type Body struct {
	// Raw is the body, byte for byte.
	Raw []byte
	// Requests are its requests, parsed.
	Requests []Request
}

// Request is one request of a body.
type Request struct {
	CustomID string          `json:"custom_id"`
	Params   json.RawMessage `json:"params"`
}

// IDs returns the custom_id of each request, in order.
func (b Body) IDs() []string {
	ids := make([]string, len(b.Requests))
	for i, r := range b.Requests {
		ids[i] = r.CustomID
	}
	return ids
}

// Analyzer is the fake AI service. Its zero value is not usable: call New. It is safe for
// concurrent use.
type Analyzer struct {
	mu       sync.Mutex
	now      func() time.Time
	outcomes []Outcome
	bodies   []Body
	held     []analysis.BatchInfo
	listErr  bool
	block    bool
	onCreate func(ctx context.Context)
	onList   func(ctx context.Context)
	creates  int
	lists    int
	next     int
}

// New returns a fake whose batches are created at now(); every create is accepted until Script
// says otherwise.
func New(now func() time.Time) *Analyzer {
	if now == nil {
		now = time.Now
	}
	return &Analyzer{now: now}
}

// Script sets the outcomes of the next creates, in order; once they are played, creates are
// accepted.
func (a *Analyzer) Script(outcomes ...Outcome) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.outcomes = append(a.outcomes, outcomes...)
}

// FailLists makes every later list fail (fail true) or answer (fail false).
func (a *Analyzer) FailLists(fail bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listErr = fail
}

// BlockLists makes every later list wait until its context is done, as a server that never
// answers.
func (a *Analyzer) BlockLists() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.block = true
}

// OnCreate runs hook in each later create, once the body is read and before the outcome is
// played: a test acts there « during the send ».
func (a *Analyzer) OnCreate(hook func(ctx context.Context)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onCreate = hook
}

// OnList runs hook at the start of each later list.
func (a *Analyzer) OnList(hook func(ctx context.Context)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onList = hook
}

// Hold adds a batch the key holds, as if another client had created it.
func (a *Analyzer) Hold(b analysis.BatchInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held = append(a.held, b)
}

// CreateBatch records the body and plays the next outcome. The body must be valid JSON of the
// Message Batches create call, exactly size bytes long: anything else fails the call as a
// rejected batch would, so that a test sees it.
func (a *Analyzer) CreateBatch(ctx context.Context, body io.ReadSeeker, size int64) (analysis.BatchRef, error) {
	if err := ctx.Err(); err != nil {
		return analysis.BatchRef{}, err
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return analysis.BatchRef{}, fmt.Errorf("analysistest: read body: %w", err)
	}
	a.mu.Lock()
	hook := a.onCreate
	a.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.creates++
	var parsed struct {
		Requests []Request `json:"requests"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		a.bodies = append(a.bodies, Body{Raw: raw})
		return analysis.BatchRef{}, &analysis.RejectedError{Status: 400, Type: "analysistest_not_json"}
	}
	a.bodies = append(a.bodies, Body{Raw: raw, Requests: parsed.Requests})
	if int64(len(raw)) != size {
		return analysis.BatchRef{}, &analysis.RejectedError{Status: 400, Type: "analysistest_wrong_size"}
	}
	outcome := Accept
	if len(a.outcomes) > 0 {
		outcome, a.outcomes = a.outcomes[0], a.outcomes[1:]
	}
	switch outcome {
	case Reject:
		return analysis.BatchRef{}, &analysis.RejectedError{Status: 400, Type: "invalid_request_error"}
	case Unknown:
		return analysis.BatchRef{}, ErrLost
	case ServerFail:
		return analysis.BatchRef{}, fmt.Errorf("%w: %w", ErrLost, &analysis.ServerError{Status: 503})
	}
	a.next++
	ref := analysis.BatchRef{ID: fmt.Sprintf("msgbatch_fake%03d", a.next), CreatedAt: a.now().UTC()}
	a.held = append(a.held, analysis.BatchInfo{ID: ref.ID, CreatedAt: ref.CreatedAt, RequestCount: len(parsed.Requests)})
	if outcome == AcceptLost {
		return analysis.BatchRef{}, ErrLost
	}
	return ref, nil
}

// ListBatches answers the batches held, newest first, those created before since left out.
func (a *Analyzer) ListBatches(ctx context.Context, since time.Time) ([]analysis.BatchInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	hook, block := a.onList, a.block
	a.lists++
	a.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.listErr {
		return nil, ErrList
	}
	var out []analysis.BatchInfo
	for _, b := range a.held {
		if !b.CreatedAt.Before(since) {
			out = append(out, b)
		}
	}
	slices.SortStableFunc(out, func(x, y analysis.BatchInfo) int { return y.CreatedAt.Compare(x.CreatedAt) })
	return out, nil
}

// Bodies returns the bodies received so far, in order.
func (a *Analyzer) Bodies() []Body {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.bodies)
}

// Creates is the number of create calls received.
func (a *Analyzer) Creates() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.creates
}

// Lists is the number of list calls received.
func (a *Analyzer) Lists() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lists
}

// Calls is the number of calls of any kind received.
func (a *Analyzer) Calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.creates + a.lists
}
