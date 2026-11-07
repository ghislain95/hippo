package analysis_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/analysistest"
	"github.com/ghislain95/hippo/backend/internal/analysis/anthropic"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// realClient is the Anthropic client against a local server that answers every batch ended and
// serves file as its results: the reader of design D3 end to end, without the network.
func realClient(t *testing.T, file string) *anthropic.Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/results") {
			w.Header().Set("Content-Type", "application/binary")
			_, _ = io.WriteString(w, file)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","type":"message_batch","processing_status":"ended","created_at":"2027-01-10T01:00:00Z",`+
			`"expires_at":"2027-01-11T01:00:00Z","archived_at":null,"cancel_initiated_at":null,"ended_at":"2027-01-10T01:56:00Z",`+
			`"results_url":null,"request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0}}`)
	}))
	t.Cleanup(s.Close)
	c, err := anthropic.New("sk-ant-test-key", anthropic.WithBaseURL(s.URL), anthropic.WithHTTPClient(s.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// jsonLine is the results line of a valid response of itemID, as the API writes it.
func jsonLine(t *testing.T, itemID string, in input) string {
	t.Helper()
	return fmt.Sprintf(`{"custom_id":%q,"result":{"type":"succeeded","message":{"id":"msg","type":"message","role":"assistant",`+
		`"model":"claude-haiku-4-5","content":[{"type":"tool_use","id":"toolu","name":"classify_item","input":%s}],`+
		`"stop_reason":"tool_use","stop_sequence":null,"usage":{"input_tokens":100,"output_tokens":10,`+
		`"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}}`, itemID, in.raw(t))
}

func TestResultReading(t *testing.T) {
	ctx := context.Background()

	t.Run("An unreadable line does not stop the read", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 3)
		in := tipInput(e.categoryID(t, e.userID, "Dev"))
		cfg := e.cfg
		cfg.Analyzer = realClient(t, jsonLine(t, ids[0], in)+"\n"+`{"custom_id":"`+ids[1]+`", not json`+"\n"+jsonLine(t, ids[2], in)+"\n")
		report := e.follow(t, cfg, e.userID, b)
		e.wantStatus(t, e.userID, ids[0], "analyzed")
		e.wantStatus(t, e.userID, ids[2], "analyzed")
		if it := e.item(t, e.userID, ids[1]); it.Status != "extracted" || it.Attempts != 0 || it.BatchID != nil {
			t.Errorf("item of the unreadable line %s with %d attempts, batch %v", it.Status, it.Attempts, it.BatchID)
		}
		if report.Outcome != "ended" || report.Counts.UnreadableLines != 1 || report.Counts.WithoutResult != 1 {
			t.Errorf("report %s %+v, want ended with one unreadable line", report.Outcome, report.Counts)
		}
	})

	t.Run("A line longer than 4 MiB is unreadable", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		in := tipInput(e.categoryID(t, e.userID, "Dev"))
		long := jsonLine(t, ids[0], in.with("title", strings.Repeat("x", anthropic.MaxResultLine)))
		cfg := e.cfg
		cfg.Analyzer = realClient(t, long+"\n"+jsonLine(t, ids[1], in)+"\n")
		report := e.follow(t, cfg, e.userID, b)
		e.wantStatus(t, e.userID, ids[0], "extracted")
		e.wantStatus(t, e.userID, ids[1], "analyzed")
		if report.Counts.UnreadableLines != 1 || report.Counts.Analyzed != 1 {
			t.Errorf("counts %+v, want one unreadable line and one analysed", report.Counts)
		}
	})

	t.Run("A result for an item of another batch is ignored", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		elsewhere := e.items(t, e.userID, 1)[0] // extracted, in no batch
		before := e.item(t, e.userID, elsewhere)
		in := tipInput(e.categoryID(t, e.userID, "Dev"))
		report := e.apply(t, b, valid(t, elsewhere, in), valid(t, ids[0], in))
		if after := e.item(t, e.userID, elsewhere); after.Status != "extracted" || after.ChangeSeq != before.ChangeSeq {
			t.Errorf("the other item is %s with change_seq %d, want unchanged", after.Status, after.ChangeSeq)
		}
		e.wantStatus(t, e.userID, ids[0], "analyzed")
		if report.Counts.Unknown != 1 {
			t.Errorf("counts %+v, want one unknown result", report.Counts)
		}
	})

	t.Run("A result for another user's item is ignored", func(t *testing.T) {
		e := newResultsEnv(t)
		other := e.user(t, "other")
		theirs := e.items(t, other, 1)
		theirBatch := e.sent(t, other, e.newBatch(), theirs...)
		b, ids := e.submitted(t, 1)
		beforeItem := e.item(t, other, theirs[0])
		beforeCategories, err := e.st.Categories(ctx, other)
		if err != nil {
			t.Fatal(err)
		}
		in := tipInput(0).with("category", kubernetes(e.categoryID(t, other, "Infra & DevOps"))).with("tags", []any{"alpha", "beta"})
		report := e.apply(t, b, valid(t, theirs[0], in), valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		after := e.item(t, other, theirs[0])
		if after.Status != "submitted" || after.ChangeSeq != beforeItem.ChangeSeq || *after.BatchID != theirBatch.ID {
			t.Errorf("the other user's item is %s, change_seq %d; want unchanged", after.Status, after.ChangeSeq)
		}
		afterCategories, err := e.st.Categories(ctx, other)
		if err != nil {
			t.Fatal(err)
		}
		sameRows := slices.EqualFunc(afterCategories, beforeCategories, func(a, b store.Category) bool {
			return a.ID == b.ID && a.Name == b.Name && a.Status == b.Status && a.ChangeSeq == b.ChangeSeq
		})
		if !sameRows || len(storetest.TagIDs(t, e.path, other)) != 0 {
			t.Error("the other user's categories or tags changed")
		}
		e.wantStatus(t, e.userID, ids[0], "analyzed")
		if report.Counts.Unknown != 1 || report.Counts.Analyzed != 1 {
			t.Errorf("counts %+v", report.Counts)
		}
	})

	t.Run("A repeated line is applied once", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		line := valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))
		var seqs []int64
		e.ai.OnLine(func(context.Context, int) { seqs = append(seqs, e.item(t, e.userID, ids[0]).ChangeSeq) })
		report := e.apply(t, b, line, line)
		it := e.item(t, e.userID, ids[0])
		if it.Status != "analyzed" || report.Counts.Analyzed != 1 || report.Counts.Unknown != 1 || report.Items[1].Outcome != "unknown" {
			t.Errorf("item %s, counts %+v; want analyzed once, the second line unknown", it.Status, report.Counts)
		}
		// The first line wrote the item (and its tags); the second wrote nothing.
		if len(seqs) != 2 || seqs[1] <= seqs[0] || it.ChangeSeq != seqs[1] {
			t.Errorf("change_seq before each line %v, after %d; want one advance, by the first line only", seqs, it.ChangeSeq)
		}
	})

	t.Run("An item without result goes back without attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[1], "submitted", nil, 1)
		report := e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		if it := e.item(t, e.userID, ids[1]); it.Status != "extracted" || it.BatchID != nil || it.Attempts != 1 {
			t.Errorf("item without result %s, batch %v, %d attempts", it.Status, it.BatchID, it.Attempts)
		}
		if report.Counts.WithoutResult != 1 {
			t.Errorf("counts %+v, want one without result", report.Counts)
		}
	})
}

