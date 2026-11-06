package analysis

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/store"
)

const (
	// DefaultSendTimeout bounds the send of a batch: 200 MiB at about 3 Mbit/s up (design.md D7).
	DefaultSendTimeout = 10 * time.Minute
	// DefaultListTimeout bounds the list of the reconciliation, retries included: a stalled
	// list must not hold the pass, nor the schedule, which runs it synchronously (design.md D8).
	DefaultListTimeout = 2 * time.Minute
	// writeTimeout bounds the writes that record the outcome of a send, made even when the
	// pass is stopped, so that an accepted batch is recorded (design.md D7).
	writeTimeout = 10 * time.Second
	// matchBefore and matchAfter bound the window in which a listed batch can be the one a
	// pending row sent: the NAS clock may be ahead, and the send lasts up to its timeout
	// (design.md D8).
	matchBefore = 10 * time.Minute
	matchAfter  = 20 * time.Minute
)

// Outcomes of a user's pass and of a reconciliation, logged in the field outcome (design.md
// D3, D8, D17). Only these classes reach a log: never an item content, a body nor an error of
// the AI service.
const (
	outcomeSent          = "sent"
	outcomeNothing       = "nothing_to_send"
	outcomeNoAPIKey      = "no_api_key"
	outcomeCostPending   = "cost_pending"
	outcomeBudget        = "budget_reached"
	outcomeCopier        = "copier_unavailable"
	outcomeStorage       = "storage_unavailable"
	outcomePrivate       = "private_item"
	outcomeRejected      = "rejected"
	outcomeUnknown       = "send_unknown"
	outcomeStopped       = "stopped"
	outcomeFailed        = "failed"
	outcomeAdopted       = "adopted"
	outcomeDoubleSend    = "possible_double_send"
	outcomeListFailed    = "list_failed"
	outcomeIDRecorded    = "anthropic_id_recorded"
	outcomeRecordFailed  = "record_failed"
	outcomeAlreadyClosed = "not_pending"
	outcomePrivateAfter  = "private_after_submit"
)

// PassConfig is what the nightly pass needs.
type PassConfig struct {
	Store *store.Store
	// Analyzer is the AI service; nil when no ANTHROPIC_API_KEY is configured: nothing is sent
	// and nothing is reconciled (RG-81, design.md D3).
	Analyzer Analyzer
	// Model is the configured model (ANALYSIS_MODEL).
	Model string
	// Prompt is the fixed part of the prompt and the classify_item tool (prompt.Load).
	Prompt prompt.Prompt
	// BudgetUSD is MONTHLY_BUDGET_USD: no batch is sent once the month's recorded spend reaches
	// it (RG-81).
	BudgetUSD float64
	// Location is Europe/Paris: the month of the budget is a Paris calendar month.
	Location *time.Location
	// Now is the clock; nil means time.Now.
	Now    func() time.Time
	Assets AssetOpener
	Copier ImageCopier
	Temp   TempFiles
	// Logger receives the lines of the pass; nil discards them.
	Logger *slog.Logger
	// BodyLimit bounds the body of a batch; zero or less means MaxBodySize. Tests lower it.
	BodyLimit int64
	// SendTimeout bounds the send of a batch; zero or less means DefaultSendTimeout.
	SendTimeout time.Duration
	// ListTimeout bounds the list of a reconciliation; zero or less means DefaultListTimeout.
	ListTimeout time.Duration

	// selected, beforeSubmit, afterSubmit and beforeRewrite are test hooks (export_test.go): the first replaces
	// the selection handed to the request building, the second runs between the request
	// building and the submission transaction, the third right after that transaction.
	selected     func(userID string, queue []store.AnalysisQueueItem) []store.AnalysisQueueItem
	beforeSubmit func(ctx context.Context, userID string, ids []string)
	afterSubmit  func(ctx context.Context, userID, batchID string)
	// beforeRewrite runs before each rewrite of the body after items were left out; its error
	// fails the rewrite (test hook).
	beforeRewrite func(ctx context.Context, userID, batchID string) error
}

