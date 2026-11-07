package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	t.Run("starts without an API key, answers the healthcheck and stops on cancel", func(t *testing.T) {
		port := freePort(t)
		env := map[string]string{
			"API_TOKEN": "test-token-0123456789abcdef012345",
			"HTTP_ADDR": "127.0.0.1:" + port,
			"DATA_DIR":  t.TempDir(),
		}
		getenv := func(name string) string { return env[name] }

		ctx, cancel := context.WithCancel(context.Background())
		logs := &bytes.Buffer{}
		done := make(chan error, 1)
		go func() { done <- run(ctx, nil, getenv, logs) }()

		deadline := time.Now().Add(5 * time.Second)
		for {
			err := healthcheck(context.Background(), getenv)
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				cancel()
				t.Fatalf("server never became healthy: %v\n%s", err, logs)
			}
			time.Sleep(20 * time.Millisecond) // polling a real server: no event to wait on
		}

		cancel()
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
		if strings.Contains(logs.String(), env["API_TOKEN"]) {
			t.Error("logs contain the API token")
		}
		for _, want := range []string{
			`"msg":"no ANTHROPIC_API_KEY: the nightly analysis sends nothing"`,
			`"msg":"analysis scheduled","batch_schedule":"0 2 * * *","time_zone":"Europe/Paris"`,
		} {
			if !strings.Contains(logs.String(), want) {
				t.Errorf("logs lack %s:\n%s", want, logs)
			}
		}
		if entries, err := os.ReadDir(filepath.Join(env["DATA_DIR"], "tmp")); err != nil || len(entries) != 0 {
			t.Errorf("temporary directory: %v (%v), want created empty", entries, err)
		}
	})

	// apply-batch-results, item-analysis « The token prices are configured », log side.
	t.Run("The prices default to the Haiku 4.5 prices", func(t *testing.T) {
		env := map[string]string{
			"API_TOKEN": "test-token-0123456789abcdef012345",
			"HTTP_ADDR": "127.0.0.1:" + freePort(t),
			"DATA_DIR":  t.TempDir(),
		}
		logs := runUntilHealthy(t, env)
		want := `"msg":"analysis prices","model":"claude-haiku-4-5","input_usd_per_mtok":1,"output_usd_per_mtok":5`
		if !strings.Contains(logs, want) {
			t.Errorf("logs lack %s:\n%s", want, logs)
		}
	})

	t.Run("starts with an API key and never logs it", func(t *testing.T) {
		// A new base holds no pending batch and no waiting item: neither the reconciliation at
		// start-up nor a pass reaches the network.
		port := freePort(t)
		env := map[string]string{
			"API_TOKEN":         "test-token-0123456789abcdef012345",
			"ANTHROPIC_API_KEY": "sk-ant-test-KEY-MUST-NOT-LEAK-7f3c9e",
			"HTTP_ADDR":         "127.0.0.1:" + port,
			"DATA_DIR":          t.TempDir(),
		}
		getenv := func(name string) string { return env[name] }
		ctx, cancel := context.WithCancel(context.Background())
		logs := &bytes.Buffer{}
		done := make(chan error, 1)
		go func() { done <- run(ctx, nil, getenv, logs) }()
		deadline := time.Now().Add(5 * time.Second)
		for healthcheck(context.Background(), getenv) != nil {
			if time.Now().After(deadline) {
				cancel()
				t.Fatalf("server never became healthy:\n%s", logs)
			}
			time.Sleep(20 * time.Millisecond) // polling a real server: no event to wait on
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("run: %v", err)
		}
		for _, secret := range []string{env["ANTHROPIC_API_KEY"], "KEY-MUST-NOT-LEAK", env["API_TOKEN"]} {
			if strings.Contains(logs.String(), secret) {
				t.Errorf("logs contain %q", secret)
			}
		}
		if strings.Contains(logs.String(), "no ANTHROPIC_API_KEY") {
			t.Errorf("logs say there is no key:\n%s", logs)
		}
		if !strings.Contains(logs.String(), `"msg":"analysis scheduled"`) {
			t.Errorf("logs lack the schedule of the analysis:\n%s", logs)
		}
	})

	t.Run("returns an error when the port is taken, without waiting for a signal", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		env := map[string]string{
			"API_TOKEN": "test-token-0123456789abcdef012345",
			"HTTP_ADDR": listener.Addr().String(),
			"DATA_DIR":  t.TempDir(),
		}
		done := make(chan error, 1)
		go func() {
			done <- run(context.Background(), nil, func(name string) string { return env[name] }, &bytes.Buffer{})
		}()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "listen") {
				t.Errorf("run() error = %v, want a listen error", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("run did not return: the extraction worker outlived the server")
		}
	})

	t.Run("refuses to start with an invalid configuration", func(t *testing.T) {
		err := run(context.Background(), nil, func(string) string { return "" }, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
			t.Errorf("run() error = %v, want an API_TOKEN error", err)
		}
	})

	t.Run("refuses to start with a schedule that is not daily", func(t *testing.T) {
		env := map[string]string{
			"API_TOKEN":      "test-token-0123456789abcdef012345",
			"DATA_DIR":       t.TempDir(),
			"BATCH_SCHEDULE": "*/5 * * * *",
		}
		err := run(context.Background(), nil, func(name string) string { return env[name] }, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "BATCH_SCHEDULE") {
			t.Errorf("run() error = %v, want a BATCH_SCHEDULE error", err)
		}
	})

	t.Run("rejects an unknown command", func(t *testing.T) {
		if err := run(context.Background(), []string{"migrate"}, func(string) string { return "" }, &bytes.Buffer{}); err == nil {
			t.Error("run(migrate): want an error")
		}
	})

	t.Run("healthcheck fails when nothing listens", func(t *testing.T) {
		port := freePort(t)
		if err := healthcheck(context.Background(), func(string) string { return "127.0.0.1:" + port }); err == nil {
			t.Error("healthcheck on a closed port: want an error")
		}
	})
}

