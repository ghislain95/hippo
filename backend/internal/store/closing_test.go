package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// fivePerMillion prices every token at 5 US dollars per million, cache tokens included: enough
// to tell which tokens a closing priced.
func fivePerMillion(tokens store.BatchTokens) float64 {
	return float64(tokens.Input+tokens.Output+tokens.CacheCreation+tokens.CacheRead) * 5 / 1e6
}

func TestCloseBatch(t *testing.T) {
	ctx := context.Background()
	tokens := store.BatchTokens{Input: 10000, Output: 2000, CacheCreation: 4000, CacheRead: 36000}

	t.Run("an item without result goes back to extracted without attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 3)
		storetest.SetAnalysisState(t, e.path, e.users[0], ids[1], "submitted", nil, 1)
		storetest.DeleteItem(t, e.path, e.users[0], ids[2])
		e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev")))
		before := e.item(t, e.users[0], ids[1])
		endedAt := time.Date(2026, 10, 28, 1, 56, 0, 0, time.UTC)

		closed, err := e.st.CloseBatch(ctx, e.users[0], batchID, tokens, 0.0143, endedAt)
		if err != nil {
			t.Fatalf("CloseBatch: %v", err)
		}
		if closed.Requeued != 1 || closed.Tokens != tokens || closed.CostUSD != 0.0143 {
			t.Errorf("CloseBatch = %+v, want 1 requeued with the tokens and cost given", closed)
		}
		after := e.item(t, e.users[0], ids[1])
		if after.Status != "extracted" || after.BatchID != nil || after.Attempts != 1 {
			t.Errorf("item without result = %s, batch %v, attempts %d; want extracted, no batch, attempts 1",
				after.Status, after.BatchID, after.Attempts)
		}
		e.assertWritten(t, before, after)
		if analysed := e.item(t, e.users[0], ids[0]); analysed.Status != "analyzed" {
			t.Errorf("analysed item = %s, want analyzed", analysed.Status)
		}
		row := store.BatchRowForTestOf(t, e.st, e.users[0], batchID)
		if row.Status != "ended" || *row.Input != 10000 || *row.Output != 2000 || *row.CacheCreation != 4000 ||
			*row.CacheRead != 36000 || *row.CostUSD != 0.0143 || *row.EndedAt != "2026-10-28T01:56:00.000Z" ||
			row.ResultsUnavailableAt != nil {
			t.Errorf("batch = %+v, want ended at 01:56 with the tokens and cost given, results available", row)
		}
	})

	t.Run("a batch already closed is not written again", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, _ := e.submitted(t, e.users[0], 1)
		if _, err := e.st.CloseBatch(ctx, e.users[0], batchID, tokens, 0.0143, time.Time{}); err != nil {
			t.Fatalf("CloseBatch: %v", err)
		}
		first := store.BatchRowForTestOf(t, e.st, e.users[0], batchID)
		if first.EndedAt == nil || *first.EndedAt != e.clock.Now().Format(store.TimeLayout) {
			t.Errorf("ended_at = %v, want now when Anthropic gave none", first.EndedAt)
		}
		_, err := e.st.CloseBatch(ctx, e.users[0], batchID, store.BatchTokens{Input: 1}, 1, time.Time{})
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("second CloseBatch error = %v, want ErrNotFound", err)
		}
		_, err = e.st.CloseBatchUnavailable(ctx, e.users[0], batchID, fivePerMillion)
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("CloseBatchUnavailable after CloseBatch error = %v, want ErrNotFound", err)
		}
		if again := store.BatchRowForTestOf(t, e.st, e.users[0], batchID); *again.CostUSD != 0.0143 || *again.Input != 10000 {
			t.Errorf("batch after the second closing = %+v, want the first cost and tokens", again)
		}
	})

	t.Run("another user's batch, a pending or a failed batch is not closed", func(t *testing.T) {
		e := newResultsEnv(t)
		otherBatch, otherIDs := e.submitted(t, e.users[1], 1)
		if _, err := e.st.CloseBatch(ctx, e.users[0], otherBatch, tokens, 1, time.Time{}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("CloseBatch of another user's batch error = %v, want ErrNotFound", err)
		}
		if got := e.item(t, e.users[1], otherIDs[0]); got.Status != "submitted" {
			t.Errorf("other user's item = %s, want submitted", got.Status)
		}
		for _, status := range []string{"pending", "failed"} {
			id := fmt.Sprintf("01JB8Z6Q4M1X2Y3Z4A5B6C7D%s", map[string]string{"pending": "P0", "failed": "F0"}[status])
			storetest.InsertBatch(t, e.path, e.users[0], storetest.Batch{ID: id, Status: status, ItemCount: 1, CreatedAt: e.clock.Now()})
			if _, err := e.st.CloseBatch(ctx, e.users[0], id, tokens, 1, time.Time{}); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("CloseBatch of a %s batch error = %v, want ErrNotFound", status, err)
			}
		}
	})

	t.Run("an ended batch without cost is closed", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, _ := e.submitted(t, e.users[0], 1)
		storetest.SetBatchState(t, e.path, e.users[0], batchID, "ended", nil)
		if _, err := e.st.CloseBatch(ctx, e.users[0], batchID, tokens, 0.5, time.Time{}); err != nil {
			t.Fatalf("CloseBatch: %v", err)
		}
		if row := store.BatchRowForTestOf(t, e.st, e.users[0], batchID); row.CostUSD == nil || *row.CostUSD != 0.5 {
			t.Errorf("cost = %v, want 0.5", row.CostUSD)
		}
	})
}

