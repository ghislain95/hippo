package analysis_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/analysistest"
	"github.com/ghislain95/hippo/backend/internal/schedule/scheduletest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// The tests of this file run the follow-up of the batches (Pass.Follow, RunFollowUp and the
// follow-up at the start of the nightly pass) over the fake AI service, a real database and fake
// clocks (apply-batch-results, task 5.1).

// negativeWait is how long a test waits for something that must not happen.
const negativeWait = 200 * time.Millisecond

// newFollowPass returns a pass of cfg.
func newFollowPass(t *testing.T, cfg analysis.PassConfig) *analysis.Pass {
	t.Helper()
	pass, err := analysis.NewPass(cfg)
	if err != nil {
		t.Fatalf("NewPass: %v", err)
	}
	return pass
}

// followAll follows every user's batches once with a new pass of cfg, as at start-up.
func (e *resultsEnv) followAll(t *testing.T, cfg analysis.PassConfig) {
	t.Helper()
	if err := newFollowPass(t, cfg).Follow(context.Background()); err != nil {
		t.Fatalf("Follow: %v", err)
	}
}

// ticker drives RunFollowUp with its own fake clock: next is the deadline it waits for.
type ticker struct {
	clock *scheduletest.Clock
	next  time.Time
}

// runFollowUp starts RunFollowUp over pass, stopped at the end of the test, and returns its
// ticker once it waits for its first tick.
func runFollowUp(t *testing.T, pass *analysis.Pass, cfg analysis.PassConfig) *ticker {
	t.Helper()
	clock := scheduletest.NewClock(passStart)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		analysis.RunFollowUp(ctx, pass, clock, analysis.FollowInterval, cfg.Logger)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &ticker{clock: clock, next: clock.Wait(t)}
}

// tick moves the clock to the next tick and returns once the follow-up has run and waits again.
func (tk *ticker) tick(t *testing.T) {
	t.Helper()
	if want := tk.clock.Now().Add(analysis.FollowInterval); !tk.next.Equal(want) {
		t.Errorf("the follow-up waits until %s, want %s", tk.next, want)
	}
	tk.clock.Set(tk.next)
	tk.next = tk.clock.Wait(t)
}

// followLines are the follow-up lines of the batch batchID, in order.
func (e *env) followLines(t *testing.T, batchID string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range e.logLines(t) {
		if line["msg"] == "analysis follow-up" && line["batch_id"] == batchID {
			lines = append(lines, line)
		}
	}
	return lines
}

// lastFollowLine is the last follow-up line of the batch batchID.
func (e *env) lastFollowLine(t *testing.T, batchID string) map[string]any {
	t.Helper()
	lines := e.followLines(t, batchID)
	if len(lines) == 0 {
		t.Fatalf("no follow-up line of batch %s in:\n%s", batchID, e.logs)
	}
	return lines[len(lines)-1]
}