// runUntilHealthy runs the server with env until it answers its healthcheck, stops it, and
// returns its logs.
func runUntilHealthy(t *testing.T, env map[string]string) string {
	t.Helper()
	getenv := func(name string) string { return env[name] }
	ctx, cancel := context.WithCancel(context.Background())
	logs := &bytes.Buffer{}
	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, getenv, logs) }()
	deadline := time.Now().Add(5 * time.Second)
	for healthcheck(context.Background(), getenv) != nil {
		if time.Now().After(deadline) {
			cancel()
			err := <-done
			t.Fatalf("server never became healthy (run: %v):\n%s", err, logs)
		}
		time.Sleep(20 * time.Millisecond) // polling a real server: no event to wait on
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run: %v", err)
	}
	return logs.String()
}

func TestOpenAssets(t *testing.T) {
	t.Run("creates the assets directory and removes leftover temporary uploads only", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "assets")
		if err := os.MkdirAll(filepath.Join(dir, "user"), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		for _, name := range []string{".tmp-leftover", "user/asset.png"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
				t.Fatalf("write %s: %v", name, err)
			}
		}
		assets, err := openAssets(dir, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		if err != nil {
			t.Fatalf("openAssets: %v", err)
		}
		t.Cleanup(func() { _ = assets.Close() })
		if _, err := os.Stat(filepath.Join(dir, ".tmp-leftover")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("leftover temporary file: stat = %v, want removed", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "user", "asset.png")); err != nil {
			t.Errorf("committed file: %v, want kept", err)
		}
	})

	t.Run("fails when the data directory cannot hold it", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := openAssets(filepath.Join(file, "assets"), slog.New(slog.NewJSONHandler(io.Discard, nil))); err == nil {
			t.Error("openAssets under a file: want an error")
		}
	})
}

func TestOpenTempDir(t *testing.T) {
	t.Run("creates the temporary directory empty, logging the leftovers removed", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "tmp")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".pdf-leftover"), []byte("%PDF-1.7"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		logs := &bytes.Buffer{}
		tmp, err := openTempDir(dir, slog.New(slog.NewJSONHandler(logs, nil)))
		if err != nil {
			t.Fatalf("openTempDir: %v", err)
		}
		t.Cleanup(func() { _ = tmp.Close() })
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
			t.Errorf("directory holds %v (%v), want nothing", entries, err)
		}
		if !strings.Contains(logs.String(), `"count":1`) || strings.Contains(logs.String(), dir) {
			t.Errorf("logs = %s, want the count of files removed and no path", logs)
		}
	})

	t.Run("fails when the data directory cannot hold it", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, err := openTempDir(filepath.Join(file, "tmp"), slog.New(slog.NewJSONHandler(io.Discard, nil))); err == nil {
			t.Error("openTempDir under a file: want an error")
		}
	})
}

func freePort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return strconv.Itoa(port)
}