// Pass is the nightly analysis pass (RG-30 to RG-32, RG-81, design.md D2, D3, D7, D8): for
// each user, it reconciles the batches whose send has an unknown outcome, checks the guards,
// selects the waiting items, writes their requests, submits them and sends one batch, once.
// Run and Reconcile exclude each other: a reconciliation at start-up never settles a batch
// whose send is in flight.
type Pass struct {
	cfg PassConfig
	mu  sync.Mutex
}

// NewPass checks cfg and returns the pass. A nil Analyzer is allowed (no API key).
func NewPass(cfg PassConfig) (*Pass, error) {
	switch {
	case cfg.Store == nil || cfg.Location == nil:
		return nil, errors.New("analysis pass: a store and a location are required")
	case cfg.Model == "" || cfg.Prompt.Version < 1 || cfg.Prompt.EnvelopeVersion < 1:
		return nil, errors.New("analysis pass: the model and the prompt are required")
	case cfg.Assets == nil || cfg.Copier == nil || cfg.Temp == nil:
		return nil, errors.New("analysis pass: assets, copier and temporary files are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = DefaultSendTimeout
	}
	if cfg.ListTimeout <= 0 {
		cfg.ListTimeout = DefaultListTimeout
	}
	return &Pass{cfg: cfg}, nil
}

// Run runs the pass for every user, one after the other. A user's failure is logged and does
// not stop the others; Run returns an error only when the users cannot be listed or ctx is
// done.
func (p *Pass) Run(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.Analyzer == nil {
		// Guard 1 (design.md D3): logged once per pass; no reconciliation either.
		p.cfg.Logger.Warn("analysis pass: nothing sent", slog.String("outcome", outcomeNoAPIKey))
		return nil
	}
	users, err := p.cfg.Store.UserIDs(ctx)
	if err != nil {
		return fmt.Errorf("analysis pass: %w", err)
	}
	for _, userID := range users {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("analysis pass: %w", err)
		}
		p.runUser(ctx, userID)
	}
	return nil
}

// Reconcile reconciles the pending batches of every user (design.md D8), as the backend does
// at start-up. Nothing is done without an Analyzer.
func (p *Pass) Reconcile(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.Analyzer == nil {
		return nil
	}
	users, err := p.cfg.Store.UserIDs(ctx)
	if err != nil {
		return fmt.Errorf("reconcile batches: %w", err)
	}
	for _, userID := range users {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("reconcile batches: %w", err)
		}
		if err := p.reconcile(ctx, userID); err != nil {
			p.cfg.Logger.Error("reconcile batches", slog.String("user_id", userID), slog.String("error", err.Error()))
		}
	}
	return nil
}

// userPass is the log line of one user's pass (design.md D17): ids, counts, sizes, durations
// and the outcome class only.
type userPass struct {
	userID, outcome           string
	selected, submitted       int
	imagesWithoutCopy         int
	bodyBytes                 int64
	batchID, anthropicBatchID string
	status                    int    // HTTP status of a refused send or of a server error
	errorType                 string // API error type of that answer
	err                       error  // logged by its class only, see log
	level                     slog.Level
}

// runUser runs the pass of userID and writes its log line.
func (p *Pass) runUser(ctx context.Context, userID string) {
	start := p.cfg.Now()
	line := &userPass{userID: userID, level: slog.LevelInfo}
	p.sendUser(ctx, line)

	attrs := []slog.Attr{
		slog.String("user_id", userID), slog.String("outcome", line.outcome),
		slog.Int("selected", line.selected), slog.Int("submitted", line.submitted),
		slog.Int("images_without_copy", line.imagesWithoutCopy), slog.Int64("body_bytes", line.bodyBytes),
		slog.Int64("duration_ms", p.cfg.Now().Sub(start).Milliseconds()),
	}
	if waiting, err := p.cfg.Store.WaitingCount(context.WithoutCancel(ctx), userID); err == nil {
		attrs = append(attrs, slog.Int("left_waiting", waiting))
	}
	if line.batchID != "" {
		attrs = append(attrs, slog.String("batch_id", line.batchID))
	}
	if line.anthropicBatchID != "" {
		attrs = append(attrs, slog.String("anthropic_batch_id", line.anthropicBatchID))
	}
	if line.status != 0 {
		attrs = append(attrs, slog.Int("status", line.status), slog.String("error_type", line.errorType))
	}
	if line.err != nil {
		// The errors of the pass name ids at most (requests.go, store); the error of a send
		// that got no answer is the transport's, which the Anthropic client strips of any
		// request detail.
		attrs = append(attrs, slog.String("error", line.err.Error()))
	}
	p.cfg.Logger.LogAttrs(ctx, line.level, "analysis pass", attrs...)
}