func TestResultCost(t *testing.T) {
	t.Run("The cost follows the batch price", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		line := valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))
		line.Usage = analysis.Usage{Input: 10000, Output: 2000, CacheWrite5m: 4000, CacheRead: 36000}
		e.apply(t, b, line)
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.Input == nil || *c.Input != 10000 || *c.Output != 2000 || *c.CacheCreation != 4000 || *c.CacheRead != 36000 ||
			c.CostUSD == nil || !sameCost(*c.CostUSD, 0.0143) {
			t.Errorf("batch %+v, want the tokens and a cost of 0.0143", c)
		}
	})

	t.Run("A one-hour cache write costs twice the input price", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		line := valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))
		line.Usage = analysis.Usage{CacheWrite1h: 1000}
		e.apply(t, b, line)
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.CacheCreation == nil || *c.CacheCreation != 1000 || c.CostUSD == nil || !sameCost(*c.CostUSD, 0.001) {
			t.Errorf("batch %+v, want 1000 cache-write tokens and a cost of 0.001", c)
		}
	})

	t.Run("Every succeeded line counts in the tokens", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 6)
		dev := e.categoryID(t, e.userID, "Dev")
		storetest.DeleteItem(t, e.path, e.userID, ids[3])
		cut := valid(t, ids[2], tipInput(dev))
		cut.StopReason = "max_tokens"
		e.apply(t, b,
			valid(t, ids[0], tipInput(dev)),
			succeeded(ids[1]), // rejected: no tool call
			cut,
			valid(t, ids[3], tipInput(dev)),          // deleted item
			valid(t, "01UNKNOWNITEM", tipInput(dev)), // unknown item
			analysis.Result{CustomID: ids[4], Kind: analysis.ResultErrored, ErrorType: "api_error"},
			analysis.Result{CustomID: ids[5], Kind: analysis.ResultExpired})
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.Input == nil || *c.Input != 500 {
			t.Errorf("batch input tokens %v, want 500", c.Input)
		}
	})

	t.Run("A batch without any processed request costs nothing", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		e.apply(t, b, analysis.Result{CustomID: ids[0], Kind: analysis.ResultExpired}, analysis.Result{CustomID: ids[1], Kind: analysis.ResultExpired})
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.Status != "ended" || c.Input == nil || *c.Input != 0 || *c.Output != 0 || c.CostUSD == nil || *c.CostUSD != 0 {
			t.Errorf("batch %+v, want ended with zero tokens and cost 0", c)
		}
		e.run(t, e.cfg)
		if got := e.sentIDs(); len(got) != 2 {
			t.Errorf("the next pass sent %v, want both items", got)
		}
	})

	t.Run("The cost counts in the month the batch was sent", func(t *testing.T) {
		e := newResultsEnv(t)
		e.clock.Set(time.Date(2027, 1, 31, 22, 30, 0, 0, time.UTC)) // 23:30 in Paris
		b, ids := e.submitted(t, 1)
		e.clock.Set(time.Date(2027, 2, 1, 6, 0, 0, 0, time.UTC))
		line := valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))
		line.Usage = analysis.Usage{Output: 2_000_000} // 5.00 at the batch price
		e.apply(t, b, line)
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.CostUSD == nil || !sameCost(*c.CostUSD, 5) {
			t.Fatalf("batch cost %v, want 5.00", c.CostUSD)
		}
		e.items(t, e.userID, 1)
		e.clock.Set(time.Date(2027, 2, 2, 1, 0, 0, 0, time.UTC)) // 02:00 in Paris
		e.run(t, e.cfg)
		if len(e.ai.Bodies()) != 1 {
			t.Errorf("%d batches sent on 2 February, want 1", len(e.ai.Bodies()))
		}
	})
}

