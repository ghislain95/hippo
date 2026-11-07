package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis/schema"
	"github.com/ghislain95/hippo/backend/internal/naming"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// This file reads and applies the results of one batch of a user (apply-batch-results, design
// D2 to D8): its state is asked, its results file read one line at a time, each line settled in
// its own transaction, and the batch closed with its tokens and cost once every line was read.
// It builds no request and never calls CreateBatch (RG-12). Pass.Follow (design D1) runs it for
// every batch to follow, under the pass mutex.

const (
	// DefaultQueryTimeout bounds the state query of a batch, the SDK's retries included
	// (design D1).
	DefaultQueryTimeout = time.Minute
	// DefaultReadTimeout bounds the read and the application of a batch's results (design D1).
	DefaultReadTimeout = 10 * time.Minute
)

// Outcomes of a followed batch, logged in the field outcome of the follow-up line (design D14).
const (
	followInProgress    = "in_progress"
	followEnded         = "ended"
	followUnavailable   = "results_unavailable"
	followPollFailed    = "poll_failed"
	followReadFailed    = "read_failed"
	followNotInProgress = "not_in_progress"
)

// Outcomes of one item's result (design D4, D14), and the reason classes of an invalid response
// (design D5; the database steps answer store.Reject*). Classes only: never a value of the
// response nor a message of the validator.
const (
	itemAnalyzed = "analyzed"
	itemInvalid  = "invalid"
	itemCut      = "cut"
	itemTooLong  = "too_long"
	itemErrored  = "errored"
	itemRequeued = "requeued"
	itemDeleted  = "deleted"
	itemUnknown  = "unknown"

	reasonStopReason = "stop_reason"
	reasonToolCalls  = "tool_calls"
	reasonTooFewTags = "too_few_tags"
	reasonSchema     = "schema"
	reasonEnvelope   = "envelope_version"
)

// minTags is the number of distinct tags a response holds at least, once normalised (RG-58).
const minTags = 2

// followReport is what following one batch did: the fields of its log line (design D14).
type followReport struct {
	BatchID, AnthropicBatchID string
	Outcome                   string
	Counts                    followCounts
	// Tokens are the sums over the succeeded lines read (all of them once the batch is ended;
	// those of the most complete cut read for a batch closed as unavailable).
	Tokens  Usage
	CostUSD float64
	// Status and ErrorType are those of a failed query (poll_failed, read_failed).
	Status    int
	ErrorType string
	// Err is an error of the store or of the read, logged by its class; never a message of the
	// AI service.
	Err error
	// Items are the outcomes of the lines applied, in order, for the debug lines.
	Items []itemReport
}

// followCounts are the counts by outcome of a followed batch.
type followCounts struct {
	Analyzed, Invalid, Cut, TooLong, Errored, Requeued, Failed, Deleted, Unknown int
	UnreadableLines, WithoutResult                                               int
}

// itemReport is the outcome of one line: the item id, the outcome class, and the reason class
// of an invalid response or the API error type of an errored request.
type itemReport struct {
	ItemID, Outcome, Reason, ErrorType string
}