func TestCloseBatchUnavailable(t *testing.T) {
	ctx := context.Background()

	t.Run("a batch closed without any read costs nothing and requeues its items", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 2)
		storetest.SetAnalysisState(t, e.path, e.users[0], ids[0], "submitted", nil, 1)

		closed, err := e.st.CloseBatchUnavailable(ctx, e.users[0], batchID, fivePerMillion)
		if err != nil {
			t.Fatalf("CloseBatchUnavailable: %v", err)
		}
		if closed.Requeued != 2 || closed.Tokens != (store.BatchTokens{}) || closed.CostUSD != 0 {
			t.Errorf("CloseBatchUnavailable = %+v, want 2 requeued, zero tokens and cost", closed)
		}
		for i, want := range []int64{1, 0} {
			if got := e.item(t, e.users[0], ids[i]); got.Status != "extracted" || got.BatchID != nil || got.Attempts != want {
				t.Errorf("item %d = %s, batch %v, attempts %d; want extracted, no batch, attempts %d", i, got.Status, got.BatchID, got.Attempts, want)
			}
		}
		row := store.BatchRowForTestOf(t, e.st, e.users[0], batchID)
		now := e.clock.Now().Format(store.TimeLayout)
		if row.Status != "ended" || *row.CostUSD != 0 || *row.Input != 0 || *row.Output != 0 || *row.CacheCreation != 0 ||
			*row.CacheRead != 0 || row.ResultsUnavailableAt == nil || *row.ResultsUnavailableAt != now || *row.EndedAt != now {
			t.Errorf("batch = %+v, want ended now with zero tokens and cost, results unavailable now", row)
		}
		if blocked, err := e.st.HasBatchWithoutCost(ctx, e.users[0]); err != nil || blocked {
			t.Errorf("HasBatchWithoutCost = %v, %v; want false", blocked, err)
		}
	})

	t.Run("the most complete cut read is kept and priced at the closing", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, _ := e.submitted(t, e.users[0], 2)
		record := func(lines int, tokens store.BatchTokens) bool {
			t.Helper()
			written, err := e.st.RecordCutRead(ctx, e.users[0], batchID, tokens, lines)
			if err != nil {
				t.Fatalf("RecordCutRead: %v", err)
			}
			return written
		}
		if !record(2, store.BatchTokens{Input: 200, Output: 20, CacheCreation: 2, CacheRead: 1}) {
			t.Error("a first cut read was not recorded")
		}
		if record(1, store.BatchTokens{Input: 100}) {
			t.Error("a shorter cut read replaced a longer one")
		}
		if record(2, store.BatchTokens{Input: 999}) {
			t.Error("a cut read of the same length replaced the recorded one")
		}
		row := store.BatchRowForTestOf(t, e.st, e.users[0], batchID)
		if row.Status != "in_progress" || row.CostUSD != nil || *row.ResultsLinesRead != 2 || *row.Input != 200 {
			t.Errorf("batch after the cut reads = %+v, want in progress without cost, 2 lines and 200 input tokens", row)
		}
		if blocked, err := e.st.HasBatchWithoutCost(ctx, e.users[0]); err != nil || !blocked {
			t.Errorf("HasBatchWithoutCost after a cut read = %v, %v; want true", blocked, err)
		}

		closed, err := e.st.CloseBatchUnavailable(ctx, e.users[0], batchID, fivePerMillion)
		if err != nil {
			t.Fatalf("CloseBatchUnavailable: %v", err)
		}
		want := store.BatchTokens{Input: 200, Output: 20, CacheCreation: 2, CacheRead: 1}
		if closed.Tokens != want || closed.CostUSD != fivePerMillion(want) {
			t.Errorf("CloseBatchUnavailable = %+v, want the recorded tokens and their cost", closed)
		}
		if row := store.BatchRowForTestOf(t, e.st, e.users[0], batchID); *row.CostUSD != fivePerMillion(want) || *row.Input != 200 {
			t.Errorf("batch = %+v, want the recorded tokens and their cost", row)
		}
		if record(3, store.BatchTokens{Input: 1}) {
			t.Error("a cut read was recorded on a closed batch")
		}
	})

	t.Run("a full read after a cut read writes the full sums", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, _ := e.submitted(t, e.users[0], 2)
		if _, err := e.st.RecordCutRead(ctx, e.users[0], batchID, store.BatchTokens{Input: 100}, 1); err != nil {
			t.Fatalf("RecordCutRead: %v", err)
		}
		full := store.BatchTokens{Input: 300, Output: 30}
		if _, err := e.st.CloseBatch(ctx, e.users[0], batchID, full, 0.25, time.Time{}); err != nil {
			t.Fatalf("CloseBatch: %v", err)
		}
		if row := store.BatchRowForTestOf(t, e.st, e.users[0], batchID); *row.Input != 300 || *row.Output != 30 || *row.CostUSD != 0.25 {
			t.Errorf("batch = %+v, want the full sums", row)
		}
	})

	t.Run("another user's batch is not recorded nor closed", func(t *testing.T) {
		e := newResultsEnv(t)
		otherBatch, _ := e.submitted(t, e.users[1], 1)
		if written, err := e.st.RecordCutRead(ctx, e.users[0], otherBatch, store.BatchTokens{Input: 1}, 1); err != nil || written {
			t.Errorf("RecordCutRead of another user's batch = %v, %v; want not written", written, err)
		}
		if _, err := e.st.CloseBatchUnavailable(ctx, e.users[0], otherBatch, fivePerMillion); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("CloseBatchUnavailable of another user's batch error = %v, want ErrNotFound", err)
		}
	})
}