// sendUser runs the guards, the selection, the request building, the submission and the send
// of userID, filling line.
func (p *Pass) sendUser(ctx context.Context, line *userPass) {
	userID := line.userID
	fail := func(outcome string, level slog.Level, err error) {
		line.outcome, line.level, line.err = outcome, level, err
	}

	// Guard 2: reconcile, then no batch without a recorded cost (RG-81, ADR-0021).
	if err := p.reconcile(ctx, userID); err != nil {
		p.cfg.Logger.Error("reconcile batches", slog.String("user_id", userID), slog.String("error", err.Error()))
	}
	blocked, err := p.cfg.Store.HasBatchWithoutCost(ctx, userID)
	if err != nil {
		fail(outcomeFailed, slog.LevelError, err)
		return
	}
	if blocked {
		fail(outcomeCostPending, slog.LevelWarn, nil)
		return
	}
	// Guard 3: the spend of the Paris calendar month stays below the budget (RG-81).
	from, to := monthBounds(p.cfg.Now(), p.cfg.Location)
	spent, err := p.cfg.Store.MonthSpend(ctx, userID, from, to)
	if err != nil {
		fail(outcomeFailed, slog.LevelError, err)
		return
	}
	if spent >= p.cfg.BudgetUSD {
		fail(outcomeBudget, slog.LevelWarn, nil)
		return
	}

	// Selection: the user's extracted public items, oldest first, 200 at most (RG-12, RG-32).
	queue, err := p.cfg.Store.AnalysisQueue(ctx, userID, MaxBatchItems)
	if err != nil {
		fail(outcomeFailed, slog.LevelError, err)
		return
	}
	if p.cfg.selected != nil {
		queue = p.cfg.selected(userID, queue)
	}
	line.selected = len(queue)
	if len(queue) == 0 {
		line.outcome = outcomeNothing
		return
	}

	reqs, outcome, err := p.build(ctx, userID, queue)
	if reqs != nil {
		defer func() { _ = reqs.Close() }() // the data of its unnamed files goes with them
		line.imagesWithoutCopy = reqs.ImagesWithoutCopy()
	}
	if err != nil {
		level := slog.LevelError
		if outcome == outcomeStopped {
			level = slog.LevelWarn
		}
		fail(outcome, level, err)
		return
	}
	ids := reqs.IDs()
	if len(ids) == 0 {
		line.outcome = outcomeNothing
		return
	}
	if p.cfg.beforeSubmit != nil {
		p.cfg.beforeSubmit(ctx, userID, ids)
	}

	// Submission: the last word on privacy, re-read under BEGIN IMMEDIATE (RG-12, design.md D7).
	batchID, moved, err := p.cfg.Store.SubmitBatch(ctx, userID, store.NewBatch{
		Model: p.cfg.Model, PromptVersion: p.cfg.Prompt.Version, EnvelopeVersion: p.cfg.Prompt.EnvelopeVersion,
	}, ids)
	if err != nil {
		fail(outcomeFailed, slog.LevelError, err)
		return
	}
	if len(moved) == 0 {
		line.outcome = outcomeNothing
		return
	}
	line.batchID, line.submitted = batchID, len(moved)
	if p.cfg.afterSubmit != nil {
		p.cfg.afterSubmit(ctx, userID, batchID)
	}
	p.send(ctx, line, reqs, moved)
}