func TestFollowUp(t *testing.T) {
	ctx := context.Background()

	t.Run("A batch is followed every 15 minutes", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.Processing(), analysistest.Ended(e.clock.Now().Add(10*time.Minute)))
		e.ai.ScriptResults(*b.AnthropicBatchID, analysistest.Download{
			Lines: analysistest.Lines(valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))),
		})
		pass := newFollowPass(t, e.cfg)
		if err := pass.Follow(ctx); err != nil { // at start-up
			t.Fatal(err)
		}
		if e.ai.Gets() != 1 || e.ai.Reads() != 0 {
			t.Errorf("at start-up: %d state queries, %d downloads; want 1, 0", e.ai.Gets(), e.ai.Reads())
		}
		e.wantStatus(t, e.userID, ids[0], "submitted")

		tk := runFollowUp(t, pass, e.cfg)
		tk.tick(t)
		if e.ai.Gets() != 2 || e.ai.Reads() != 1 {
			t.Errorf("after the tick: %d state queries, %d downloads; want 2, 1", e.ai.Gets(), e.ai.Reads())
		}
		e.wantStatus(t, e.userID, ids[0], "analyzed")
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchEnded || c.CostUSD == nil {
			t.Errorf("batch %+v, want ended with its cost", c)
		}
	})

	t.Run("A batch still processing is left in progress", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.Processing(), analysistest.Canceling())
		e.followAll(t, e.cfg)
		e.followAll(t, e.cfg)
		e.wantStatus(t, e.userID, ids[0], "submitted")
		if e.ai.Gets() != 2 || e.ai.Reads() != 0 {
			t.Errorf("%d state queries, %d downloads; want 2, none", e.ai.Gets(), e.ai.Reads())
		}
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchInProgress || c.CostUSD != nil {
			t.Errorf("batch %+v, want in progress without cost", c)
		}
		for _, line := range e.followLines(t, b.ID) {
			if line["outcome"] != "in_progress" {
				t.Errorf("follow-up line %v, want the outcome in_progress", line)
			}
		}
	})

	t.Run("A failed query is retried at the next tick", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.QueryFailure(503, "overloaded_error"), analysistest.Ended(e.clock.Now()))
		e.ai.ScriptResults(*b.AnthropicBatchID, analysistest.Download{
			Lines: analysistest.Lines(valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))),
		})
		pass := newFollowPass(t, e.cfg)
		if err := pass.Follow(ctx); err != nil {
			t.Fatal(err)
		}
		e.wantStatus(t, e.userID, ids[0], "submitted")
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchInProgress || c.CostUSD != nil || c.Input != nil {
			t.Errorf("batch after the failed query %+v, want unchanged", c)
		}
		line := e.lastFollowLine(t, b.ID)
		if line["outcome"] != "poll_failed" || line["status"] != float64(503) || line["error_type"] != "overloaded_error" {
			t.Errorf("follow-up line %v, want poll_failed with the status 503", line)
		}

		tk := runFollowUp(t, pass, e.cfg)
		tk.tick(t)
		e.wantStatus(t, e.userID, ids[0], "analyzed")
		if line := e.lastFollowLine(t, b.ID); line["outcome"] != "ended" {
			t.Errorf("follow-up line after the tick %v, want ended", line)
		}
	})

	t.Run("Nothing to follow makes no call", func(t *testing.T) {
		e := newResultsEnv(t)
		endedBatch(t, e.passEnv, e.userID, 0, passStart.Add(-24*time.Hour), 0.01)
		storetest.InsertBatch(t, e.path, e.userID, storetest.Batch{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7F00", Status: store.BatchFailed, ItemCount: 1, CreatedAt: passStart.Add(-48 * time.Hour),
		})
		tk := runFollowUp(t, newFollowPass(t, e.cfg), e.cfg)
		tk.tick(t)
		if n := e.ai.Calls(); n != 0 {
			t.Errorf("the AI service received %d calls, want none", n)
		}
	})

	t.Run("without an API key, Follow makes no call and logs nothing", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		cfg := e.cfg
		cfg.Analyzer = nil
		tk := runFollowUp(t, newFollowPass(t, cfg), cfg)
		tk.tick(t)
		if n := e.ai.Calls(); n != 0 {
			t.Errorf("the AI service received %d calls, want none", n)
		}
		e.wantStatus(t, e.userID, ids[0], "submitted")
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchInProgress {
			t.Errorf("batch %s, want in progress", c.Status)
		}
		if logs := e.logs.String(); logs != "" {
			t.Errorf("a tick without an API key logged:\n%s", logs)
		}
	})

	t.Run("A batch that ended before the pass is applied before the guards", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.ended(b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		waiting := e.items(t, e.userID, 1)
		e.run(t, e.cfg)
		e.wantStatus(t, e.userID, ids[0], "analyzed")
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchEnded || c.CostUSD == nil {
			t.Errorf("batch %+v, want ended with its cost", c)
		}
		if got := e.sentIDs(); fmt.Sprint(got) != fmt.Sprint(waiting) {
			t.Errorf("the pass sent %v, want a new batch of %v", got, waiting)
		}
		e.wantLog(t, map[string]string{"user_id": e.userID, "outcome": "sent"})
	})

	t.Run("A restart in the middle of the read applies nothing twice", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 3)
		dev := e.categoryID(t, e.userID, "Dev")
		e.ended(b, valid(t, ids[0], tipInput(dev)), valid(t, ids[1], tipInput(dev)), valid(t, ids[2], tipInput(dev)))
		// The backend stops once the first item is written: before the second line is applied.
		stopped, stop := context.WithCancel(ctx)
		defer stop()
		e.ai.OnLine(func(_ context.Context, n int) {
			if n == 1 {
				stop()
			}
		})
		_ = newFollowPass(t, e.cfg).Follow(stopped) // stopped: its error is the context's
		first := e.item(t, e.userID, ids[0])
		if first.Status != "analyzed" {
			t.Fatalf("first item %s before the restart, want analyzed", first.Status)
		}
		e.wantStatus(t, e.userID, ids[1], "submitted")
		e.wantStatus(t, e.userID, ids[2], "submitted")
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchInProgress || c.CostUSD != nil {
			t.Errorf("batch after the stop %+v, want in progress without cost", c)
		}

		e.ai.OnLine(nil)
		e.clock.Advance(time.Hour)
		e.followAll(t, e.cfg) // the backend starts again
		again := e.item(t, e.userID, ids[0])
		if again.ChangeSeq != first.ChangeSeq || again.AnalyzedAt == nil || !again.AnalyzedAt.Equal(*first.AnalyzedAt) {
			t.Errorf("first item change_seq %d, analyzed_at %v; want %d, %v of its first write",
				again.ChangeSeq, again.AnalyzedAt, first.ChangeSeq, first.AnalyzedAt)
		}
		e.wantStatus(t, e.userID, ids[1], "analyzed")
		e.wantStatus(t, e.userID, ids[2], "analyzed")
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.Status != store.BatchEnded || c.Input == nil || *c.Input != 300 || c.CostUSD == nil {
			t.Errorf("batch %+v, want ended with the 300 input tokens of the three lines", c)
		}
		if line := e.lastFollowLine(t, b.ID); line["unknown"] != float64(1) || line["analyzed"] != float64(2) {
			t.Errorf("follow-up line %v, want 2 analysed and the first line unknown", line)
		}
	})

	t.Run("The cost is written once", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		before := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		calls := e.ai.Calls()
		e.followAll(t, e.cfg)
		if n := e.ai.Calls(); n != calls {
			t.Errorf("the AI service received %d more calls, want none", n-calls)
		}
		after := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if after.Status != before.Status || *after.CostUSD != *before.CostUSD || *after.Input != *before.Input || *after.Output != *before.Output {
			t.Errorf("batch %+v, was %+v; want unchanged", after, before)
		}
	})

	t.Run("a follow-up tick waits while the nightly pass holds the lock", func(t *testing.T) {
		e := newResultsEnv(t)
		e.items(t, e.userID, 1)
		entered, release := make(chan struct{}), make(chan struct{})
		e.ai.OnCreate(func(context.Context) {
			close(entered)
			<-release
		})
		pass := newFollowPass(t, e.cfg)
		tk := runFollowUp(t, pass, e.cfg)
		ran := make(chan error, 1)
		go func() { ran <- pass.Run(ctx) }()
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("the nightly pass never sent its batch")
		}

		tk.clock.Set(tk.next) // the tick fires while the pass sends
		time.Sleep(negativeWait)
		if n := e.ai.Gets(); n != 0 {
			t.Errorf("%d state queries during the send, want none: the follow-up did not wait for the lock", n)
		}
		close(release)
		if err := <-ran; err != nil {
			t.Fatalf("Run: %v", err)
		}
		tk.next = tk.clock.Wait(t) // the follow-up ran once the pass released the lock
		if n := e.ai.Gets(); n != 1 {
			t.Errorf("%d state queries after the send, want the batch just sent queried once", n)
		}
	})
}

