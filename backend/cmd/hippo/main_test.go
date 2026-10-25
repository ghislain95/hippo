package main

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	t.Run("starts, answers the healthcheck and stops on cancel", func(t *testing.T) {
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
	})

	t.Run("refuses to start with an invalid configuration", func(t *testing.T) {
		err := run(context.Background(), nil, func(string) string { return "" }, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "API_TOKEN") {
			t.Errorf("run() error = %v, want an API_TOKEN error", err)
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
