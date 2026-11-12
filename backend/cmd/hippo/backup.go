package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/ghislain95/hippo/backend/internal/backup"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/config"
	"github.com/ghislain95/hippo/backend/internal/schedule"
)

const (
	// backupCommandName is the sub-command that makes one backup in the running container:
	// `docker compose exec hippo /hippo backup` (nightly-backup design D13).
	backupCommandName = "backup"
	// backupLockWait is how long `hippo backup` waits for a backup in progress (design D11).
	backupLockWait = 10 * time.Minute
	// backupMemoryLimit is the soft limit of the Go heap of `hippo backup`, a fourth Go process
	// in the 384 MB of the container (ADR-0038). SQLite's page cache is outside the Go heap.
	backupMemoryLimit = 32 << 20
)

// backupCommand makes one backup with the configuration of the service, as `hippo backup`
// does, waiting up to lockWait for a backup in progress. Its log line goes to stdout: an exec
// does not reach the Docker logs. It opens neither the store nor the images folder through
// assetfs, nor DATA_DIR/tmp: no migration, no sweep. nil only when a complete backup exists.
func backupCommand(ctx context.Context, getenv func(string) string, stdout io.Writer, lockWait time.Duration) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	// The line of the backup is the command's report, read by the operator and nightly-update:
	// printed whatever LOG_LEVEL says, at info as at warn or error.
	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: min(cfg.LogLevel, slog.LevelInfo)}))
	paris, err := capture.LoadParis()
	if err != nil {
		return err
	}
	if _, err := backup.Run(ctx, backup.Options{
		Database: filepath.Join(cfg.DataDir, databaseFile),
		Assets:   filepath.Join(cfg.DataDir, assetsDirectory),
		Dir:      cfg.BackupDir,
		Location: paris,
		Now:      time.Now,
		Version:  version,
		Trigger:  backup.TriggerCommand,
		LockWait: lockWait,
		Logger:   logger,
	}); err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	return nil
}

// backupOptions is what the nightly backup needs (design D14).
type backupOptions struct {
	Schedule schedule.Daily
	Location *time.Location // Europe/Paris
	Clock    schedule.Clock
	Logger   *slog.Logger
	Database string // DATA_DIR/hippo.db
	Assets   string // DATA_DIR/assets
	Dir      string // BACKUP_DIR
	Version  string
}

// startBackup starts, in the background, the nightly backup at its schedule in Paris time: one
// backup per date, never caught up, skipped when another backup holds the lock (trigger
// nightly, no wait). Its log line reports each outcome. stop cancels it and returns once a
// backup in flight has ended (a stop removes its partial folder).
func startBackup(ctx context.Context, opts backupOptions) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		// The With tells the missed runs of the backup from those of the analysis.
		schedule.Run(ctx, opts.Schedule, opts.Location, opts.Clock, opts.Logger.With(slog.String("schedule", "backup")),
			func(ctx context.Context) {
				// Its one log line reports the outcome and the error: nothing more to do here.
				_, _ = backup.Run(ctx, backup.Options{
					Database: opts.Database,
					Assets:   opts.Assets,
					Dir:      opts.Dir,
					Location: opts.Location,
					Now:      opts.Clock.Now,
					Version:  opts.Version,
					Trigger:  backup.TriggerNightly,
					Logger:   opts.Logger,
				})
			})
	})
	opts.Logger.Info("backup scheduled",
		slog.String("backup_schedule", opts.Schedule.String()),
		slog.String("time_zone", opts.Location.String()),
		slog.Time("next_run", opts.Schedule.Next(opts.Clock.Now(), opts.Location).UTC()))
	return func() {
		cancel()
		wg.Wait()
	}
}
