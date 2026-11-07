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
	Clock  schedule.Clock
	Logger *slog.Logger
}

// startAnalysis builds the one analysis pass of the process and starts, in the background, the
// reconciliation of the batches left pending at the last stop, then the schedule of the
// nightly pass. Both use the same pass, whose lock keeps the reconciliation from settling a
// batch whose send is in flight (design.md D8). stop cancels both and returns once they have
// returned: the store, the images and the temporary files can then be closed.
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
	wg.Go(func() {
		// In the background: a slow list of the AI service never delays the start-up.
		if err := pass.Reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
			opts.Logger.Error("reconcile batches at start-up", slog.String("error", err.Error()))
		}
	})
	wg.Go(func() {
		schedule.Run(ctx, opts.Schedule, opts.Location, opts.Clock, opts.Logger, func(ctx context.Context) {
			if err := pass.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				opts.Logger.Error("analysis pass", slog.String("error", err.Error()))
			}
		})
	})
	opts.Logger.Info("analysis scheduled",
		slog.String("batch_schedule", opts.Schedule.String()),
		slog.String("time_zone", opts.Location.String()),
		slog.Time("next_run", opts.Schedule.Next(opts.Clock.Now(), opts.Location).UTC()))
	return func() {
		cancel()
		wg.Wait()
	}, nil
}
