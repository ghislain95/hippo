package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
// clock at 10 January 2027, 00:00 UTC, one hour before the default schedule in Paris; the
// follow-up waits on a fake clock of its own at the same time.
type analysisEnv struct {
	st     *store.Store
	path   string
	clock  *scheduletest.Clock
	follow *scheduletest.Clock
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
	follow := scheduletest.NewClock(clock.Now())
	return &analysisEnv{
		st: st, path: path, clock: clock, follow: follow, ai: ai, userID: userID, logs: logs,
		opts: analysisOptions{
			Store: st, Analyzer: ai, Model: "claude-haiku-4-5", Prompt: p, BudgetUSD: 5,
			Prices:   analysis.Prices{InputPerMTok: 1, OutputPerMTok: 5},
			Schedule: daily, Location: paris, Assets: assets, Temp: tmp,
			CopierCommand: []string{os.Args[0], imagecopy.ChildCommand},
			Clock:         clock, FollowClock: follow, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		},
	}
}

// waiting captures a public link of the user and writes its extraction: it waits for the
// batch. It returns the id of the item.
func (e *analysisEnv) waiting(t *testing.T) string {
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
	return item.ID
}

// inProgress sends the item id in a batch accepted by the service, as a nightly pass before the
// restart left it, and returns the Anthropic id of the batch.
func (e *analysisEnv) inProgress(t *testing.T, id string) string {
	t.Helper()
	ctx := context.Background()
	batchID, moved, err := e.st.SubmitBatch(ctx, e.userID, store.NewBatch{
		Model: e.opts.Model, PromptVersion: e.opts.Prompt.Version, EnvelopeVersion: e.opts.Prompt.EnvelopeVersion,
	}, []string{id})
	if err != nil || len(moved) != 1 {
		t.Fatalf("SubmitBatch = %v, %v", moved, err)
	}
	anthropicID := "msgbatch_" + batchID
	if err := e.st.MarkBatchSent(ctx, e.userID, batchID, anthropicID); err != nil {
		t.Fatalf("MarkBatchSent: %v", err)
	}
	return anthropicID
}

// validResult is a valid response for the item id: a tip filed under the category Dev.
func (e *analysisEnv) validResult(t *testing.T, id string) analysis.Result {
	t.Helper()
	categories, err := e.st.Categories(context.Background(), e.userID)
	if err != nil {
		t.Fatal(err)
	}
	var dev int64
	for _, c := range categories {
		if c.Name == "Dev" {
			dev = c.ID
		}
	}
	input := fmt.Sprintf(`{"type":{"key":"tip"},"category":{"id":%d},"tags":["go","testing"],"title":"Un titre fictif",`+
		`"fiche":{"template":"tip","tip":"Une astuce fictive.","steps":["Faire ceci."],"materials":[],"warnings":[]},`+
		`"language":"en","freshness":"timeless","content_ok":true,"content_issue":null}`, dev)
	return analysis.Result{
		CustomID: id, Kind: analysis.ResultSucceeded, StopReason: "tool_use",
		ToolCalls: []analysis.ToolCall{{Name: "classify_item", Input: json.RawMessage(input)}},
		Usage:     analysis.Usage{Input: 100, Output: 10},
	}
}