func TestUnavailableResults(t *testing.T) {
	t.Run("A batch unknown to the service is closed", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[1], "submitted", nil, 1)
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.NotFound())
		report := e.follow(t, e.cfg, e.userID, b)
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.Status != "ended" || c.CostUSD == nil || *c.CostUSD != 0 || !c.ResultsUnavailable {
			t.Errorf("batch %+v, want ended with cost 0, results unavailable", c)
		}
		for i, want := range []int64{0, 1} {
			if it := e.item(t, e.userID, ids[i]); it.Status != "extracted" || it.BatchID != nil || it.Attempts != want {
				t.Errorf("item %s, batch %v, %d attempts; want extracted with %d", it.Status, it.BatchID, it.Attempts, want)
			}
		}
		if report.Outcome != "results_unavailable" || report.Counts.WithoutResult != 2 {
			t.Errorf("report %s %+v, want results_unavailable", report.Outcome, report.Counts)
		}
	})

	t.Run("An archived batch is closed", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.Archived(e.clock.Now()))
		report := e.follow(t, e.cfg, e.userID, b)
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if c.Status != "ended" || c.CostUSD == nil || *c.CostUSD != 0 || report.Outcome != "results_unavailable" {
			t.Errorf("batch %+v, outcome %s; want ended with cost 0", c, report.Outcome)
		}
		if it := e.item(t, e.userID, ids[0]); it.Status != "extracted" || it.Attempts != 0 {
			t.Errorf("item %s with %d attempts", it.Status, it.Attempts)
		}
	})

	t.Run("Results that disappear after a cut read keep the tokens read", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		dev := e.categoryID(t, e.userID, "Dev")
		first := valid(t, ids[0], tipInput(dev))
		first.Usage = analysis.Usage{Input: 1000, Output: 200}
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.Ended(e.clock.Now()), analysistest.NotFound())
		e.ai.ScriptResults(*b.AnthropicBatchID, analysistest.Download{
			Lines: analysistest.Lines(first, valid(t, ids[1], tipInput(dev))), Cut: true, CutAfter: 1,
		})
		report := e.follow(t, e.cfg, e.userID, b)
		if report.Outcome != "read_failed" || report.ErrorType != "transport" {
			t.Errorf("first follow-up %s (%s), want read_failed", report.Outcome, report.ErrorType)
		}
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != "in_progress" || c.CostUSD != nil {
			t.Errorf("batch after the cut read %+v, want still in progress without cost", c)
		}
		report = e.follow(t, e.cfg, e.userID, b)
		e.wantStatus(t, e.userID, ids[0], "analyzed")
		if it := e.item(t, e.userID, ids[1]); it.Status != "extracted" || it.Attempts != 0 {
			t.Errorf("second item %s with %d attempts, want extracted without attempt", it.Status, it.Attempts)
		}
		c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		want := analysis.BatchCost(first.Usage, haiku)
		if c.Status != "ended" || c.Input == nil || *c.Input != 1000 || *c.Output != 200 || c.CostUSD == nil || !sameCost(*c.CostUSD, want) {
			t.Errorf("batch %+v, want the tokens and cost (%v) of the first line", c, want)
		}
		if report.Outcome != "results_unavailable" {
			t.Errorf("second follow-up %s, want results_unavailable", report.Outcome)
		}
	})

	t.Run("A closed batch no longer blocks the next night", func(t *testing.T) {
		e := newResultsEnv(t)
		b, _ := e.submitted(t, 1)
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.NotFound())
		e.follow(t, e.cfg, e.userID, b)
		waiting := e.items(t, e.userID, 1)
		e.run(t, e.cfg)
		if got := e.sentIDs(); !slices.Contains(got, waiting[0]) {
			t.Errorf("the next pass sent %v, want a batch with %s", got, waiting[0])
		}
	})
}