func TestBatchesToFollow(t *testing.T) {
	ctx := context.Background()

	t.Run("lists the user's batches in progress and ended without cost, oldest first", func(t *testing.T) {
		e := newResultsEnv(t)
		cost := 0.1
		created := e.clock.Now()
		insert := func(userID, id, status string, cost *float64, at time.Duration) {
			anthropicID := "msgbatch_" + id
			storetest.InsertBatch(t, e.path, userID, storetest.Batch{
				ID: id, Status: status, AnthropicBatchID: &anthropicID, ItemCount: 1, CostUSD: cost, CreatedAt: created.Add(at),
			})
		}
		insert(e.users[0], "01JB8Z6Q4M1X2Y3Z4A5B6C7DC3", "in_progress", nil, 3*time.Hour)
		insert(e.users[0], "01JB8Z6Q4M1X2Y3Z4A5B6C7DC1", "ended", nil, time.Hour)
		insert(e.users[0], "01JB8Z6Q4M1X2Y3Z4A5B6C7DC2", "ended", &cost, 2*time.Hour)
		insert(e.users[0], "01JB8Z6Q4M1X2Y3Z4A5B6C7DC4", "failed", nil, 4*time.Hour)
		storetest.InsertBatch(t, e.path, e.users[0], storetest.Batch{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7DC5", Status: "pending", ItemCount: 1, CreatedAt: created,
		})
		insert(e.users[1], "01JB8Z6Q4M1X2Y3Z4A5B6C7DC6", "in_progress", nil, 0)

		batches, err := e.st.BatchesToFollow(ctx, e.users[0])
		if err != nil {
			t.Fatalf("BatchesToFollow: %v", err)
		}
		var ids []string
		for _, b := range batches {
			ids = append(ids, b.ID)
		}
		want := []string{"01JB8Z6Q4M1X2Y3Z4A5B6C7DC1", "01JB8Z6Q4M1X2Y3Z4A5B6C7DC3"}
		if !slices.Equal(ids, want) {
			t.Errorf("BatchesToFollow = %v, want %v", ids, want)
		}
		if len(batches) == 2 && (batches[1].AnthropicBatchID == nil || *batches[1].AnthropicBatchID != "msgbatch_01JB8Z6Q4M1X2Y3Z4A5B6C7DC3" ||
			batches[1].EnvelopeVersion != 1 || batches[1].Model != "claude-haiku-test") {
			t.Errorf("batch in progress = %+v, want its Anthropic id and versions", batches[1])
		}
	})

	t.Run("a user without batch to follow gets none", func(t *testing.T) {
		e := newResultsEnv(t)
		batches, err := e.st.BatchesToFollow(ctx, e.users[0])
		if err != nil || len(batches) != 0 {
			t.Errorf("BatchesToFollow = %v, %v; want none", batches, err)
		}
	})
}