// build writes the requests of queue, in order, until one does not fit. It returns the
// outcome class of an error.
func (p *Pass) build(ctx context.Context, userID string, queue []store.AnalysisQueueItem) (*Requests, string, error) {
	referentiel, err := LoadReferentiel(ctx, p.cfg.Store, userID)
	if err != nil {
		return nil, outcomeFailed, err
	}
	reqs, err := NewRequests(RequestsConfig{
		UserID: userID, Model: p.cfg.Model, Prompt: p.cfg.Prompt, Referentiel: referentiel, BodyLimit: p.cfg.BodyLimit,
		Contents: p.cfg.Store, Assets: p.cfg.Assets, Copier: p.cfg.Copier, Temp: p.cfg.Temp, Logger: p.cfg.Logger,
	})
	if err != nil {
		return nil, buildOutcome(err), err
	}
	for _, item := range queue {
		added, err := reqs.Add(ctx, item)
		switch {
		case errors.Is(err, ErrItemGone):
			continue
		case err != nil:
			return reqs, buildOutcome(err), err
		case !added:
			// The body is full: this item and the later ones wait for the next pass (RG-32).
			return reqs, "", nil
		}
	}
	return reqs, "", nil
}

// buildOutcome is the outcome class of an error of the request building.
func buildOutcome(err error) string {
	switch {
	case errors.Is(err, ErrPrivateItem):
		return outcomePrivate
	case errors.Is(err, ErrCopierUnavailable):
		return outcomeCopier
	case errors.Is(err, errStorage):
		return outcomeStorage
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return outcomeStopped
	default:
		return outcomeFailed
	}
}

