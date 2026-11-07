package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// MaxAttempts is the number of attempts after which an item is failed (RG-35).
const MaxAttempts = 3

// Codes of items.last_error (contract AnalysisError): never a message of the API or the
// validator, which may quote the request or the fiche.
const (
	// LastErrorInvalidResponse: the AI's response was rejected by the validation.
	LastErrorInvalidResponse = "invalid_response"
	// LastErrorRequestErrored: the AI service reported the request in error.
	LastErrorRequestErrored = "request_errored"
	// LastErrorTooLong: the response was cut by its output cap after its retry (RG-36).
	LastErrorTooLong = "too_long"
)

// Outcome is what a result line asks of its item when it writes no analysis (design D4).
type Outcome string

// Outcomes of SettleItem.
const (
	// OutcomeRequeue: the request was not processed (expired, canceled), or its batch was sent
	// with another envelope version: back to extracted without batch nor attempt.
	OutcomeRequeue Outcome = "requeue"
	// OutcomeCut: the response stopped at its output cap. The first time, back to extracted,
	// marked retried; when already retried, failed with too_long. No attempt either way (RG-36).
	OutcomeCut Outcome = "cut"
	// OutcomeInvalid: the response was rejected (RG-35): one attempt, invalid_response.
	OutcomeInvalid Outcome = "invalid"
	// OutcomeErrored: the service reported the request in error: one attempt, request_errored.
	OutcomeErrored Outcome = "errored"
)

// Settlement is the state SettleItem or WriteAnalysis left the item in.
type Settlement string

// Settlements of SettleItem and WriteAnalysis.
const (
	// SettledAnalyzed: the analysis was written.
	SettledAnalyzed Settlement = "analyzed"
	// SettledExtracted: back in the queue of the nightly batch, without batch.
	SettledExtracted Settlement = "extracted"
	// SettledFailed: failed, its batch kept for traceability.
	SettledFailed Settlement = "failed"
	// SettledNotSubmitted: the item of this user is not submitted in this batch (unknown, another
	// user's, settled already, or submitted with another batch): nothing was written.
	SettledNotSubmitted Settlement = "not_submitted"
	// SettledDeleted: the item was deleted after its batch left: nothing was written (RG-79).
	SettledDeleted Settlement = "deleted"
)

// Reasons WriteAnalysis rejects a response for, at the validation steps that need the database
// (design D6). They are classes for the logs, never stored.
const (
	// RejectUnknownType: the chosen type does not exist, or a proposed key is held only by a
	// deleted type.
	RejectUnknownType = "unknown_type"
	// RejectTemplate: the fiche's template does not match the type.
	RejectTemplate = "template"
	// RejectUnknownCategory: the chosen category is not a non-deleted category of the user.
	RejectUnknownCategory = "unknown_category"
)

// genericTemplate is the template of a type without its own (RG-57).
const genericTemplate = "generic"

// Fields of items.user_edited (RG-60, contract CorrectedField).
const (
	editedContentType = "content_type"
	editedCategory    = "category"
	editedTags        = "tags"
)

// TypeChoice is the type of a response: an existing type by its key, or a proposal.
type TypeChoice struct {
	Key      string        // an existing type, when Proposed is nil
	Proposed *ProposedType // a proposal (RG-57)
}

// ProposedType is a type the AI proposes.
type ProposedType struct {
	Key, Name, Reason string
}

// CategoryChoice is the category of a response: an existing category of the user by its id, or
// a proposal.
type CategoryChoice struct {
	ID       int64             // an existing category, when Proposed is nil
	Proposed *ProposedCategory // a proposal (RG-53)
}

// ProposedCategory is a category the AI proposes. ParentID is corrected, never a reason to
// reject (RG-55).
type ProposedCategory struct {
	Name        string
	ParentID    *int64 // nil for a root
	Description string
	Reason      string
}

