package capture_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// noWake is the waker of the tests that run without extraction worker.
type noWake struct{}

func (noWake) Wake() {}

// countingWaker counts the wake-ups a capture asked for.
type countingWaker struct{ calls atomic.Int64 }

func (w *countingWaker) Wake() { w.calls.Add(1) }

func TestCaptureWakesTheExtraction(t *testing.T) {
	ctx := context.Background()
	newService := func(t *testing.T) (*capture.Service, *countingWaker, string) {
		t.Helper()
		st, _ := storetest.OpenWithClock(t, time.Now)
		userID, err := st.CreateUser(ctx, "owner", "hash")
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		paris, err := capture.LoadParis()
		if err != nil {
			t.Fatalf("LoadParis: %v", err)
		}
		waker := &countingWaker{}
		return capture.NewService(st, waker, paris, slog.New(slog.NewJSONHandler(io.Discard, nil))), waker, userID
	}

	t.Run("a new link or note wakes the extraction once", func(t *testing.T) {
		service, waker, userID := newService(t)
		if _, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a")}); err != nil {
			t.Fatalf("Capture link: %v", err)
		}
		if _, err := service.Capture(ctx, userID, capture.Request{Note: ptr("buy milk")}); err != nil {
			t.Fatalf("Capture note: %v", err)
		}
		if got := waker.calls.Load(); got != 2 {
			t.Errorf("wake-ups = %d, want 2", got)
		}
	})

	t.Run("a replay, a duplicate or a refusal wakes nothing", func(t *testing.T) {
		service, waker, userID := newService(t)
		first := capture.Request{URL: ptr("https://example.com/a"), ClientID: ptr(testClientID)}
		if _, err := service.Capture(ctx, userID, first); err != nil {
			t.Fatalf("Capture: %v", err)
		}
		waker.calls.Store(0)
		if _, err := service.Capture(ctx, userID, first); err != nil {
			t.Fatalf("replay: %v", err)
		}
		if _, err := service.Capture(ctx, userID, capture.Request{URL: ptr("https://example.com/a"), UserNote: ptr("again")}); err != nil {
			t.Fatalf("duplicate: %v", err)
		}
		if _, err := service.Capture(ctx, userID, capture.Request{}); err == nil {
			t.Fatal("empty capture accepted")
		}
		if got := waker.calls.Load(); got != 0 {
			t.Errorf("wake-ups = %d, want none", got)
		}
	})
}