// send assembles the body of the moved items, sends it once and records the outcome
// (design.md D7). The batch batchID is pending with its items submitted.
func (p *Pass) send(ctx context.Context, line *userPass, reqs *Requests, moved []string) {
	userID, batchID := line.userID, line.batchID
	// The writes after the transaction happen even when the pass is stopped, bounded.
	record := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	}
	// putBack records a send that definitely did not happen: the items go back to extracted.
	putBack := func() {
		wctx, cancel := record()
		defer cancel()
		if _, err := p.cfg.Store.FailBatch(wctx, userID, batchID); err != nil {
			p.cfg.Logger.Error("analysis pass: put the items back", slog.String("user_id", userID),
				slog.String("batch_id", batchID), slog.String("error", err.Error()))
		}
	}

	bodyW, bodyR, err := p.cfg.Temp.NewFile()
	if err != nil {
		putBack()
		line.outcome, line.level, line.err = outcomeStorage, slog.LevelError, fmt.Errorf("%w: body file: %w", errStorage, err)
		return
	}
	defer closeFiles(bodyW, bodyR)
	size, err := reqs.WriteBody(bodyW, moved)
	if err != nil {
		putBack()
		line.outcome, line.level, line.err = outcomeStorage, slog.LevelError, err
		return
	}
	line.bodyBytes = size

	// Last check before the send: an item checked Privé since the submission is left out of
	// the batch, back to extracted, and the body is assembled again from the remaining ids, so
	// that item_count is the number of requests sent; the others go the same night. Repeated
	// until a round leaves nothing out, so that an item checked Privé during a rewrite is left
	// out too: the window left is the upload itself. When none is left, nothing is sent (RG-12,
	// RG-14, design.md D7, D11).
	// The loop ends: each round that goes on leaves out at least one item of a batch of at most
	// 200, and a batch left empty is failed.
	for {
		dropped, remaining, err := p.cfg.Store.DropPrivateFromBatch(ctx, userID, batchID)
		if err != nil {
			putBack()
			line.outcome, line.level, line.err = outcomeFailed, slog.LevelError, err
			if ctx.Err() != nil {
				line.outcome, line.level = outcomeStopped, slog.LevelWarn
			}
			return
		}
		if len(dropped) == 0 {
			break
		}
		p.cfg.Logger.Warn("analysis pass: items made private before the send left out",
			slog.String("user_id", userID), slog.String("batch_id", batchID), slog.String("outcome", outcomePrivateAfter),
			slog.Int("removed", len(dropped)), slog.Int("remaining", len(remaining)))
		line.submitted = len(remaining)
		if len(remaining) == 0 {
			// The batch is failed by the same transaction: nothing is sent.
			line.outcome, line.level, line.bodyBytes = outcomePrivateAfter, slog.LevelWarn, 0
			return
		}
		if p.cfg.beforeRewrite != nil {
			err = p.cfg.beforeRewrite(ctx, userID, batchID)
		}
		if err == nil {
			size, err = rewriteBody(reqs, bodyW, remaining)
		}
		if err != nil {
			putBack()
			line.outcome, line.level, line.err = outcomeStorage, slog.LevelError, err
			return
		}
		line.bodyBytes = size
	}

	// A pass stopped before the send sends nothing: the items go back rather than wait for a
	// reconciliation that would report a possible double send.
	if err := ctx.Err(); err != nil {
		putBack()
		line.outcome, line.level, line.err = outcomeStopped, slog.LevelWarn, err
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, p.cfg.SendTimeout)
	ref, err := p.cfg.Analyzer.CreateBatch(sendCtx, io.NewSectionReader(bodyR, 0, size), size)
	cancel()
	var (
		rejected *RejectedError
		server   *ServerError
	)
	switch {
	case err == nil:
		line.anthropicBatchID = ref.ID
		wctx, cancel := record()
		defer cancel()
		if err := p.cfg.Store.MarkBatchSent(wctx, userID, batchID, ref.ID); err != nil {
			// The batch stays pending: the reconciliation settles it (design.md D7).
			line.outcome, line.level, line.err = outcomeRecordFailed, slog.LevelError, err
			if errors.Is(err, store.ErrAnthropicBatchIDRecorded) {
				line.outcome = outcomeIDRecorded
			}
			return
		}
		line.outcome = outcomeSent
	case errors.As(err, &rejected) || errors.Is(err, ErrRejected):
		putBack()
		line.outcome, line.level = outcomeRejected, slog.LevelError
		if rejected != nil {
			line.status, line.errorType = rejected.Status, rejected.Type
		}
	default:
		// Unknown outcome (no answer, or a server error status): the batch stays pending with
		// its items submitted, and is reconciled at the next start or pass (design.md D8).
		line.outcome, line.level, line.err = outcomeUnknown, slog.LevelWarn, err
		if errors.As(err, &server) {
			line.status, line.errorType = server.Status, server.Type
		}
	}
}

// rewriteBody empties the body file w and writes in it the body of ids, from the segments.
func rewriteBody(reqs *Requests, w *os.File, ids []string) (int64, error) {
	if err := w.Truncate(0); err != nil {
		return 0, fmt.Errorf("%w: truncate body: %w", errStorage, pathless(err))
	}
	if _, err := w.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("%w: rewind body: %w", errStorage, pathless(err))
	}
	return reqs.WriteBody(w, ids)
}

// closeFiles closes the two ends of an unnamed temporary file; its data goes with them.
func closeFiles(files ...*os.File) {
	for _, f := range files {
		_ = f.Close() // nothing to flush: the data is dropped
	}
}

// monthBounds returns midnight of the first day of the Paris month of now and of the next
// month, in UTC (design.md D3). Midnight never falls in a clock change in Paris.
func monthBounds(now time.Time, loc *time.Location) (from, to time.Time) {
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	return start.UTC(), start.AddDate(0, 1, 0).UTC()
}