// Analysis is a response that passed the validation steps outside the database (design D5).
type Analysis struct {
	Type          TypeChoice
	Template      string // the template of the fiche
	Category      CategoryChoice
	Tags          []string // normalised and distinct (RG-58)
	Title         string
	FicheJSON     json.RawMessage
	SearchText    string // the fiche flattened (ADR-0007)
	Language      string
	Freshness     string
	ContentOK     bool
	ContentIssue  *string
	SchemaVersion int // of the fiche's template (RG-64)
}

// AnalysisWrite is what WriteAnalysis did.
type AnalysisWrite struct {
	Settlement Settlement
	// Reason is set when a database step rejected the response (Reject*): the attempt was
	// counted, nothing else written.
	Reason string
}

// submittedItem is the state of an item read under the write transaction of its result.
type submittedItem struct {
	attempts   int64
	retried    bool
	typeID     sql.Null[int64]
	categoryID sql.Null[int64]
	userEdited []string
}

// lockSubmitted reads the item itemID of userID inside tx and tells whether it is still
// submitted in batchID and not deleted: the idempotence key of a result (design D4). A
// settlement other than "" means nothing is to be written.
func lockSubmitted(ctx context.Context, tx *sql.Tx, userID, batchID, itemID string) (submittedItem, Settlement, error) {
	var (
		item             submittedItem
		inBatch, deleted bool
		userEdited       string
	)
	err := tx.QueryRowContext(ctx,
		`SELECT status = 'submitted' AND batch_id IS ?, deleted_at IS NOT NULL, attempts, incomplete_retried, type_id,
		   category_id, user_edited
		 FROM items WHERE user_id = ? AND id = ?`, batchID, userID, itemID).Scan(
		&inBatch, &deleted, &item.attempts, &item.retried, &item.typeID, &item.categoryID, &userEdited)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return submittedItem{}, SettledNotSubmitted, nil
	case err != nil:
		return submittedItem{}, "", fmt.Errorf("read item %s: %w", itemID, err)
	case !inBatch:
		return submittedItem{}, SettledNotSubmitted, nil
	case deleted:
		return submittedItem{}, SettledDeleted, nil
	}
	if err := json.Unmarshal([]byte(userEdited), &item.userEdited); err != nil {
		return submittedItem{}, "", fmt.Errorf("read item %s: user_edited: %w", itemID, err)
	}
	return item, "", nil
}