func TestFollowSendsNothing(t *testing.T) {
	t.Run("reading a batch never calls CreateBatch", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 3)
		private := e.items(t, e.userID, 1)
		if _, err := e.st.SetPrivate(context.Background(), e.userID, private[0], true); err != nil {
			t.Fatal(err)
		}
		dev := e.categoryID(t, e.userID, "Dev")
		cut := valid(t, ids[1], tipInput(dev))
		cut.StopReason = "max_tokens"
		e.apply(t, b, valid(t, ids[0], tipInput(dev)), cut, analysis.Result{CustomID: ids[2], Kind: analysis.ResultExpired})
		if e.ai.Creates() != 0 || e.ai.Lists() != 0 || len(e.ai.Bodies()) != 0 {
			t.Errorf("the follow-up made %d creates and %d lists, want none", e.ai.Creates(), e.ai.Lists())
		}
		if e.ai.Gets() != 1 || e.ai.Reads() != 1 {
			t.Errorf("%d state queries, %d downloads; want one of each", e.ai.Gets(), e.ai.Reads())
		}
	})

	t.Run("a batch still processing is not read", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		for _, state := range []analysistest.State{analysistest.Processing(), analysistest.Canceling()} {
			e.ai.ScriptStates(*b.AnthropicBatchID, state)
			if report := e.follow(t, e.cfg, e.userID, b); report.Outcome != "in_progress" {
				t.Errorf("outcome %s, want in_progress", report.Outcome)
			}
		}
		e.wantStatus(t, e.userID, ids[0], "submitted")
		if e.ai.Reads() != 0 || e.ai.Creates() != 0 {
			t.Errorf("%d downloads, %d creates; want none", e.ai.Reads(), e.ai.Creates())
		}
		if c := storetest.BatchClosingOf(t, e.path, e.userID, b.ID); c.Status != "in_progress" {
			t.Errorf("batch %s, want in progress", c.Status)
		}
	})

	t.Run("a failed query changes nothing and keeps the status and type only", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.QueryFailure(503, "overloaded_error"))
		report := e.follow(t, e.cfg, e.userID, b)
		if report.Outcome != "poll_failed" || report.Status != 503 || report.ErrorType != "overloaded_error" {
			t.Errorf("report %s %d %s, want poll_failed 503 overloaded_error", report.Outcome, report.Status, report.ErrorType)
		}
		e.wantStatus(t, e.userID, ids[0], "submitted")
	})

	t.Run("a batch closed already is not closed again", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		before := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		report := e.follow(t, e.cfg, e.userID, b) // a stale row: the store refuses a second closing
		if report.Outcome != "not_in_progress" {
			t.Errorf("outcome %s, want not_in_progress", report.Outcome)
		}
		after := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if after.Status != before.Status || *after.CostUSD != *before.CostUSD || *after.Input != *before.Input || *after.EndedAt != *before.EndedAt {
			t.Errorf("batch changed: %+v, was %+v", after, before)
		}
	})
}
