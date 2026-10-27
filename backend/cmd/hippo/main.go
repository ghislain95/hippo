// Command hippo is the Hippo backend: HTTP API, SQLite storage and, later, the extraction and
// the nightly analysis (ADR-0003).
//
// Usage:
//
//	hippo              start the server (configuration from environment variables)
//	hippo healthcheck  probe the local /healthz, for the Docker healthcheck
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // the Paris zone of RG-04 without a time zone database in the image

	"github.com/ghislain95/hippo/backend/internal/assetfs"
	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/config"
	"github.com/ghislain95/hippo/backend/internal/httpapi"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// version is set at build time (-ldflags "-X main.version=<git revision>").
var version = "dev"

const (
	databaseFile    = "hippo.db"
	assetsDirectory = "assets" // image files, under DATA_DIR (ADR-0004, ADR-0019)
	shutdownTimeout = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// After the first signal, restore the default handling: a second Ctrl-C during the drain
	// stops the process at once.
	go func() {
		<-ctx.Done()
		stop()
	}()
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hippo:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) > 0 {
		if args[0] == "healthcheck" {
			return healthcheck(ctx, getenv)
		}
		return fmt.Errorf("unknown command %q (want no argument, or healthcheck)", args[0])
	}

	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	// The dates of a duplicate's note are in Paris time (RG-04): no start without the zone.
	paris, err := capture.LoadParis()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, databaseFile))
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close database", slog.String("error", err.Error()))
		}
	}()

	assets, err := openAssets(filepath.Join(cfg.DataDir, assetsDirectory), logger)
	if err != nil {
		return err
	}
	defer func() {
		if err := assets.Close(); err != nil {
			logger.Error("close assets directory", slog.String("error", err.Error()))
		}
	}()

	tokenHash := auth.HashToken(cfg.APIToken)
	userID, err := st.EnsureSingleUser(ctx, tokenHash)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewServer(httpapi.Options{
			Store:      st,
			Auth:       auth.NewSingleUser(userID, tokenHash),
			Capture:    capture.NewService(st, paris, logger),
			Assets:     st,
			AssetFiles: assets,
			Logger:     logger,
			Version:    version,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		// Image uploads and downloads get 5 minutes, set per request by httpapi.
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	return serve(ctx, server, logger)
}

// openAssets creates the assets directory if needed, removes the temporary files an
// interrupted upload left, and fails at once when the service cannot write there.
func openAssets(path string, logger *slog.Logger) (*assetfs.Dir, error) {
	assets, err := assetfs.Open(path)
	if err != nil {
		return nil, err
	}
	removed, err := assets.RemoveTemps()
	if err == nil {
		err = assets.CheckWritable()
	}
	if err != nil {
		return nil, errors.Join(err, assets.Close())
	}
	if removed > 0 {
		logger.Info("removed leftover temporary uploads", slog.Int("count", removed))
	}
	return assets, nil
}

// serve runs the server until ctx is cancelled (SIGINT, SIGTERM), then drains it.
func serve(ctx context.Context, server *http.Server, logger *slog.Logger) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", server.Addr, err)
	}
	logger.Info("hippo started", slog.String("addr", listener.Addr().String()), slog.String("version", version))

	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()

	select {
	case err := <-errs:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errs; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	logger.Info("hippo stopped")
	return nil
}

// healthcheck calls the local /healthz and fails unless it answers 200. The distroless image has
// no curl: the Docker healthcheck runs `hippo healthcheck`.
func healthcheck(ctx context.Context, getenv func(string) string) error {
	addr := getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("HTTP_ADDR %q: %w", addr, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", resp.StatusCode)
	}
	return nil
}