// SettleItem writes the outcome of the result of the item itemID of userID in its batch
// batchID, when it writes no analysis (design D4), in one write transaction: only while the
// item is still submitted in that batch and not deleted, re-read under the transaction, so
// that a line applied before a stop, or repeated, writes nothing. Each write advances
// updated_at and change_seq. An extracted item has no batch; a failed one keeps it.
func (s *Store) SettleItem(ctx context.Context, userID, batchID, itemID string, outcome Outcome) (Settlement, error) {
	switch outcome {
	case OutcomeRequeue, OutcomeCut, OutcomeInvalid, OutcomeErrored:
	default:
		return "", fmt.Errorf("settle item %s: unknown outcome %q", itemID, outcome)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("settle item %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	item, skipped, err := lockSubmitted(ctx, tx, userID, batchID, itemID)
	if err != nil || skipped != "" {
		return skipped, err
	}
	settled, err := s.settle(ctx, tx, userID, batchID, itemID, item, outcome)
	if err != nil {
		return "", fmt.Errorf("settle item %s: %w", itemID, err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("settle item %s: %w", itemID, err)
	}
	return settled, nil
}

// settle writes outcome on the submitted item inside tx.
func (s *Store) settle(ctx context.Context, tx *sql.Tx, userID, batchID, itemID string, item submittedItem, outcome Outcome) (Settlement, error) {
	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return "", err
	}
	date := s.now().UTC().Format(TimeLayout)
	const where = ` WHERE user_id = ? AND id = ? AND batch_id = ? AND status = 'submitted'`
	key := []any{userID, itemID, batchID}
	var (
		result  sql.Result
		settled = SettledExtracted
	)
	switch {
	case outcome == OutcomeRequeue:
		result, err = tx.ExecContext(ctx,
			`UPDATE items SET status = 'extracted', batch_id = NULL, updated_at = ?, change_seq = ?`+where,
			append([]any{date, seq}, key...)...)
	case outcome == OutcomeCut && item.retried:
		settled = SettledFailed
		result, err = tx.ExecContext(ctx,
			`UPDATE items SET status = 'failed', last_error = ?, updated_at = ?, change_seq = ?`+where,
			append([]any{LastErrorTooLong, date, seq}, key...)...)
	case outcome == OutcomeCut:
		result, err = tx.ExecContext(ctx,
			`UPDATE items SET status = 'extracted', batch_id = NULL, incomplete_retried = 1, updated_at = ?, change_seq = ?`+where,
			append([]any{date, seq}, key...)...)
	default: // OutcomeInvalid, OutcomeErrored: one attempt (RG-35)
		code := LastErrorInvalidResponse
		if outcome == OutcomeErrored {
			code = LastErrorRequestErrored
		}
		if item.attempts+1 >= MaxAttempts {
			settled = SettledFailed
			result, err = tx.ExecContext(ctx,
				`UPDATE items SET status = 'failed', attempts = attempts + 1, last_error = ?, updated_at = ?, change_seq = ?`+where,
				append([]any{code, date, seq}, key...)...)
		} else {
			result, err = tx.ExecContext(ctx,
				`UPDATE items SET status = 'extracted', batch_id = NULL, attempts = attempts + 1, last_error = ?, updated_at = ?,
				   change_seq = ?`+where,
				append([]any{code, date, seq}, key...)...)
		}
	}
	if err != nil {
		return "", fmt.Errorf("write outcome %s: %w", outcome, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("write outcome %s: %w", outcome, err)
	}
	if n != 1 {
		// lockSubmitted read the item submitted in this transaction: a programming error.
		return "", fmt.Errorf("write outcome %s: %d rows written, want 1", outcome, n)
	}
	return settled, nil
}

// WriteAnalysis writes a valid response of the item itemID of userID in its batch batchID, in
// one write transaction (design D6), under the same check as SettleItem. The steps of the
// validation that need the database run first and write nothing: the type exists (or is
// proposed), the template matches it, the category is a non-deleted one of the user. A failure
// counts one attempt as an invalid response, in the same transaction, and writes nothing else:
// no proposal, no tag (RG-35). Otherwise, in order: the proposed type is created or reused, the
// proposed category's parent corrected and its normalised name matched or created (RG-53 to
// RG-57, RG-59), the tags reused or created (RG-58), each new row with its own change_seq;
// nothing existing is changed (RG-56). A field of user_edited keeps its value and proposes
// nothing (RG-61). The item gets status analyzed, the model, prompt and envelope versions of
// its batch row (RG-64), and its failure counters reset.
func (s *Store) WriteAnalysis(ctx context.Context, userID, batchID, itemID string, a Analysis) (AnalysisWrite, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AnalysisWrite{}, fmt.Errorf("write analysis of item %s: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	written, err := s.writeAnalysis(ctx, tx, userID, batchID, itemID, a)
	if err != nil {
		return AnalysisWrite{}, fmt.Errorf("write analysis of item %s: %w", itemID, err)
	}
	if err := tx.Commit(); err != nil {
		return AnalysisWrite{}, fmt.Errorf("write analysis of item %s: %w", itemID, err)
	}
	return written, nil
}

func (s *Store) writeAnalysis(ctx context.Context, tx *sql.Tx, userID, batchID, itemID string, a Analysis) (AnalysisWrite, error) {
	item, skipped, err := lockSubmitted(ctx, tx, userID, batchID, itemID)
	if err != nil || skipped != "" {
		return AnalysisWrite{Settlement: skipped}, err
	}
	var (
		model                          string
		promptVersion, envelopeVersion int64
	)
	if err := tx.QueryRowContext(ctx,
		`SELECT model, prompt_version, envelope_version FROM batches WHERE user_id = ? AND id = ?`, userID, batchID).Scan(
		&model, &promptVersion, &envelopeVersion); err != nil {
		return AnalysisWrite{}, fmt.Errorf("read batch %s: %w", batchID, err)
	}

	// Validation steps that read the database; nothing is written before they all pass.
	chosenType, reason, err := checkType(ctx, tx, a)
	if err == nil && reason == "" {
		reason, err = checkCategory(ctx, tx, userID, a.Category)
	}
	if err != nil {
		return AnalysisWrite{}, err
	}
	if reason != "" {
		settled, err := s.settle(ctx, tx, userID, batchID, itemID, item, OutcomeInvalid)
		return AnalysisWrite{Settlement: settled, Reason: reason}, err
	}

	date := s.now().UTC().Format(TimeLayout)
	typeID, categoryID := item.typeID, item.categoryID
	if !slices.Contains(item.userEdited, editedContentType) {
		if chosenType == 0 {
			if chosenType, err = insertProposedType(ctx, tx, *a.Type.Proposed, date); err != nil {
				return AnalysisWrite{}, err
			}
		}
		typeID = sql.Null[int64]{V: chosenType, Valid: true}
	}
	if !slices.Contains(item.userEdited, editedCategory) {
		id := a.Category.ID
		if a.Category.Proposed != nil {
			if id, err = resolveProposedCategory(ctx, tx, userID, *a.Category.Proposed, date); err != nil {
				return AnalysisWrite{}, err
			}
		}
		categoryID = sql.Null[int64]{V: id, Valid: true}
	}
	if !slices.Contains(item.userEdited, editedTags) {
		tagIDs := make([]int64, 0, len(a.Tags))
		for _, name := range a.Tags {
			id, err := resolveTag(ctx, tx, userID, name, date)
			if err != nil {
				return AnalysisWrite{}, err
			}
			tagIDs = append(tagIDs, id)
		}
		if err := replaceItemTags(ctx, tx, userID, itemID, tagIDs); err != nil {
			return AnalysisWrite{}, err
		}
	}

	seq, err := nextChangeSeq(ctx, tx)
	if err != nil {
		return AnalysisWrite{}, err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE items SET status = 'analyzed', type_id = ?, category_id = ?, title = ?, fiche_json = ?, search_text = ?,
		   language = ?, freshness = ?, content_ok = ?, content_issue = ?, model = ?, prompt_version = ?,
		   envelope_version = ?, schema_version = ?, analyzed_at = ?, updated_at = ?, attempts = 0, last_error = NULL,
		   incomplete_retried = 0, change_seq = ?
		 WHERE user_id = ? AND id = ? AND batch_id = ? AND status = 'submitted'`,
		typeID, categoryID, a.Title, string(a.FicheJSON), a.SearchText, a.Language, a.Freshness, boolInt(a.ContentOK),
		a.ContentIssue, model, promptVersion, envelopeVersion, a.SchemaVersion, date, date, seq, userID, itemID, batchID)
	if err != nil {
		return AnalysisWrite{}, fmt.Errorf("write item: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return AnalysisWrite{}, fmt.Errorf("write item: %w", err)
	}
	if n != 1 {
		// lockSubmitted read the item submitted in this transaction: a programming error.
		return AnalysisWrite{}, fmt.Errorf("write item: %d rows written, want 1", n)
	}
	return AnalysisWrite{Settlement: SettledAnalyzed}, nil
}

// checkType runs the type and template steps of the validation (design D6 steps 1 and 2). It
// returns the id of the type to file the item under, 0 for a proposed type to create, or a
// rejection reason.
func checkType(ctx context.Context, tx *sql.Tx, a Analysis) (int64, string, error) {
	key := a.Type.Key
	if a.Type.Proposed != nil {
		key = a.Type.Proposed.Key
	}
	var (
		id                   int64
		hasTemplate, deleted bool
	)
	err := tx.QueryRowContext(ctx,
		`SELECT id, has_template, deleted_at IS NOT NULL FROM content_types WHERE key = ?`, key).Scan(&id, &hasTemplate, &deleted)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, "", fmt.Errorf("read content type: %w", err)
	}
	// A key held only by a deleted (merged) type cannot exist before E4 merges types: refused
	// meanwhile (design, Open Questions).
	if (found && deleted) || (!found && a.Type.Proposed == nil) {
		return 0, RejectUnknownType, nil
	}
	want := genericTemplate
	if a.Type.Proposed == nil && hasTemplate {
		want = key
	}
	if a.Template != want {
		return 0, RejectTemplate, nil
	}
	return id, "", nil // id is 0 when not found: a proposed type to create
}

// checkCategory runs the category step of the validation (design D6 step 3): a chosen id must
// be a non-deleted category of userID. A proposal is never rejected (RG-55).
func checkCategory(ctx context.Context, tx *sql.Tx, userID string, c CategoryChoice) (string, error) {
	if c.Proposed != nil {
		return "", nil
	}
	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM categories WHERE id = ? AND user_id = ? AND deleted_at IS NULL)`, c.ID, userID).Scan(&exists); err != nil {
		return "", fmt.Errorf("read category: %w", err)
	}
	if !exists {
		return RejectUnknownCategory, nil
	}
	return "", nil
}

// BatchTokens are the token sums of a batch's succeeded lines, as batches records them (RG-80).
// CacheCreation is the sum of both cache-write durations; the cost is computed from the split
// before it is summed (design D12).
type BatchTokens struct {
	Input, Output, CacheCreation, CacheRead int64
}

// ClosedBatch is what a closing wrote.
type ClosedBatch struct {
	Requeued int // items still submitted, back to extracted without attempt
	Tokens   BatchTokens
	CostUSD  float64
}

// openBatch is the condition of a batch whose results may still be applied: accepted by
// Anthropic and without a recorded cost.
const openBatch = `status IN ('in_progress', 'ended') AND cost_usd IS NULL`

// BatchesToFollow returns the batches of userID to follow (design D1): in progress, or ended
// without a recorded cost (an older binary, or storetest), oldest first.
func (s *Store) BatchesToFollow(ctx context.Context, userID string) ([]Batch, error) {
	return s.batches(ctx, batchSelect+` AND `+openBatch+batchOrder, userID)
}

// RecordCutRead records the token sums of a read of the results of the batch batchID of userID
// that was cut after lines lines, when it went further than the recorded one, so that a batch
// whose results disappear afterwards keeps the tokens read (decision 1, design D8). The batch
// stays without cost. It tells whether it wrote.
func (s *Store) RecordCutRead(ctx context.Context, userID, batchID string, tokens BatchTokens, lines int) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE batches SET input_tokens = ?, output_tokens = ?, cache_creation_input_tokens = ?, cache_read_input_tokens = ?,
		   results_lines_read = ?
		 WHERE user_id = ? AND id = ? AND `+openBatch+` AND (results_lines_read IS NULL OR results_lines_read < ?)`,
		tokens.Input, tokens.Output, tokens.CacheCreation, tokens.CacheRead, lines, userID, batchID, lines)
	if err != nil {
		return false, fmt.Errorf("record cut read of batch %s: %w", batchID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("record cut read of batch %s: %w", batchID, err)
	}
	return n == 1, nil
}

// CloseBatch ends the batch batchID of userID once every line of its results was read (design
// D8), in one write transaction: every item still submitted with it and not deleted goes back
// to extracted without batch, its attempts unchanged, with a new updated_at and change_seq
// (no readable result: RG-35); the batch gets the token sums of this full read, its cost, and
// ended_at (endedAt, or now when zero). It returns ErrNotFound when the user has no batch of
// this id in progress or ended without cost: a batch closed already is never written again,
// so its cost is written once.
func (s *Store) CloseBatch(ctx context.Context, userID, batchID string, tokens BatchTokens, costUSD float64, endedAt time.Time) (ClosedBatch, error) {
	return s.closeBatch(ctx, userID, batchID, func(*sql.Tx, time.Time) (BatchTokens, float64, *time.Time, error) {
		return tokens, costUSD, nil, nil
	}, endedAt)
}

// CloseBatchUnavailable ends the batch batchID of userID whose results Anthropic no longer
// serves (not found, archived; decision 1, design D8), as CloseBatch does, with the token sums
// of the most complete cut read recorded (zeros when none), their cost as cost computes it, and
// results_unavailable_at and ended_at now.
func (s *Store) CloseBatchUnavailable(ctx context.Context, userID, batchID string, cost func(BatchTokens) float64) (ClosedBatch, error) {
	return s.closeBatch(ctx, userID, batchID, func(tx *sql.Tx, now time.Time) (BatchTokens, float64, *time.Time, error) {
		var tokens BatchTokens
		err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(input_tokens, 0), COALESCE(output_tokens, 0), COALESCE(cache_creation_input_tokens, 0),
			   COALESCE(cache_read_input_tokens, 0)
			 FROM batches WHERE user_id = ? AND id = ? AND `+openBatch, userID, batchID).Scan(
			&tokens.Input, &tokens.Output, &tokens.CacheCreation, &tokens.CacheRead)
		if errors.Is(err, sql.ErrNoRows) {
			return BatchTokens{}, 0, nil, ErrNotFound
		}
		if err != nil {
			return BatchTokens{}, 0, nil, err
		}
		return tokens, cost(tokens), &now, nil
	}, time.Time{})
}

