package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Statuses of a batch (design.md, « Statut d'un batch »).
const (
	// BatchPending: the items are submitted, the send is not confirmed.
	BatchPending = "pending"
	// BatchInProgress: Anthropic accepted the batch.
	BatchInProgress = "in_progress"
	// BatchEnded: its results are available (E3-02).
	BatchEnded = "ended"
	// BatchFailed: refused, or never arrived; its items went back to extracted.
	BatchFailed = "failed"
)

// MaxBatchItems is the number of items of a batch at most (RG-32, batches.item_count CHECK).
const MaxBatchItems = 200

// ErrAnthropicBatchIDRecorded: the Anthropic id is already the id of another batch row
// (batches.anthropic_batch_id is UNIQUE). MarkBatchSent writes nothing; the row stays pending.
var ErrAnthropicBatchIDRecorded = errors.New("anthropic batch id already recorded")

// NewBatch is what SubmitBatch records of a batch (RG-64).
type NewBatch struct {
	Model           string
	PromptVersion   int
	EnvelopeVersion int
}

// Batch is a batch row of a user.
type Batch struct {
	ID               string
	AnthropicBatchID *string
	Status           string
	Model            string
	PromptVersion    int
	EnvelopeVersion  int
	ItemCount        int
	CostUSD          *float64
	CreatedAt        time.Time
	SentAt           *time.Time
}

// HasBatchWithoutCost tells whether userID has a batch whose cost is not recorded: being sent
// (pending), in progress, or ended without a cost. Such a batch blocks the next one: the budget
// cannot be checked without its cost (RG-81, ADR-0021, design.md D3). A failed batch never
// blocks: nothing was billed.
func (s *Store) HasBatchWithoutCost(ctx context.Context, userID string) (bool, error) {
	var found bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM batches
		   WHERE user_id = ? AND (status IN ('pending', 'in_progress') OR (status = 'ended' AND cost_usd IS NULL)))`,
		userID).Scan(&found); err != nil {
		return false, fmt.Errorf("check batches without cost: %w", err)
	}
	return found, nil
}

// MonthSpend returns the recorded cost of userID's batches created in [from, to), in US
// dollars: the caller gives the bounds of the Paris calendar month. A batch counts in the month
// it was created (sent), whenever it ends (RG-81, design.md D3).
func (s *Store) MonthSpend(ctx context.Context, userID string, from, to time.Time) (float64, error) {
	var spent float64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(cost_usd), 0) FROM batches WHERE user_id = ? AND created_at >= ? AND created_at < ?`,
		userID, from.UTC().Format(TimeLayout), to.UTC().Format(TimeLayout)).Scan(&spent); err != nil {
		return 0, fmt.Errorf("read month spend: %w", err)
	}
	return spent, nil
}