// reconcile matches the pending batches of userID with the batches the key holds (design.md
// D8): a pending batch adopts its single candidate, otherwise its items go back to extracted
// and a possible double send is logged. When the list fails, the batches stay pending, and
// the guard then blocks the user.
func (p *Pass) reconcile(ctx context.Context, userID string) error {
	pending, err := p.cfg.Store.PendingBatches(ctx, userID)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	listCtx, cancelList := context.WithTimeout(ctx, p.cfg.ListTimeout)
	listed, err := p.cfg.Analyzer.ListBatches(listCtx, pending[0].CreatedAt.Add(-matchBefore))
	cancelList()
	if err != nil {
		for _, b := range pending {
			p.cfg.Logger.Warn("reconcile batch", slog.String("user_id", userID), slog.String("batch_id", b.ID),
				slog.String("outcome", outcomeListFailed))
		}
		return nil
	}
	all, err := p.cfg.Store.Batches(ctx, userID)
	if err != nil {
		return err
	}
	recorded := map[string]bool{}
	for _, b := range all {
		if b.AnthropicBatchID != nil {
			recorded[*b.AnthropicBatchID] = true
		}
	}
	counts := map[string]int{}
	for _, b := range pending {
		counts[b.ID] = b.ItemCount
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	for _, m := range matchPending(pending, listed, recorded) {
		attrs := []slog.Attr{
			slog.String("user_id", userID), slog.String("batch_id", m.BatchID), slog.Int("item_count", counts[m.BatchID]),
		}
		if m.AnthropicID != "" {
			attrs = append(attrs, slog.String("anthropic_batch_id", m.AnthropicID))
			err := p.cfg.Store.MarkBatchSent(wctx, userID, m.BatchID, m.AnthropicID)
			if err == nil {
				p.cfg.Logger.LogAttrs(ctx, slog.LevelInfo, "reconcile batch", append(attrs, slog.String("outcome", outcomeAdopted))...)
				continue
			}
			if !errors.Is(err, store.ErrAnthropicBatchIDRecorded) {
				outcome := outcomeRecordFailed
				if errors.Is(err, store.ErrNotFound) {
					outcome = outcomeAlreadyClosed
				}
				p.cfg.Logger.LogAttrs(ctx, slog.LevelError, "reconcile batch",
					append(attrs, slog.String("outcome", outcome), slog.String("error", err.Error()))...)
				continue
			}
			// The candidate is another row's (another user's on the same key): this batch has
			// no candidate of its own. Left pending, it would block the user for good.
		}
		reverted, err := p.cfg.Store.FailBatch(wctx, userID, m.BatchID)
		if err != nil {
			outcome := outcomeRecordFailed
			if errors.Is(err, store.ErrNotFound) {
				outcome = outcomeAlreadyClosed
			}
			p.cfg.Logger.LogAttrs(ctx, slog.LevelError, "reconcile batch",
				append(attrs, slog.String("outcome", outcome), slog.String("error", err.Error()))...)
			continue
		}
		p.cfg.Logger.LogAttrs(ctx, slog.LevelWarn, "reconcile batch",
			append(attrs, slog.String("outcome", outcomeDoubleSend), slog.Int("items_put_back", reverted))...)
	}
	return nil
}

// match is the decision of the reconciliation for one pending batch: the Anthropic batch it
// adopts, or none (AnthropicID empty: its items go back to extracted).
type match struct {
	BatchID     string
	AnthropicID string
}

// matchPending decides, for each pending batch, oldest first, which listed batch it sent
// (design.md D8): the candidates are created within [created_at − 10 min, created_at + 20 min],
// hold as many requests as the batch holds items, and are not recorded yet. Exactly one
// candidate is adopted and leaves the pool; none, or more than one, adopts nothing.
func matchPending(pending []store.Batch, listed []BatchInfo, recorded map[string]bool) []match {
	taken := make(map[string]bool, len(recorded))
	for id, ok := range recorded {
		taken[id] = ok
	}
	decisions := make([]match, 0, len(pending))
	for _, row := range pending {
		from, to := row.CreatedAt.Add(-matchBefore), row.CreatedAt.Add(matchAfter)
		var candidates []string
		for _, b := range listed {
			if taken[b.ID] || b.RequestCount != row.ItemCount || b.CreatedAt.Before(from) || b.CreatedAt.After(to) {
				continue
			}
			candidates = append(candidates, b.ID)
		}
		decision := match{BatchID: row.ID}
		if len(candidates) == 1 {
			decision.AnthropicID = candidates[0]
			taken[candidates[0]] = true
		}
		decisions = append(decisions, decision)
	}
	return decisions
}