// closeBatch runs a closing: totals gives the tokens, the cost and results_unavailable_at.
func (s *Store) closeBatch(ctx context.Context, userID, batchID string,
	totals func(tx *sql.Tx, now time.Time) (BatchTokens, float64, *time.Time, error), endedAt time.Time,
) (ClosedBatch, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ClosedBatch{}, fmt.Errorf("close batch %s: %w", batchID, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	now := s.now().UTC()
	if endedAt.IsZero() {
		endedAt = now
	}
	tokens, cost, unavailableAt, err := totals(tx, now)
	if errors.Is(err, ErrNotFound) {
		return ClosedBatch{}, fmt.Errorf("close batch %s: no batch to close: %w", batchID, ErrNotFound)
	}
	if err != nil {
		return ClosedBatch{}, fmt.Errorf("close batch %s: %w", batchID, err)
	}
	var unavailable *string
	if unavailableAt != nil {
		date := unavailableAt.Format(TimeLayout)
		unavailable = &date
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE batches SET status = 'ended', input_tokens = ?, output_tokens = ?, cache_creation_input_tokens = ?,
		   cache_read_input_tokens = ?, cost_usd = ?, ended_at = ?, results_unavailable_at = ?
		 WHERE user_id = ? AND id = ? AND `+openBatch,
		tokens.Input, tokens.Output, tokens.CacheCreation, tokens.CacheRead, cost, endedAt.UTC().Format(TimeLayout), unavailable,
		userID, batchID)
	if err != nil {
		return ClosedBatch{}, fmt.Errorf("close batch %s: %w", batchID, err)
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return ClosedBatch{}, fmt.Errorf("close batch %s: %w", batchID, err)
		}
		return ClosedBatch{}, fmt.Errorf("close batch %s: no batch to close: %w", batchID, ErrNotFound)
	}
	ids, err := queryIDs(ctx, tx,
		`SELECT id FROM items WHERE user_id = ? AND batch_id = ? AND status = 'submitted' AND deleted_at IS NULL ORDER BY id`,
		userID, batchID)
	if err != nil {
		return ClosedBatch{}, fmt.Errorf("close batch %s: %w", batchID, err)
	}
	date := now.Format(TimeLayout)
	for _, id := range ids {
		seq, err := nextChangeSeq(ctx, tx)
		if err != nil {
			return ClosedBatch{}, fmt.Errorf("close batch %s: %w", batchID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE items SET status = 'extracted', batch_id = NULL, updated_at = ?, change_seq = ?
			 WHERE user_id = ? AND id = ? AND batch_id = ? AND status = 'submitted'`,
			date, seq, userID, id, batchID); err != nil {
			return ClosedBatch{}, fmt.Errorf("close batch %s: put item %s back: %w", batchID, id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return ClosedBatch{}, fmt.Errorf("close batch %s: %w", batchID, err)
	}
	return ClosedBatch{Requeued: len(ids), Tokens: tokens, CostUSD: cost}, nil
}