// SubmitBatch moves the items ids of userID to submitted with a new pending batch, in one write
// transaction (BEGIN IMMEDIATE), and returns the batch id and the ids it moved, in the order of
// ids. Each item is read again under the transaction: only those still extracted, not private
// and not deleted move, so an item checked Privé since the selection stays extracted and is
// never sent (RG-12, design.md D7, D11). Each moved item gets its own change_seq and a new
// updated_at; its attempts are unchanged. When none can move, nothing is written and the batch
// id is empty: nothing is sent (RG-31).
func (s *Store) SubmitBatch(ctx context.Context, userID string, b NewBatch, ids []string) (batchID string, moved []string, err error) {
	if len(ids) > MaxBatchItems {
		return "", nil, fmt.Errorf("submit batch: %d items, at most %d", len(ids), MaxBatchItems)
	}
	if b.Model == "" || b.PromptVersion < 1 || b.EnvelopeVersion < 1 {
		return "", nil, errors.New("submit batch: model, prompt version and envelope version are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, fmt.Errorf("submit batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var waiting bool
		err := tx.QueryRowContext(ctx,
			`SELECT status = 'extracted' AND is_private = 0 AND deleted_at IS NULL FROM items WHERE user_id = ? AND id = ?`,
			userID, id).Scan(&waiting)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", nil, fmt.Errorf("submit batch: read item %s: %w", id, err)
		}
		if waiting {
			moved = append(moved, id)
		}
	}
	if len(moved) == 0 {
		return "", nil, nil
	}

	now := s.now().UTC()
	date := now.Format(TimeLayout)
	if batchID, err = s.newID(now); err != nil {
		return "", nil, fmt.Errorf("submit batch: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO batches (id, user_id, status, model, prompt_version, envelope_version, item_count, created_at)
		 VALUES (?, ?, 'pending', ?, ?, ?, ?, ?)`,
		batchID, userID, b.Model, b.PromptVersion, b.EnvelopeVersion, len(moved), date); err != nil {
		return "", nil, fmt.Errorf("submit batch: insert batch: %w", err)
	}
	for _, id := range moved {
		seq, err := nextChangeSeq(ctx, tx)
		if err != nil {
			return "", nil, fmt.Errorf("submit batch: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE items SET status = 'submitted', batch_id = ?, updated_at = ?, change_seq = ? WHERE user_id = ? AND id = ?`,
			batchID, date, seq, userID, id); err != nil {
			return "", nil, fmt.Errorf("submit batch: move item %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return "", nil, fmt.Errorf("submit batch: %w", err)
	}
	return batchID, moved, nil
}

// MarkBatchSent records that Anthropic accepted the pending batch batchID of userID under
// anthropicID: the batch is in progress (design.md D7, D8). It returns ErrNotFound when the
// user has no pending batch of this id (already recorded, failed, or another user's), and
// ErrAnthropicBatchIDRecorded when another row holds anthropicID; nothing is written then.
func (s *Store) MarkBatchSent(ctx context.Context, userID, batchID, anthropicID string) error {
	if anthropicID == "" {
		return errors.New("mark batch sent: empty anthropic batch id")
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE batches SET anthropic_batch_id = ?, status = 'in_progress', sent_at = ?
		 WHERE user_id = ? AND id = ? AND status = 'pending'`,
		anthropicID, s.now().UTC().Format(TimeLayout), userID, batchID)
	if err != nil {
		var violation *uniqueViolation
		if errors.As(asUniqueViolation("batches", err), &violation) {
			return fmt.Errorf("mark batch %s sent: %w", batchID, ErrAnthropicBatchIDRecorded)
		}
		return fmt.Errorf("mark batch %s sent: %w", batchID, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark batch %s sent: %w", batchID, err)
	}
	if n == 0 {
		return fmt.Errorf("mark batch %s sent: no pending batch: %w", batchID, ErrNotFound)
	}
	return nil
}

// FailBatch records that the pending batch batchID of userID was not sent (refused, or never
// arrived), in one write transaction: the batch is failed, and each of its items still
// submitted goes back to extracted without batch, with a new updated_at and change_seq, its
// attempts unchanged: no attempt is counted (RG-30, RG-35, design.md D7). It returns the
// number of items put back, or ErrNotFound when the user has no pending batch of this id.
func (s *Store) FailBatch(ctx context.Context, userID, batchID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("fail batch %s: %w", batchID, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	result, err := tx.ExecContext(ctx,
		`UPDATE batches SET status = 'failed' WHERE user_id = ? AND id = ? AND status = 'pending'`, userID, batchID)
	if err != nil {
		return 0, fmt.Errorf("fail batch %s: %w", batchID, err)
	}
	if n, err := result.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return 0, fmt.Errorf("fail batch %s: %w", batchID, err)
		}
		return 0, fmt.Errorf("fail batch %s: no pending batch: %w", batchID, ErrNotFound)
	}
	ids, err := submittedItems(ctx, tx, userID, batchID)
	if err != nil {
		return 0, fmt.Errorf("fail batch %s: %w", batchID, err)
	}
	date := s.now().UTC().Format(TimeLayout)
	for _, id := range ids {
		seq, err := nextChangeSeq(ctx, tx)
		if err != nil {
			return 0, fmt.Errorf("fail batch %s: %w", batchID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE items SET status = 'extracted', batch_id = NULL, updated_at = ?, change_seq = ?
			 WHERE user_id = ? AND id = ? AND batch_id = ? AND status = 'submitted'`,
			date, seq, userID, id, batchID); err != nil {
			return 0, fmt.Errorf("fail batch %s: put item %s back: %w", batchID, id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("fail batch %s: %w", batchID, err)
	}
	return len(ids), nil
}

// submittedItems returns the ids of the items of userID still submitted with batchID.
func submittedItems(ctx context.Context, tx *sql.Tx, userID, batchID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM items WHERE user_id = ? AND batch_id = ? AND status = 'submitted' ORDER BY id`, userID, batchID)
	if err != nil {
		return nil, fmt.Errorf("list submitted items: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list submitted items: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list submitted items: %w", err)
	}
	return ids, nil
}

// PendingBatches returns the pending batches of userID, oldest first: the ones whose send has
// an unknown outcome, to reconcile (design.md D8).
func (s *Store) PendingBatches(ctx context.Context, userID string) ([]Batch, error) {
	return s.batches(ctx, batchSelect+` AND status = 'pending'`+batchOrder, userID)
}

// Batches returns every batch of userID, oldest first.
func (s *Store) Batches(ctx context.Context, userID string) ([]Batch, error) {
	return s.batches(ctx, batchSelect+batchOrder, userID)
}

// batchSelect and batchOrder frame the constant queries of batches: the batches of a user
// (RG-90), oldest first.
const (
	batchSelect = `SELECT id, anthropic_batch_id, status, model, prompt_version, envelope_version, item_count, cost_usd,
		created_at, sent_at FROM batches WHERE user_id = ?`
	batchOrder = ` ORDER BY created_at, id`
)

// batches reads the batches of userID by query, a constant built from batchSelect and
// batchOrder.
func (s *Store) batches(ctx context.Context, query, userID string) ([]Batch, error) {
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	batches := []Batch{}
	for rows.Next() {
		var (
			b           Batch
			anthropicID sql.Null[string]
			cost        sql.Null[float64]
			createdAt   string
			sentAt      sql.Null[string]
			err         error
		)
		if err = rows.Scan(&b.ID, &anthropicID, &b.Status, &b.Model, &b.PromptVersion, &b.EnvelopeVersion,
			&b.ItemCount, &cost, &createdAt, &sentAt); err != nil {
			return nil, fmt.Errorf("list batches: %w", err)
		}
		if b.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("list batches: batch %s created_at: %w", b.ID, err)
		}
		if sentAt.Valid {
			parsed, err := parseTime(sentAt.V)
			if err != nil {
				return nil, fmt.Errorf("list batches: batch %s sent_at: %w", b.ID, err)
			}
			b.SentAt = &parsed
		}
		b.AnthropicBatchID, b.CostUSD = ptr(anthropicID), ptr(cost)
		batches = append(batches, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list batches: %w", err)
	}
	return batches, nil
}

// WaitingCount counts the items of userID waiting for the nightly batch, as AnalysisQueue
// selects them: extracted, not private, not deleted. The log line of a refused send carries it.
func (s *Store) WaitingCount(ctx context.Context, userID string) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM items WHERE user_id = ? AND status = 'extracted' AND is_private = 0 AND deleted_at IS NULL`,
		userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count waiting items: %w", err)
	}
	return count, nil
}

// DropPrivateFromBatch leaves out of the pending batch batchID of userID its items checked
// Privé since the submission, in one write transaction (RG-12, design.md D7, D11): each goes
// back to extracted without batch, with a new updated_at and change_seq, its attempts
// unchanged, and the batch's item_count becomes the number of items still submitted with it.
// When none remains, the batch is failed in the same transaction (nothing is sent). It returns
// the ids left out and the ids remaining, both in id order (the order of the submission): the
// body is built from the remaining ids, so that item_count is the number of requests sent.
// Nothing is written when no item was made private. It returns ErrNotFound when the user has no
// pending batch of this id.
func (s *Store) DropPrivateFromBatch(ctx context.Context, userID, batchID string) (dropped, remaining []string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("drop private items of batch %s: %w", batchID, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit

	var pending bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM batches WHERE user_id = ? AND id = ? AND status = 'pending')`,
		userID, batchID).Scan(&pending); err != nil {
		return nil, nil, fmt.Errorf("drop private items of batch %s: %w", batchID, err)
	}
	if !pending {
		return nil, nil, fmt.Errorf("drop private items of batch %s: no pending batch: %w", batchID, ErrNotFound)
	}
	dropped, err = queryIDs(ctx, tx,
		`SELECT id FROM items WHERE user_id = ? AND batch_id = ? AND status = 'submitted' AND is_private = 1 ORDER BY id`,
		userID, batchID)
	if err != nil {
		return nil, nil, fmt.Errorf("drop private items of batch %s: %w", batchID, err)
	}
	remaining, err = queryIDs(ctx, tx,
		`SELECT id FROM items WHERE user_id = ? AND batch_id = ? AND status = 'submitted' AND is_private = 0 ORDER BY id`,
		userID, batchID)
	if err != nil {
		return nil, nil, fmt.Errorf("drop private items of batch %s: %w", batchID, err)
	}
	if len(dropped) == 0 {
		return nil, remaining, nil
	}
	date := s.now().UTC().Format(TimeLayout)
	for _, id := range dropped {
		seq, err := nextChangeSeq(ctx, tx)
		if err != nil {
			return nil, nil, fmt.Errorf("drop private items of batch %s: %w", batchID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE items SET status = 'extracted', batch_id = NULL, updated_at = ?, change_seq = ?
			 WHERE user_id = ? AND id = ? AND batch_id = ? AND status = 'submitted'`,
			date, seq, userID, id, batchID); err != nil {
			return nil, nil, fmt.Errorf("drop private items of batch %s: item %s: %w", batchID, id, err)
		}
	}
	// item_count is between 1 and 200 (CHECK): a batch left empty keeps its count and fails.
	update := `UPDATE batches SET item_count = ? WHERE user_id = ? AND id = ? AND status = 'pending'`
	args := []any{len(remaining), userID, batchID}
	if len(remaining) == 0 {
		update = `UPDATE batches SET status = 'failed' WHERE user_id = ? AND id = ? AND status = 'pending'`
		args = args[1:]
	}
	if _, err := tx.ExecContext(ctx, update, args...); err != nil {
		return nil, nil, fmt.Errorf("drop private items of batch %s: %w", batchID, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("drop private items of batch %s: %w", batchID, err)
	}
	return dropped, remaining, nil
}

// queryIDs returns the first column of the rows of query, as strings.
func queryIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() // read only: rows.Err below reports what matters
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
