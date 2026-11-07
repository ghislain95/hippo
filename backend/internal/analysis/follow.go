package analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ghislain95/hippo/backend/internal/schedule"
)

// FollowInterval is the period of the follow-up of the batches in progress between two nightly
// passes (RG-30, apply-batch-results design D1).
const FollowInterval = 15 * time.Minute

// Follow follows the batches of every user (design D1): each batch in progress, or ended
// without a recorded cost, is asked for and, once ended, read and applied (results.go). It
// holds the pass mutex, like Run and Reconcile: a follow-up never runs while a nightly send or
// a reconciliation is in flight. Without an Analyzer it returns at once and logs nothing. A
// user's failure is logged and does not stop the others; Follow returns an error only when the
// users cannot be listed or ctx is done. It never builds nor sends a request (RG-12).
func (p *Pass) Follow(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.Analyzer == nil {
		return nil
	}
	users, err := p.cfg.Store.UserIDs(ctx)
	if err != nil {
		return fmt.Errorf("follow batches: %w", err)
	}
	for _, userID := range users {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("follow batches: %w", err)
		}
		if err := p.followUser(ctx, userID); err != nil && ctx.Err() == nil {
			p.cfg.Logger.Error("follow batches", slog.String("user_id", userID), slog.String("error", err.Error()))
		}
	}
	return nil
}

// followUser follows the batches of userID, oldest first, and writes one line per batch
// (design D14). Nothing to follow makes no call. The caller holds the pass mutex and has an
// Analyzer.
func (p *Pass) followUser(ctx context.Context, userID string) error {
	batches, err := p.cfg.Store.BatchesToFollow(ctx, userID)
	if err != nil {
		return err
	}
	for _, b := range batches {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := p.cfg.Now()
		report := p.followBatch(ctx, userID, b)
		p.logFollow(ctx, userID, report, p.cfg.Now().Sub(start))
	}
	return nil
}

// logFollow writes the line of a followed batch and, at debug level, one line per item (design
// D14): ids, counts, tokens, cost, outcome and reason classes, error types; never a value of a
// response, nor a message of the AI service or of the validator.
func (p *Pass) logFollow(ctx context.Context, userID string, r followReport, took time.Duration) {
	level := slog.LevelInfo
	switch {
	case r.ErrorType == errorTypeStore:
		level = slog.LevelError
	case r.Outcome == followPollFailed, r.Outcome == followReadFailed, r.Outcome == followUnavailable, r.Outcome == followNotInProgress:
		level = slog.LevelWarn
	}
	c := r.Counts
	attrs := []slog.Attr{
		slog.String("user_id", userID), slog.String("batch_id", r.BatchID), slog.String("outcome", r.Outcome),
		slog.Int("analyzed", c.Analyzed), slog.Int("invalid", c.Invalid), slog.Int("cut", c.Cut), slog.Int("too_long", c.TooLong),
		slog.Int("errored", c.Errored), slog.Int("requeued", c.Requeued), slog.Int("failed", c.Failed),
		slog.Int("deleted", c.Deleted), slog.Int("unknown", c.Unknown), slog.Int("unreadable_lines", c.UnreadableLines),
		slog.Int("without_result", c.WithoutResult),
		slog.Int64("input_tokens", r.Tokens.Input), slog.Int64("output_tokens", r.Tokens.Output),
		slog.Int64("cache_creation_input_tokens", r.Tokens.CacheWrite5m+r.Tokens.CacheWrite1h),
		slog.Int64("cache_read_input_tokens", r.Tokens.CacheRead),
		slog.Float64("cost_usd", r.CostUSD), slog.Int64("duration_ms", took.Milliseconds()),
	}
	if r.AnthropicBatchID != "" {
		attrs = append(attrs, slog.String("anthropic_batch_id", r.AnthropicBatchID))
	}
	if r.Outcome == followPollFailed || r.Outcome == followReadFailed {
		if r.Status != 0 {
			attrs = append(attrs, slog.Int("status", r.Status))
		}
		if r.ErrorType != "" {
			attrs = append(attrs, slog.String("error_type", r.ErrorType))
		}
	}
	if r.Err != nil && r.ErrorType == errorTypeStore {
		// An error of the store names ids and columns at most; an error of the read is reduced
		// to its class (ErrorType) and never logged.
		attrs = append(attrs, slog.String("error", r.Err.Error()))
	}
	p.cfg.Logger.LogAttrs(ctx, level, "analysis follow-up", attrs...)
	if r.RecordErr != nil {
		// The tokens of the cut read are not recorded: logged on its own line, the store's
		// error only, so that the read's error class above stays the read's.
		p.cfg.Logger.LogAttrs(ctx, slog.LevelError, "analysis follow-up: record the cut read",
			slog.String("user_id", userID), slog.String("batch_id", r.BatchID),
			slog.String("error_type", errorTypeStore), slog.String("error", r.RecordErr.Error()))
	}

	for _, item := range r.Items {
		itemLevel := slog.LevelDebug
		attrs := []slog.Attr{
			slog.String("user_id", userID), slog.String("batch_id", r.BatchID), slog.String("item_id", item.ItemID),
			slog.String("outcome", item.Outcome),
		}
		if item.Reason != "" {
			attrs = append(attrs, slog.String("reason", item.Reason))
		}
		if item.ErrorType != "" {
			attrs = append(attrs, slog.String("error_type", item.ErrorType))
		}
		if item.Err != nil {
			itemLevel = slog.LevelError
			attrs = append(attrs, slog.String("error", item.Err.Error()))
		}
		p.cfg.Logger.LogAttrs(ctx, itemLevel, "analysis follow-up item", attrs...)
	}
}

// RunFollowUp follows the batches of pass every interval until ctx is done (design D1): it
// waits on clock, then calls pass.Follow. A tick that comes while the pass is busy waits on its
// mutex; ticks are not queued, the next wait starts once Follow has returned. logger nil
// discards the lines.
func RunFollowUp(ctx context.Context, pass *Pass, clock schedule.Clock, interval time.Duration, logger *slog.Logger) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-clock.After(interval):
		}
		if err := pass.Follow(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("follow batches", slog.String("error", err.Error()))
		}
	}
}