// followBatch follows the batch b of userID once (design D1, D8): a batch still processing is
// left as it is; an ended one is read and applied, then closed with its tokens and cost; one
// whose results the service no longer serves is closed with the tokens already read. A failed
// query changes nothing. The caller holds the pass mutex and has an Analyzer.
func (p *Pass) followBatch(ctx context.Context, userID string, b store.Batch) followReport {
	report := followReport{BatchID: b.ID}
	if b.AnthropicBatchID == nil {
		// No writer leaves an accepted batch without its Anthropic id; such a row cannot be
		// asked for, and would block the user for good (design D1, As built tranche B).
		p.closeUnavailable(ctx, userID, b, &report)
		return report
	}
	report.AnthropicBatchID = *b.AnthropicBatchID

	qctx, cancel := context.WithTimeout(ctx, p.cfg.QueryTimeout)
	state, err := p.cfg.Analyzer.GetBatch(qctx, *b.AnthropicBatchID)
	cancel()
	switch {
	case errors.Is(err, ErrResultsUnavailable):
		p.closeUnavailable(ctx, userID, b, &report)
		return report
	case err != nil:
		report.Outcome = followPollFailed
		queryFailure(&report, err)
		return report
	case state.Processing != ProcessingEnded:
		report.Outcome = followInProgress
		return report
	}

	rctx, cancel := context.WithTimeout(ctx, p.cfg.ReadTimeout)
	defer cancel()
	results, err := p.cfg.Analyzer.Results(rctx, *b.AnthropicBatchID)
	switch {
	case errors.Is(err, ErrResultsUnavailable):
		p.closeUnavailable(ctx, userID, b, &report)
		return report
	case err != nil:
		report.Outcome = followReadFailed
		queryFailure(&report, err)
		return report
	}
	defer func() { _ = results.Close() }() // read only: what was read is applied already

	lines, err := p.applyResults(rctx, userID, b, results, &report)
	if err != nil {
		// A cut read: the tokens read so far are kept when this read went further than the
		// recorded one; the next occasion reads again from the start (design D8).
		report.Outcome = followReadFailed
		if report.Err != nil {
			report.ErrorType = "store" // a write failed: report.Err is its error
		} else {
			report.Err = err
			queryFailure(&report, err)
		}
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
		defer cancel()
		if _, rerr := p.cfg.Store.RecordCutRead(wctx, userID, b.ID, batchTokens(report.Tokens), lines); rerr != nil {
			report.Err = errors.Join(err, rerr)
		}
		return report
	}

	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	closed, err := p.cfg.Store.CloseBatch(wctx, userID, b.ID, batchTokens(report.Tokens), BatchCost(report.Tokens, p.cfg.Prices), state.EndedAt)
	switch {
	case errors.Is(err, store.ErrNotFound):
		report.Outcome = followNotInProgress
	case err != nil:
		report.Outcome, report.Err = followReadFailed, err
	default:
		report.Outcome, report.CostUSD, report.Counts.WithoutResult = followEnded, closed.CostUSD, closed.Requeued
	}
	return report
}

// closeUnavailable closes the batch b of userID whose results are no longer served, with the
// tokens of the most complete cut read (decision 1, design D8).
func (p *Pass) closeUnavailable(ctx context.Context, userID string, b store.Batch, report *followReport) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	// The batch row keeps the sum of both cache-write durations: priced as 5-minute writes, the
	// only duration the requests set (design D8, As built tranche A).
	closed, err := p.cfg.Store.CloseBatchUnavailable(wctx, userID, b.ID, func(t store.BatchTokens) float64 {
		return BatchCost(usageOf(t), p.cfg.Prices)
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		report.Outcome = followNotInProgress
	case err != nil:
		report.Outcome, report.Err = followReadFailed, err
	default:
		report.Outcome, report.CostUSD = followUnavailable, closed.CostUSD
		report.Tokens, report.Counts.WithoutResult = usageOf(closed.Tokens), closed.Requeued
	}
}

// queryFailure records in report the status and the error type of a failed query, never its
// message.
func queryFailure(report *followReport, err error) {
	var query *QueryError
	switch {
	case errors.As(err, &query):
		report.Status, report.ErrorType = query.Status, query.Type
	case errors.Is(err, context.DeadlineExceeded):
		report.ErrorType = "timeout"
	case errors.Is(err, context.Canceled):
		report.ErrorType = "stopped"
	default:
		report.ErrorType = "transport"
	}
}

// applyResults reads every line of results and settles each item (design D4). It returns the
// number of lines read, and an error when the read was cut or a write failed: the batch then
// stays open. The tokens of every succeeded line are summed, whatever its outcome.
func (p *Pass) applyResults(ctx context.Context, userID string, b store.Batch, results ResultReader, report *followReport) (int, error) {
	sameEnvelope := b.EnvelopeVersion == p.cfg.Prompt.EnvelopeVersion
	lines := 0
	for {
		result, err := results.Next()
		switch {
		case errors.Is(err, io.EOF):
			return lines, nil
		case errors.Is(err, ErrUnreadableLine):
			lines++
			report.Counts.UnreadableLines++
			continue
		case err != nil:
			return lines, fmt.Errorf("read results: %w", err)
		}
		lines++
		if result.Kind == ResultSucceeded {
			report.Tokens = report.Tokens.Add(result.Usage)
		}
		item, err := p.applyResult(ctx, userID, b.ID, sameEnvelope, result)
		if err != nil {
			report.Err = err
			return lines, err
		}
		report.count(item)
		report.Items = append(report.Items, item.itemReport)
	}
}

