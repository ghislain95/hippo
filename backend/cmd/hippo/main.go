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

	"github.com/ghislain95/hippo/backend/internal/auth"
	"github.com/ghislain95/hippo/backend/internal/config"
	"github.com/ghislain95/hippo/backend/internal/httpapi"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// version is set at build time (-ldflags "-X main.version=<git revision>").
var version = "dev"

const (
	databaseFile    = "hippo.db"
	shutdownTimeout = 10 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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

	tokenHash := auth.HashToken(cfg.APIToken)
	userID, err := st.EnsureSingleUser(ctx, tokenHash)
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewServer(httpapi.Options{
			Store:   st,
			Auth:    auth.NewSingleUser(userID, tokenHash),
			Logger:  logger,
			Version: version,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second, // image uploads up to 20 MB (RG-02)
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return serve(ctx, server, logger)
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