func TestReferentielTags(t *testing.T) {
	ctx := context.Background()

	// carried captures count notes of userID, each carrying the tags names (created once).
	carried := func(t *testing.T, e *resultsEnv, userID string, tagIDs map[string]int64, count int, names ...string) []string {
		t.Helper()
		var ids []string
		for range count {
			e.n++
			n, _ := capture(t, e.st, userID, note(fmt.Sprintf("Note %d", e.n)), keepNote)
			for _, name := range names {
				id, ok := tagIDs[name]
				if !ok {
					id = storetest.AddTag(t, e.path, userID, name)
					tagIDs[name] = id
				}
				storetest.AttachTag(t, e.path, userID, n.ID, id)
			}
			ids = append(ids, n.ID)
		}
		return ids
	}
	tagsOf := func(t *testing.T, e *resultsEnv, userID string, limit int) []string {
		t.Helper()
		tags, err := e.st.ReferentielTags(ctx, userID, limit)
		if err != nil {
			t.Fatalf("ReferentielTags: %v", err)
		}
		return tags
	}

	t.Run("Tags are listed most used first, then by name", func(t *testing.T) {
		e := newResultsEnv(t)
		ids := map[string]int64{}
		carried(t, e, e.users[0], ids, 1, "docker")
		carried(t, e, e.users[0], ids, 3, "go")
		carried(t, e, e.users[0], ids, 1, "ansible")
		if got := tagsOf(t, e, e.users[0], 300); !slices.Equal(got, []string{"go", "ansible", "docker"}) {
			t.Errorf("ReferentielTags = %v, want [go ansible docker]", got)
		}
	})

	t.Run("At most the limit of tags is listed", func(t *testing.T) {
		e := newResultsEnv(t)
		ids := map[string]int64{}
		carried(t, e, e.users[0], ids, 1, "zzz-rare")
		carried(t, e, e.users[0], ids, 2, "a1", "a2", "a3")
		if got := tagsOf(t, e, e.users[0], 3); !slices.Equal(got, []string{"a1", "a2", "a3"}) {
			t.Errorf("ReferentielTags = %v, want [a1 a2 a3] without zzz-rare", got)
		}
	})

	t.Run("A tag carried only by private items is not listed", func(t *testing.T) {
		e := newResultsEnv(t)
		ids := map[string]int64{}
		private := carried(t, e, e.users[0], ids, 1, "sante")
		carried(t, e, e.users[0], ids, 1, "go")
		if _, err := e.st.SetPrivate(ctx, e.users[0], private[0], true); err != nil {
			t.Fatalf("SetPrivate: %v", err)
		}
		if got := tagsOf(t, e, e.users[0], 300); !slices.Equal(got, []string{"go"}) {
			t.Errorf("ReferentielTags = %v, want [go]", got)
		}
	})

	t.Run("A tag carried only by deleted items is not listed", func(t *testing.T) {
		e := newResultsEnv(t)
		ids := map[string]int64{}
		deleted := carried(t, e, e.users[0], ids, 1, "ancien")
		carried(t, e, e.users[0], ids, 1, "go")
		storetest.DeleteItem(t, e.path, e.users[0], deleted[0])
		if got := tagsOf(t, e, e.users[0], 300); !slices.Equal(got, []string{"go"}) {
			t.Errorf("ReferentielTags = %v, want [go]", got)
		}
	})

	t.Run("A deleted tag is not listed", func(t *testing.T) {
		e := newResultsEnv(t)
		ids := map[string]int64{}
		carried(t, e, e.users[0], ids, 1, "golang", "go")
		storetest.DeleteTag(t, e.path, e.users[0], ids["golang"])
		if got := tagsOf(t, e, e.users[0], 300); !slices.Equal(got, []string{"go"}) {
			t.Errorf("ReferentielTags = %v, want [go]", got)
		}
	})

	t.Run("Another user's tags are never listed", func(t *testing.T) {
		e := newResultsEnv(t)
		carried(t, e, e.users[1], map[string]int64{}, 1, "jardin-secret")
		carried(t, e, e.users[0], map[string]int64{}, 1, "go")
		if got := tagsOf(t, e, e.users[0], 300); !slices.Equal(got, []string{"go"}) {
			t.Errorf("ReferentielTags = %v, want [go]", got)
		}
	})

	t.Run("a user without any tag gets none", func(t *testing.T) {
		e := newResultsEnv(t)
		storetest.AddTag(t, e.path, e.users[0], "unused")
		if got := tagsOf(t, e, e.users[0], 300); len(got) != 0 {
			t.Errorf("ReferentielTags = %v, want none (a tag on no item is not listed)", got)
		}
	})
}