// batchOf returns the user's batch whose Anthropic id is anthropicID.
func (e *analysisEnv) batchOf(t *testing.T, anthropicID string) store.Batch {
	t.Helper()
	batches, err := e.st.Batches(context.Background(), e.userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range batches {
		if b.AnthropicBatchID != nil && *b.AnthropicBatchID == anthropicID {
			return b
		}
	}
	t.Fatalf("no batch %s", anthropicID)
	return store.Batch{}
}

func (e *analysisEnv) status(t *testing.T, id string) string {
	t.Helper()
	item, err := e.st.Item(context.Background(), e.userID, id)
	if err != nil {
		t.Fatal(err)
	}
	return item.Status
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

// TestStartFollowUp follows openspec/changes/apply-batch-results/specs/item-analysis: the
// follow-up of the batches in progress at start-up and every 15 minutes.
func TestStartFollowUp(t *testing.T) {
	t.Run("A batch is followed after a restart", func(t *testing.T) {
		e := newAnalysisEnv(t)
		id := e.waiting(t)
		anthropicID := e.inProgress(t, id)
		e.ai.ScriptStates(anthropicID, analysistest.Ended(e.clock.Now()))
		e.ai.ScriptResults(anthropicID, analysistest.Download{Lines: analysistest.Lines(e.validResult(t, id))})
		e.start(t)
		e.follow.Wait(t) // the start-up follow-up has run: the loop waits for its first tick
		if got := e.status(t, id); got != "analyzed" {
			t.Errorf("item %s, want analyzed", got)
		}
		if b := e.batchOf(t, anthropicID); b.Status != store.BatchEnded || b.CostUSD == nil || *b.CostUSD <= 0 {
			t.Errorf("batch %s with cost %v, want ended with its cost", b.Status, b.CostUSD)
		}
	})

	t.Run("No API key, no follow-up", func(t *testing.T) {
		e := newAnalysisEnv(t)
		anthropicID := e.inProgress(t, e.waiting(t))
		e.opts.Analyzer = nil
		e.start(t)
		e.follow.Set(e.follow.Wait(t)) // 15 minutes pass
		e.follow.Wait(t)
		if n := e.ai.Calls(); n != 0 {
			t.Errorf("the AI service received %d calls, want none", n)
		}
		if b := e.batchOf(t, anthropicID); b.Status != store.BatchInProgress {
			t.Errorf("batch %s, want in progress", b.Status)
		}
	})

	t.Run("the start-up follow-up runs after the reconciliation", func(t *testing.T) {
		e := newAnalysisEnv(t)
		e.inProgress(t, e.waiting(t))
		id, err := e.st.NewID()
		if err != nil {
			t.Fatal(err)
		}
		storetest.InsertBatch(t, e.path, e.userID, storetest.Batch{
			ID: id, Status: "pending", ItemCount: 1, CreatedAt: time.Date(2027, 1, 9, 1, 0, 0, 0, time.UTC),
		})
		listed, release := make(chan struct{}, 1), make(chan struct{})
		e.ai.OnList(func(context.Context) {
			listed <- struct{}{}
			<-release
		})
		e.start(t)
		receive(t, listed, "the start-up reconciliation")
		time.Sleep(negativeWait)
		if n := e.ai.Gets(); n != 0 {
			t.Errorf("%d state queries during the reconciliation, want none", n)
		}
		close(release)
		e.follow.Wait(t)
		if n := e.ai.Gets(); n != 1 {
			t.Errorf("%d state queries after the reconciliation, want the batch in progress queried once", n)
		}
	})

	t.Run("A batch is followed every 15 minutes", func(t *testing.T) {
		e := newAnalysisEnv(t)
		id := e.waiting(t)
		anthropicID := e.inProgress(t, id)
		e.ai.ScriptStates(anthropicID, analysistest.Processing(), analysistest.Ended(e.clock.Now().Add(10*time.Minute)))
		e.ai.ScriptResults(anthropicID, analysistest.Download{Lines: analysistest.Lines(e.validResult(t, id))})
		e.start(t)
		deadline := e.follow.Wait(t)
		if n := e.ai.Gets(); n != 1 || e.status(t, id) != "submitted" {
			t.Errorf("at start-up: %d state queries, item %s; want 1, submitted", n, e.status(t, id))
		}
		if want := e.follow.Now().Add(15 * time.Minute); !deadline.Equal(want) {
			t.Errorf("the follow-up waits until %s, want %s", deadline, want)
		}
		e.follow.Set(deadline)
		e.follow.Wait(t)
		if n := e.ai.Gets(); n != 2 || e.status(t, id) != "analyzed" {
			t.Errorf("after the tick: %d state queries, item %s; want 2, analyzed", n, e.status(t, id))
		}
	})

	t.Run("stop waits for a follow-up in flight", func(t *testing.T) {
		e := newAnalysisEnv(t)
		id := e.waiting(t)
		anthropicID := e.inProgress(t, id)
		e.ai.ScriptStates(anthropicID, analysistest.Ended(e.clock.Now()))
		e.ai.ScriptResults(anthropicID, analysistest.Download{Lines: analysistest.Lines(e.validResult(t, id))})
		entered, gate := make(chan struct{}), make(chan struct{})
		e.ai.OnLine(func(ctx context.Context, _ int) {
			close(entered)
			<-ctx.Done()
			<-gate
		})
		stop := e.start(t)
		receive(t, entered, "the read of the results")
		stopped := make(chan struct{})
		go func() {
			stop()
			close(stopped)
		}()
		select {
		case <-stopped:
			t.Fatal("stop returned while the follow-up was reading")
		case <-time.After(negativeWait):
		}
		close(gate)
		receive(t, stopped, "the stop")
		if !strings.Contains(e.logs.String(), `"msg":"analysis follow-up"`) {
			t.Errorf("the follow-up did not finish before stop returned; logs:\n%s", e.logs)
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