// TestFollowStoreFailures covers a write of the store that fails while a batch is applied
// (design D8, As built tranche C; item-analysis « A valid response the store cannot write
// counts an attempt »).
func TestFollowStoreFailures(t *testing.T) {
	// wantStorageFailed checks that the item id is status with attempts and storage_failed, and
	// nothing of its response.
	wantStorageFailed := func(t *testing.T, e *resultsEnv, id, status string, attempts int64) {
		t.Helper()
		it := e.item(t, e.userID, id)
		if it.Status != status || it.Attempts != attempts || it.LastError == nil || *it.LastError != store.LastErrorStorageFailed {
			t.Errorf("item %s with %d attempts, last_error %v; want %s, %d, storage_failed", it.Status, it.Attempts, it.LastError, status, attempts)
		}
		if status == "extracted" && it.BatchID != nil {
			t.Errorf("item batch %v, want none", it.BatchID)
		}
		e.wantNothingWritten(t, it)
	}

	t.Run("A valid response the store refuses counts an attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		dev := e.categoryID(t, e.userID, "Dev")
		storetest.RefuseItemWrites(t, e.path, e.userID, ids[0], "analyzed")
		e.ended(b, valid(t, ids[0], tipInput(dev)), valid(t, ids[1], tipInput(dev)))
		e.followAll(t, e.cfg)

		wantStorageFailed(t, e, ids[0], "extracted", 1)
		e.wantStatus(t, e.userID, ids[1], "analyzed")
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchEnded || c.CostUSD == nil {
			t.Errorf("batch %+v, want ended with its cost", c)
		}
		line := e.lastFollowLine(t, b.ID)
		if line["outcome"] != "ended" || line["invalid"] != float64(1) || line["analyzed"] != float64(1) {
			t.Errorf("follow-up line %v, want ended with one invalid and one analysed", line)
		}
		if !e.hasLog(t, map[string]string{"msg": "analysis follow-up item", "item_id": ids[0], "outcome": "invalid", "error_type": "store"}) {
			t.Errorf("no item line with error_type store in:\n%s", e.logs)
		}
	})

	t.Run("The third refused write fails the item", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[0], "submitted", nil, 2)
		storetest.RefuseItemWrites(t, e.path, e.userID, ids[0], "analyzed")
		e.ended(b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		e.followAll(t, e.cfg)
		wantStorageFailed(t, e, ids[0], "failed", 3)
	})

	t.Run("A store that refuses every write cuts the read", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.RefuseItemWrites(t, e.path, e.userID, ids[0], "")
		e.ended(b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		e.followAll(t, e.cfg)

		e.wantStatus(t, e.userID, ids[0], "submitted")
		if it := e.item(t, e.userID, ids[0]); it.Attempts != 0 {
			t.Errorf("item with %d attempts, want none", it.Attempts)
		}
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.Status != store.BatchInProgress || c.CostUSD != nil || c.ResultsLinesRead == nil || *c.ResultsLinesRead != 1 {
			t.Errorf("batch %+v, want in progress, without cost, the cut read of one line recorded", c)
		}
		line := e.lastFollowLine(t, b.ID)
		if line["outcome"] != "read_failed" || line["error_type"] != "store" || line["level"] != "ERROR" {
			t.Errorf("follow-up line %v, want read_failed with error_type store at error level", line)
		}
		reads := e.ai.Reads()
		e.followAll(t, e.cfg) // the next follow-up
		if n := e.ai.Reads(); n != reads+1 {
			t.Errorf("%d downloads at the next follow-up, want the batch read again", n-reads)
		}
	})

	t.Run("a closing the store refuses is logged with the error type store", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		// The item without result is put back by the closing: refused, the closing fails.
		storetest.RefuseItemWrites(t, e.path, e.userID, ids[1], "")
		e.ended(b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		e.followAll(t, e.cfg)
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchInProgress || c.CostUSD != nil {
			t.Errorf("batch %+v, want still in progress without cost", c)
		}
		line := e.lastFollowLine(t, b.ID)
		if line["outcome"] != "read_failed" || line["error_type"] != "store" || line["level"] != "ERROR" {
			t.Errorf("follow-up line %v, want read_failed with error_type store at error level", line)
		}
	})

	t.Run("a closing as unavailable the store refuses is logged with the error type store", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.RefuseItemWrites(t, e.path, e.userID, ids[0], "")
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.NotFound())
		e.followAll(t, e.cfg)
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != store.BatchInProgress {
			t.Errorf("batch %s, want still in progress", c.Status)
		}
		if line := e.lastFollowLine(t, b.ID); line["outcome"] != "read_failed" || line["error_type"] != "store" {
			t.Errorf("follow-up line %v, want read_failed with error_type store", line)
		}
	})

	t.Run("a cut read the store cannot record is logged on its own", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		dev := e.categoryID(t, e.userID, "Dev")
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.Ended(e.clock.Now()))
		e.ai.ScriptResults(*b.AnthropicBatchID, analysistest.Download{
			Lines: analysistest.Lines(valid(t, ids[0], tipInput(dev)), valid(t, ids[1], tipInput(dev))), Cut: true, CutAfter: 1,
		})
		storetest.RefuseBatchWrites(t, e.path, e.userID, b.ID)
		e.followAll(t, e.cfg)
		line := e.lastFollowLine(t, b.ID)
		if line["outcome"] != "read_failed" || line["error_type"] != "transport" || line["error"] != nil {
			t.Errorf("follow-up line %v, want read_failed of the read's class, without an error text", line)
		}
		if !e.hasLog(t, map[string]string{"msg": "analysis follow-up: record the cut read", "batch_id": b.ID, "error_type": "store", "level": "ERROR"}) {
			t.Errorf("no line of the cut read not recorded in:\n%s", e.logs)
		}
		if strings.Contains(e.logs.String(), analysistest.ErrCut.Error()) {
			t.Errorf("the logs hold the read's error text:\n%s", e.logs)
		}
	})

	t.Run("a write cut by the bound of the read is a timeout", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		dev := e.categoryID(t, e.userID, "Dev")
		e.ended(b, valid(t, ids[0], tipInput(dev)), valid(t, ids[1], tipInput(dev)))
		cfg := e.cfg
		cfg.ReadTimeout = 50 * time.Millisecond
		// The bound passes while the second line is read: its write finds the context done.
		e.ai.OnLine(func(ctx context.Context, n int) {
			if n == 1 {
				<-ctx.Done()
			}
		})
		e.followAll(t, cfg)
		e.wantStatus(t, e.userID, ids[1], "submitted")
		if line := e.lastFollowLine(t, b.ID); line["outcome"] != "read_failed" || line["error_type"] != "timeout" {
			t.Errorf("follow-up line %v, want read_failed with error_type timeout", line)
		}
	})
}

// TestFollowedBatches checks which batches the follow-up asks for: every user's batches in
// progress, oldest first.
func TestFollowedBatches(t *testing.T) {
	t.Run("every user's batches in progress are followed, oldest first", func(t *testing.T) {
		e := newResultsEnv(t)
		other := e.user(t, "other")
		mine, _ := e.submitted(t, 1)
		theirs := e.sent(t, other, e.newBatch(), e.items(t, other, 1)...)
		e.followAll(t, e.cfg)
		var followed []string
		for _, line := range e.logLines(t) {
			if line["msg"] == "analysis follow-up" {
				followed = append(followed, fmt.Sprint(line["batch_id"]))
			}
		}
		if !slices.Contains(followed, mine.ID) || !slices.Contains(followed, theirs.ID) || len(followed) != 2 {
			t.Errorf("followed %v, want %s and %s", followed, mine.ID, theirs.ID)
		}
	})
}
