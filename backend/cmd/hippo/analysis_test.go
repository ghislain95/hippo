package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/analysistest"
	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/assetfs"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/imagecopy"
	"github.com/ghislain95/hippo/backend/internal/schedule"
	"github.com/ghislain95/hippo/backend/internal/schedule/scheduletest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// negativeWait is how long a test waits for something that must not happen.
const negativeWait = 200 * time.Millisecond

// analysisEnv is a real store, images and temporary directory, the fake AI service and a fake
// clock at 10 January 2027, 00:00 UTC, one hour before the default schedule in Paris.
type analysisEnv struct {
	st     *store.Store
	path   string
	clock  *scheduletest.Clock
	ai     *analysistest.Analyzer
	opts   analysisOptions
	userID string
	logs   *syncBuffer
}

func newAnalysisEnv(t *testing.T) *analysisEnv {
	t.Helper()
	clock := scheduletest.NewClock(time.Date(2027, 1, 10, 0, 0, 0, 0, time.UTC))
	st, path := storetest.OpenWithClock(t, clock.Now)
	assets, err := assetfs.Open(filepath.Join(t.TempDir(), "assets"))
	if err != nil {
		t.Fatal(err)
	}
	tmp, _, err := extraction.OpenTempDir(filepath.Join(t.TempDir(), "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = assets.Close()
		_ = tmp.Close()
	})
	p, err := prompt.Load()
	if err != nil {
		t.Fatal(err)
	}
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatal(err)
	}
	daily, err := schedule.ParseDaily("0 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	userID, err := st.CreateUser(context.Background(), "owner", "hash-owner")
	if err != nil {
		t.Fatal(err)
	}
	ai := analysistest.New(clock.Now)
	logs := &syncBuffer{}
	return &analysisEnv{
		st: st, path: path, clock: clock, ai: ai, userID: userID, logs: logs,
		opts: analysisOptions{
			Store: st, Analyzer: ai, Model: "claude-haiku-4-5", Prompt: p, BudgetUSD: 5,
			Prices:   analysis.Prices{InputPerMTok: 1, OutputPerMTok: 5},
			Schedule: daily, Location: paris, Assets: assets, Temp: tmp,
			CopierCommand: []string{os.Args[0], imagecopy.ChildCommand},
			Clock:         clock, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		},
	}
}

// waiting captures a public link of the user and writes its extraction: it waits for the
// batch.
func (e *analysisEnv) waiting(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	u := "https://example.com/a"
	item, _, err := e.st.CaptureItem(ctx, e.userID, store.NewItem{
		Kind: "link", Source: "share", Status: "queued", Title: "example.com", URL: &u,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := "Le contenu"
	if ok, err := e.st.WriteExtraction(ctx, e.userID, item.ID, store.Extraction{Mode: "full", ContentText: &text}); err != nil || !ok {
		t.Fatalf("WriteExtraction = %v, %v", ok, err)
	}
}

func (e *analysisEnv) start(t *testing.T) func() {
	t.Helper()
	stop, err := startAnalysis(context.Background(), e.opts)
	if err != nil {
		t.Fatalf("startAnalysis: %v", err)
	}
	var once sync.Once
	stopOnce := func() { once.Do(stop) }
	t.Cleanup(stopOnce)
	return stopOnce
}

func receive(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never happened", what)
	}
}

func TestStartAnalysis(t *testing.T) {
	t.Run("the start-up reconciliation and the scheduled pass share one pass", func(t *testing.T) {
		e := newAnalysisEnv(t)
		e.waiting(t)
		// A batch whose send had an unknown outcome at the last stop: the reconciliation at
		// start-up lists the batches of the AI service, and so would a second pass instance
		// at the start of its run.
		id, err := e.st.NewID()
		if err != nil {
			t.Fatal(err)
		}
		storetest.InsertBatch(t, e.path, e.userID, storetest.Batch{
			ID: id, Status: "pending", ItemCount: 1, CreatedAt: time.Date(2027, 1, 9, 1, 0, 0, 0, time.UTC),
		})
		listed := make(chan struct{}, 4)
		release := make(chan struct{})
		e.ai.OnList(func(context.Context) {
			listed <- struct{}{}
			<-release
		})
		e.start(t)
		receive(t, listed, "the start-up reconciliation")

		// The scheduled pass comes while the reconciliation is still listing.
		e.clock.Wait(t)
		e.clock.Set(time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC))
		select {
		case <-listed:
			t.Error("the scheduled pass listed during the reconciliation: two pass instances")
		case <-time.After(negativeWait):
		}
		if n := e.ai.Creates(); n != 0 {
			t.Errorf("%d batches sent during the reconciliation, want 0", n)
		}

		close(release)
		e.clock.Wait(t) // the pass has run and the scheduler waits for the next date
		if n := e.ai.Creates(); n != 1 {
			t.Errorf("%d batches sent after the reconciliation, want 1", n)
		}
		if n := e.ai.Lists(); n != 1 {
			t.Errorf("%d lists, want the reconciliation's only", n)
		}
	})

	t.Run("stop waits for a pass in flight", func(t *testing.T) {
		e := newAnalysisEnv(t)
		e.waiting(t)
		entered := make(chan struct{})
		gate := make(chan struct{})
		e.ai.OnCreate(func(ctx context.Context) {
			close(entered)
			<-ctx.Done()
			<-gate
		})
		stop := e.start(t)
		e.clock.Wait(t)
		e.clock.Set(time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC))
		receive(t, entered, "the send of the batch")

		stopped := make(chan struct{})
		go func() {
			stop()
			close(stopped)
		}()
		select {
		case <-stopped:
			t.Fatal("stop returned while the pass was sending")
		case <-time.After(negativeWait):
		}
		close(gate)
		receive(t, stopped, "the stop")
		if !strings.Contains(e.logs.String(), `"msg":"analysis pass"`) {
			t.Errorf("the pass did not finish before stop returned; logs:\n%s", e.logs)
		}
	})

	t.Run("without an API key, the scheduled pass runs and sends nothing", func(t *testing.T) {
		e := newAnalysisEnv(t)
		e.waiting(t)
		e.opts.Analyzer = nil
		e.start(t)
		e.clock.Wait(t)
		e.clock.Set(time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC))
		e.clock.Wait(t)
		if !strings.Contains(e.logs.String(), `"outcome":"no_api_key"`) {
			t.Errorf("logs = %s, want the no_api_key outcome", e.logs)
		}
	})
}

// syncBuffer is a bytes.Buffer safe for the goroutines of the analysis and the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
