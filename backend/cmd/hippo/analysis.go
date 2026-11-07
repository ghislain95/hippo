package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/imagecopy"
	"github.com/ghislain95/hippo/backend/internal/schedule"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// analysisOptions is what the nightly analysis needs (design.md D1, D8 of submit-nightly-batch).
type analysisOptions struct {
	Store *store.Store
	// Analyzer is nil without ANTHROPIC_API_KEY: the pass then sends and reconciles nothing.
	Analyzer  analysis.Analyzer
	Model     string
	Prompt    prompt.Prompt
	BudgetUSD float64
	// Prices are the standard prices of Model (ANALYSIS_*_PRICE_USD_PER_MTOK).
	Prices   analysis.Prices
	Schedule schedule.Daily
	Location *time.Location // Europe/Paris
	Assets   analysis.AssetOpener
	Temp     analysis.TempFiles
	// CopierCommand runs the child that copies an image: [executable, "copy-image"].
	CopierCommand []string
	// Clock is the clock of the schedule and of the pass.
	Clock schedule.Clock
	// FollowClock is the clock of the 15-minute follow-up of the batches in progress; nil means
	// Clock. Tests give it a fake clock of its own, so that the waits of the schedule and of the
	// follow-up are told apart.
	FollowClock schedule.Clock
	Logger      *slog.Logger
}

// startAnalysis builds the one analysis pass of the process and starts, in the background, the
// reconciliation of the batches left pending at the last stop, then the follow-up of the
// batches in progress, at once and every 15 minutes (apply-batch-results design D1), and the
// schedule of the nightly pass. All use the same pass, whose lock keeps a reconciliation or a
// follow-up from settling a batch whose send is in flight (design.md D8). stop cancels them and
// returns once every goroutine has returned: the store, the images and the temporary files can
// then be closed.
func startAnalysis(ctx context.Context, opts analysisOptions) (stop func(), err error) {
	pass, err := analysis.NewPass(analysis.PassConfig{
		Store:     opts.Store,
		Analyzer:  opts.Analyzer,
		Model:     opts.Model,
		Prompt:    opts.Prompt,
		BudgetUSD: opts.BudgetUSD,
		Prices:    opts.Prices,
		Location:  opts.Location,
		Now:       opts.Clock.Now,
		Assets:    opts.Assets,
		Copier:    imagecopy.NewCopier(opts.CopierCommand, imagecopy.DefaultLimits()),
		Temp:      opts.Temp,
		Logger:    opts.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("start the analysis: %w", err)
	}

	// The analysis has its own context: a server that fails without any signal stops it too.
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	followClock := opts.FollowClock
	if followClock == nil {
		followClock = opts.Clock
	}
	wg.Go(func() {
		// In the background: a slow list or state query of the AI service never delays the
		// start-up. The batches reconciled are followed at once: one that ended while the
		// service was stopped is applied without waiting for a tick.
		if err := pass.Reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
			opts.Logger.Error("reconcile batches at start-up", slog.String("error", err.Error()))
		}
		if err := pass.Follow(ctx); err != nil && !errors.Is(err, context.Canceled) {
			opts.Logger.Error("follow batches at start-up", slog.String("error", err.Error()))
		}
		analysis.RunFollowUp(ctx, pass, followClock, analysis.FollowInterval, opts.Logger)
	})
	wg.Go(func() {
		schedule.Run(ctx, opts.Schedule, opts.Location, opts.Clock, opts.Logger, func(ctx context.Context) {
			if err := pass.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				opts.Logger.Error("analysis pass", slog.String("error", err.Error()))
			}
		})
	})
	// The prices in force price every batch once read (RG-80): logged, so that a price left
	// behind a change of model shows.
	opts.Logger.Info("analysis prices", slog.String("model", opts.Model),
		slog.Float64("input_usd_per_mtok", opts.Prices.InputPerMTok),
		slog.Float64("output_usd_per_mtok", opts.Prices.OutputPerMTok))
	opts.Logger.Info("analysis scheduled",
		slog.String("batch_schedule", opts.Schedule.String()),
		slog.String("time_zone", opts.Location.String()),
		slog.Time("next_run", opts.Schedule.Next(opts.Clock.Now(), opts.Location).UTC()))
	return func() {
		cancel()
		wg.Wait()
	}, nil
}