// appliedItem is the outcome of one line and whether its item ended failed.
type appliedItem struct {
	itemReport
	failed bool
}

// count adds item to the counts of report.
func (r *followReport) count(item appliedItem) {
	c := &r.Counts
	switch item.Outcome {
	case itemAnalyzed:
		c.Analyzed++
	case itemInvalid:
		c.Invalid++
	case itemCut:
		c.Cut++
	case itemTooLong:
		c.TooLong++
	case itemErrored:
		c.Errored++
	case itemRequeued:
		c.Requeued++
	case itemDeleted:
		c.Deleted++
	default:
		c.Unknown++
	}
	if item.failed {
		c.Failed++
	}
}

// applyResult decides the outcome of one result and writes it in one store transaction (design
// D4): only while the item of userID is still submitted in batchID and not deleted.
func (p *Pass) applyResult(ctx context.Context, userID, batchID string, sameEnvelope bool, r Result) (appliedItem, error) {
	item := appliedItem{itemReport: itemReport{ItemID: r.CustomID}}
	settle := func(outcome store.Outcome, class string) (appliedItem, error) {
		settled, err := p.cfg.Store.SettleItem(ctx, userID, batchID, r.CustomID, outcome)
		if err != nil {
			return appliedItem{}, err
		}
		item.Outcome, item.failed = settledClass(settled, class), settled == store.SettledFailed
		if outcome == store.OutcomeCut && settled == store.SettledFailed {
			item.Outcome = itemTooLong
		}
		return item, nil
	}

	switch r.Kind {
	case ResultErrored:
		item.ErrorType = r.ErrorType
		return settle(store.OutcomeErrored, itemErrored)
	case ResultExpired, ResultCanceled:
		return settle(store.OutcomeRequeue, itemRequeued)
	}
	// succeeded
	if !sameEnvelope {
		// Validated against another envelope than the one it was asked with: sent again
		// without an attempt counted (design D4).
		item.Reason = reasonEnvelope
		return settle(store.OutcomeRequeue, itemRequeued)
	}
	if r.StopReason == "max_tokens" {
		return settle(store.OutcomeCut, itemCut)
	}
	a, reason := p.validate(r)
	if reason != "" {
		item.Reason = reason
		return settle(store.OutcomeInvalid, itemInvalid)
	}
	written, err := p.cfg.Store.WriteAnalysis(ctx, userID, batchID, r.CustomID, a)
	if err != nil {
		return appliedItem{}, err
	}
	item.Reason, item.failed = written.Reason, written.Settlement == store.SettledFailed
	item.Outcome = settledClass(written.Settlement, itemAnalyzed)
	if written.Reason != "" {
		item.Outcome = settledClass(written.Settlement, itemInvalid)
	}
	return item, nil
}

// settledClass is the outcome class of a settlement: unknown or deleted when nothing was
// written, class otherwise.
func settledClass(settled store.Settlement, class string) string {
	switch settled {
	case store.SettledNotSubmitted:
		return itemUnknown
	case store.SettledDeleted:
		return itemDeleted
	}
	return class
}

// validate runs the steps of the validation that need no database (design D5), in order, and
// returns the analysis to write or the reason class of the first failure. The input of the
// tool call is never logged nor stored when it fails, nor is the validator's message.
func (p *Pass) validate(r Result) (store.Analysis, string) {
	// 1. The stop reason of a forced tool call.
	if r.StopReason != "tool_use" {
		return store.Analysis{}, reasonStopReason
	}
	// 2. Exactly one call, to classify_item, whatever the text blocks.
	if len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != toolName {
		return store.Analysis{}, reasonToolCalls
	}
	input := r.ToolCalls[0].Input
	// 3. A JSON object, its tags normalised before the schema (RG-58, decision 2).
	answer, ok := decodeObject(input)
	if !ok {
		return store.Analysis{}, reasonSchema
	}
	tags, isStrings := normaliseTags(answer["tags"])
	if isStrings {
		if len(tags) < minTags {
			return store.Analysis{}, reasonTooFewTags
		}
		list := make([]any, len(tags))
		for i, tag := range tags {
			list[i] = tag
		}
		answer["tags"] = list
	}
	// 4. The bundled schema, compiled once.
	if err := p.validator.Validate(answer); err != nil {
		return store.Analysis{}, reasonSchema
	}
	// 5. The typed fields, the fiche version and the search text.
	a, ok := p.typed(input, tags)
	if !ok {
		return store.Analysis{}, reasonSchema
	}
	return a, ""
}