func TestItemClassification(t *testing.T) {
	t.Run("an analysed item carries its type, category, tags and fiche", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		dev := e.categoryID(t, e.users[0], "Dev")
		storetest.AddTag(t, e.path, e.users[0], "zeta")
		// testing is created before go: the ids are ascending, not in the response's order.
		e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(dev, "testing", "go"))

		got := e.item(t, e.users[0], ids[0])
		tags := store.TagRowsForTest(t, e.st, e.users[0])
		if *got.TypeKey != "tip" || *got.CategoryID != dev || !slices.Equal(got.TagIDs, []int64{tags[1].ID, tags[2].ID}) ||
			!json.Valid(got.FicheJSON) || string(got.FicheJSON) != string(tipFiche) || *got.EnvelopeVersion != 1 {
			t.Errorf("item = type %v, category %v, tags %v, fiche %s, envelope %v; want tip, Dev, [%d %d], the fiche, 1",
				got.TypeKey, got.CategoryID, got.TagIDs, got.FicheJSON, got.EnvelopeVersion, tags[1].ID, tags[2].ID)
		}
	})

	t.Run("an item without analysis has no classification", func(t *testing.T) {
		e := newResultsEnv(t)
		_, ids := e.submitted(t, e.users[0], 1)
		got := e.item(t, e.users[0], ids[0])
		if got.TypeKey != nil || got.CategoryID != nil || got.TagIDs == nil || len(got.TagIDs) != 0 || got.FicheJSON != nil ||
			got.EnvelopeVersion != nil {
			t.Errorf("item = type %v, category %v, tags %#v, fiche %s, envelope %v; want nil, nil, [], none, nil",
				got.TypeKey, got.CategoryID, got.TagIDs, got.FicheJSON, got.EnvelopeVersion)
		}
	})

	t.Run("a deleted tag is not carried", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev"), "go", "golang"))
		storetest.DeleteTag(t, e.path, e.users[0], got.TagIDs[1])
		if after := e.item(t, e.users[0], ids[0]); !slices.Equal(after.TagIDs, got.TagIDs[:1]) {
			t.Errorf("TagIDs = %v, want %v", after.TagIDs, got.TagIDs[:1])
		}
	})
}