// decodeObject decodes raw as one JSON object, numbers kept as json.Number for the schema.
func decodeObject(raw json.RawMessage) (map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false // data after the value
	}
	object, ok := v.(map[string]any)
	return object, ok
}

// normaliseTags normalises the tags of an answer when they are an array of strings: each by
// naming.Tag, empty results dropped, duplicates dropped keeping the first (RG-58). Any other
// shape is left to the schema (isStrings false).
func normaliseTags(value any) (tags []string, isStrings bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			return nil, false
		}
		tag := naming.Tag(s)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags, true
}

// answer is the classify_item input once it passed the schema.
type answer struct {
	Type struct {
		Key      *string `json:"key"`
		Proposed *struct {
			Key    string `json:"key"`
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"proposed"`
	} `json:"type"`
	Category struct {
		ID       *int64 `json:"id"`
		Proposed *struct {
			Name        string `json:"name"`
			ParentID    *int64 `json:"parent_id"`
			Description string `json:"description"`
			Reason      string `json:"reason"`
		} `json:"proposed"`
	} `json:"category"`
	Title        string          `json:"title"`
	Fiche        json.RawMessage `json:"fiche"`
	Language     string          `json:"language"`
	Freshness    string          `json:"freshness"`
	ContentOK    bool            `json:"content_ok"`
	ContentIssue *string         `json:"content_issue"`
}

// typed decodes the fields of an input that passed the schema (design D5 step 5). The fiche is
// kept as the model wrote it, compacted, so that its search text follows its order.
func (p *Pass) typed(input json.RawMessage, tags []string) (store.Analysis, bool) {
	var in answer
	if err := json.Unmarshal(input, &in); err != nil {
		return store.Analysis{}, false
	}
	var fiche bytes.Buffer
	if err := json.Compact(&fiche, in.Fiche); err != nil {
		return store.Analysis{}, false
	}
	var head struct {
		Template string `json:"template"`
	}
	if err := json.Unmarshal(fiche.Bytes(), &head); err != nil {
		return store.Analysis{}, false
	}
	version, ok := p.validator.FicheVersion(head.Template)
	if !ok {
		return store.Analysis{}, false
	}
	text, err := SearchText(fiche.Bytes())
	if err != nil {
		return store.Analysis{}, false
	}
	a := store.Analysis{
		Template: head.Template, Tags: tags, Title: in.Title, FicheJSON: fiche.Bytes(), SearchText: text,
		Language: in.Language, Freshness: in.Freshness, ContentOK: in.ContentOK, ContentIssue: in.ContentIssue,
		SchemaVersion: version,
	}
	switch {
	case in.Type.Proposed != nil:
		a.Type.Proposed = &store.ProposedType{Key: in.Type.Proposed.Key, Name: in.Type.Proposed.Name, Reason: in.Type.Proposed.Reason}
	case in.Type.Key != nil:
		a.Type.Key = *in.Type.Key
	default:
		return store.Analysis{}, false
	}
	switch c := in.Category; {
	case c.Proposed != nil:
		a.Category.Proposed = &store.ProposedCategory{
			Name: c.Proposed.Name, ParentID: c.Proposed.ParentID, Description: c.Proposed.Description, Reason: c.Proposed.Reason,
		}
	case c.ID != nil:
		a.Category.ID = *c.ID
	default:
		return store.Analysis{}, false
	}
	return a, true
}

// batchTokens are the token sums of u as a batch row records them: both cache-write durations
// summed (design D12).
func batchTokens(u Usage) store.BatchTokens {
	return store.BatchTokens{Input: u.Input, Output: u.Output, CacheCreation: u.CacheWrite5m + u.CacheWrite1h, CacheRead: u.CacheRead}
}

// usageOf reads the token sums of a batch row, its cache writes as 5-minute ones.
func usageOf(t store.BatchTokens) Usage {
	return Usage{Input: t.Input, Output: t.Output, CacheWrite5m: t.CacheCreation, CacheRead: t.CacheRead}
}

// newValidator compiles the bundled schema once, for the pass.
func newValidator() (*schema.Validator, error) {
	v, err := schema.Compile()
	if err != nil {
		return nil, fmt.Errorf("analysis pass: %w", err)
	}
	return v, nil
}
