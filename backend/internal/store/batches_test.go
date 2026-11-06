package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

var testBatch = store.NewBatch{Model: "claude-haiku-4-5", PromptVersion: 3, EnvelopeVersion: 1}

// twoUsers opens a store with a clock and two users, each with two extracted links.
func twoUsers(t *testing.T) (st *store.Store, path string, clock *testClock, users [2]string, items [2][]string) {
	t.Helper()
	clock = newClock()
	st, path = storetest.OpenWithClock(t, clock.Now)
	full := store.Extraction{Mode: "full", ContentText: ptr("Body")}
	for i, name := range []string{"owner", "other"} {
		users[i] = newUser(t, st, name)
		for j := range 2 {
			clock.Advance(time.Second)
			item := extracted(t, st, users[i], link(fmt.Sprintf("https://example.com/%s/%d", name, j)), full)
			items[i] = append(items[i], item.ID)
		}
	}
	return st, path, clock, users, items
}

func item(t *testing.T, st *store.Store, userID, id string) store.Item {
	t.Helper()
	it, err := st.Item(context.Background(), userID, id)
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	return it
}

func batches(t *testing.T, st *store.Store, userID string) []store.Batch {
	t.Helper()
	b, err := st.Batches(context.Background(), userID)
	if err != nil {
		t.Fatalf("Batches: %v", err)
	}
	return b
}

func TestSubmitBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("moves the items to submitted with one change_seq each and a pending batch", func(t *testing.T) {
		st, _, clock, users, items := twoUsers(t)
		before := []store.Item{item(t, st, users[0], items[0][0]), item(t, st, users[0], items[0][1])}
		seq := store.SequenceForTest(t, st)
		clock.Advance(time.Minute)

		batchID, moved, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil || batchID == "" {
			t.Fatalf("SubmitBatch = %q, %v", batchID, err)
		}
		if fmt.Sprint(moved) != fmt.Sprint(items[0]) {
			t.Errorf("moved %v, want %v", moved, items[0])
		}
		if got := store.SequenceForTest(t, st); got != seq+2 {
			t.Errorf("sequence advanced by %d, want 2 (one per item)", got-seq)
		}
		seqs := map[int64]bool{}
		for i, id := range items[0] {
			after := item(t, st, users[0], id)
			if after.Status != "submitted" || after.BatchID == nil || *after.BatchID != batchID ||
				after.ChangeSeq <= before[i].ChangeSeq || !after.UpdatedAt.After(before[i].UpdatedAt) || after.Attempts != before[i].Attempts {
				t.Errorf("item %d = %s, batch %v, seq %d→%d, attempts %d", i, after.Status, after.BatchID, before[i].ChangeSeq, after.ChangeSeq, after.Attempts)
			}
			seqs[after.ChangeSeq] = true
		}
		if len(seqs) != 2 {
			t.Error("the two items share a change_seq")
		}
		got := batches(t, st, users[0])
		if len(got) != 1 || got[0].ID != batchID || got[0].Status != store.BatchPending || got[0].ItemCount != 2 ||
			got[0].Model != testBatch.Model || got[0].PromptVersion != 3 || got[0].EnvelopeVersion != 1 ||
			got[0].AnthropicBatchID != nil || !got[0].CreatedAt.Equal(clock.Now()) {
			t.Errorf("batches = %+v", got)
		}
	})

	t.Run("keeps only the items still extracted, public and not deleted", func(t *testing.T) {
		st, path, _, users, items := twoUsers(t)
		full := store.Extraction{Mode: "full", ContentText: ptr("Body")}
		deleted := extracted(t, st, users[0], link("https://example.com/deleted"), full)
		storetest.DeleteItem(t, path, users[0], deleted.ID)
		if _, err := st.SetPrivate(ctx, users[0], items[0][1], true); err != nil {
			t.Fatal(err)
		}
		queued, _ := capture(t, st, users[0], link("https://example.com/queued"), keepNote)

		_, moved, err := st.SubmitBatch(ctx, users[0], testBatch,
			[]string{items[0][0], items[0][1], deleted.ID, queued.ID, items[1][0], "01JB8Z6Q4M1X2Y3Z4A5B6C7D99"})
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(moved) != fmt.Sprint(items[0][:1]) {
			t.Errorf("moved %v, want only %v", moved, items[0][:1])
		}
		if it := item(t, st, users[0], items[0][1]); it.Status != "extracted" || it.BatchID != nil {
			t.Errorf("the private item is %s, batch %v; want extracted without batch", it.Status, it.BatchID)
		}
		if it := item(t, st, users[1], items[1][0]); it.Status != "extracted" {
			t.Errorf("the other user's item is %s, want extracted", it.Status)
		}
		if got := batches(t, st, users[0]); len(got) != 1 || got[0].ItemCount != 1 {
			t.Errorf("batches = %+v, want one of 1 item", got)
		}
	})

	t.Run("writes nothing when no item can move", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		seq := store.SequenceForTest(t, st)
		batchID, moved, err := st.SubmitBatch(ctx, users[0], testBatch, items[1])
		if err != nil || batchID != "" || len(moved) != 0 {
			t.Errorf("SubmitBatch of another user's items = %q, %v, %v; want nothing", batchID, moved, err)
		}
		if store.SequenceForTest(t, st) != seq || len(batches(t, st, users[0])) != 0 || len(batches(t, st, users[1])) != 0 {
			t.Error("something was written")
		}
	})

	t.Run("refuses more than 200 items", func(t *testing.T) {
		st, _, _, users, _ := twoUsers(t)
		ids := make([]string, 201)
		for i := range ids {
			ids[i] = fmt.Sprintf("%026d", i)
		}
		if _, _, err := st.SubmitBatch(ctx, users[0], testBatch, ids); err == nil {
			t.Error("SubmitBatch accepted 201 items")
		}
	})
}

func TestMarkBatchSent(t *testing.T) {
	ctx := context.Background()

	t.Run("records the Anthropic id and puts the batch in progress", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := st.MarkBatchSent(ctx, users[0], batchID, "msgbatch_01"); err != nil {
			t.Fatalf("MarkBatchSent: %v", err)
		}
		got := batches(t, st, users[0])
		if len(got) != 1 || got[0].Status != store.BatchInProgress || got[0].AnthropicBatchID == nil ||
			*got[0].AnthropicBatchID != "msgbatch_01" || got[0].SentAt == nil {
			t.Errorf("batch = %+v", got)
		}
		if err := st.MarkBatchSent(ctx, users[0], batchID, "msgbatch_02"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("second MarkBatchSent = %v, want ErrNotFound", err)
		}
	})

	t.Run("does not touch another user's batch", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := st.MarkBatchSent(ctx, users[1], batchID, "msgbatch_01"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("MarkBatchSent by another user = %v, want ErrNotFound", err)
		}
		if got := batches(t, st, users[0]); got[0].Status != store.BatchPending {
			t.Errorf("batch is %s, want pending", got[0].Status)
		}
	})

	t.Run("reports an Anthropic id already recorded and leaves the batch pending", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		first, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0][:1])
		if err != nil {
			t.Fatal(err)
		}
		second, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0][1:])
		if err != nil {
			t.Fatal(err)
		}
		if err := st.MarkBatchSent(ctx, users[0], first, "msgbatch_01"); err != nil {
			t.Fatal(err)
		}
		if err := st.MarkBatchSent(ctx, users[0], second, "msgbatch_01"); !errors.Is(err, store.ErrAnthropicBatchIDRecorded) {
			t.Errorf("MarkBatchSent of a recorded id = %v, want ErrAnthropicBatchIDRecorded", err)
		}
		if got := batches(t, st, users[0]); got[1].Status != store.BatchPending || got[1].AnthropicBatchID != nil {
			t.Errorf("second batch = %+v, want pending without id", got[1])
		}
	})
}

func TestFailBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("puts the items back to extracted without batch nor attempt, and fails the batch", func(t *testing.T) {
		st, path, clock, users, items := twoUsers(t)
		storetest.SetAnalysisState(t, path, users[0], items[0][0], "extracted", nil, 1)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		submitted := []store.Item{item(t, st, users[0], items[0][0]), item(t, st, users[0], items[0][1])}
		clock.Advance(time.Minute)

		n, err := st.FailBatch(ctx, users[0], batchID)
		if err != nil || n != 2 {
			t.Fatalf("FailBatch = %d, %v; want 2", n, err)
		}
		for i, id := range items[0] {
			after := item(t, st, users[0], id)
			if after.Status != "extracted" || after.BatchID != nil || after.ChangeSeq <= submitted[i].ChangeSeq ||
				!after.UpdatedAt.After(submitted[i].UpdatedAt) || after.Attempts != submitted[i].Attempts {
				t.Errorf("item %d = %s, batch %v, seq %d→%d, attempts %d→%d", i, after.Status, after.BatchID,
					submitted[i].ChangeSeq, after.ChangeSeq, submitted[i].Attempts, after.Attempts)
			}
		}
		if item(t, st, users[0], items[0][0]).Attempts != 1 {
			t.Error("the attempts changed")
		}
		if got := batches(t, st, users[0]); got[0].Status != store.BatchFailed {
			t.Errorf("batch is %s, want failed", got[0].Status)
		}
		if _, err := st.FailBatch(ctx, users[0], batchID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("second FailBatch = %v, want ErrNotFound", err)
		}
	})

	t.Run("does not touch another user's batch nor items", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.FailBatch(ctx, users[1], batchID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("FailBatch by another user = %v, want ErrNotFound", err)
		}
		if it := item(t, st, users[0], items[0][0]); it.Status != "submitted" {
			t.Errorf("item is %s, want submitted", it.Status)
		}
	})

	t.Run("refuses a batch already in progress", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := st.MarkBatchSent(ctx, users[0], batchID, "msgbatch_01"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.FailBatch(ctx, users[0], batchID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("FailBatch of a batch in progress = %v, want ErrNotFound", err)
		}
		if it := item(t, st, users[0], items[0][0]); it.Status != "submitted" {
			t.Errorf("item is %s, want submitted", it.Status)
		}
	})
}

func TestPendingBatches(t *testing.T) {
	ctx := context.Background()

	t.Run("lists the user's pending batches only, oldest first", func(t *testing.T) {
		st, path, clock, users, items := twoUsers(t)
		clock.Advance(time.Minute)
		newer, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0][1:])
		if err != nil {
			t.Fatal(err)
		}
		clock.Advance(-30 * time.Second)
		older, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0][:1])
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.SubmitBatch(ctx, users[1], testBatch, items[1]); err != nil {
			t.Fatal(err)
		}
		storetest.InsertBatch(t, path, users[0], storetest.Batch{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7DA0", Status: "in_progress", AnthropicBatchID: ptr("msgbatch_x"), ItemCount: 1, CreatedAt: clock.Now(),
		})
		got, err := st.PendingBatches(ctx, users[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].ID != older || got[1].ID != newer || got[0].ItemCount != 1 {
			t.Errorf("PendingBatches = %+v, want %s then %s", got, older, newer)
		}
	})
}

func TestHasBatchWithoutCost(t *testing.T) {
	ctx := context.Background()
	date := time.Date(2027, 2, 1, 1, 0, 0, 0, time.UTC)
	cost := 1.0
	for _, tc := range []struct {
		name   string
		status string
		cost   *float64
		want   bool
	}{
		{"a pending batch blocks", "pending", nil, true},
		{"a batch in progress blocks", "in_progress", nil, true},
		{"an ended batch without cost blocks", "ended", nil, true},
		{"an ended batch with its cost does not block", "ended", &cost, false},
		{"a failed batch does not block", "failed", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, path := storetest.OpenWithPath(t)
			owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
			var anthropicID *string
			if tc.status != "pending" && tc.status != "failed" {
				anthropicID = ptr("msgbatch_1")
			}
			storetest.InsertBatch(t, path, owner, storetest.Batch{
				ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7DB0", Status: tc.status, AnthropicBatchID: anthropicID, ItemCount: 1, CostUSD: tc.cost, CreatedAt: date,
			})
			got, err := st.HasBatchWithoutCost(ctx, owner)
			if err != nil || got != tc.want {
				t.Errorf("HasBatchWithoutCost = %v, %v; want %v", got, err, tc.want)
			}
			if got, err := st.HasBatchWithoutCost(ctx, other); err != nil || got {
				t.Errorf("HasBatchWithoutCost of the other user = %v, %v; want false", got, err)
			}
		})
	}
}

func TestMonthSpend(t *testing.T) {
	ctx := context.Background()

	t.Run("sums the user's costs of batches created in the bounds", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		from := time.Date(2027, 1, 31, 23, 0, 0, 0, time.UTC) // 1 February 2027, 00:00 in Paris
		to := time.Date(2027, 2, 28, 23, 0, 0, 0, time.UTC)
		for i, b := range []struct {
			user    string
			created time.Time
			cost    *float64
		}{
			{owner, from.Add(-time.Millisecond), ptr(5.0)}, // the month before
			{owner, from, ptr(1.25)},
			{owner, to.Add(-time.Millisecond), ptr(2.5)},
			{owner, to, ptr(7.0)}, // the month after
			{owner, from.Add(time.Hour), nil},
			{other, from.Add(time.Hour), ptr(9.0)},
		} {
			storetest.InsertBatch(t, path, b.user, storetest.Batch{
				ID: fmt.Sprintf("01JB8Z6Q4M1X2Y3Z4A5B6C7DC%d", i), Status: "ended", AnthropicBatchID: ptr(fmt.Sprintf("msgbatch_%d", i)),
				ItemCount: 1, CostUSD: b.cost, CreatedAt: b.created,
			})
		}
		got, err := st.MonthSpend(ctx, owner, from, to)
		if err != nil || got != 3.75 {
			t.Errorf("MonthSpend = %v, %v; want 3.75", got, err)
		}
		if got, err := st.MonthSpend(ctx, newUser(t, st, "third"), from, to); err != nil || got != 0 {
			t.Errorf("MonthSpend of a user without batch = %v, %v; want 0", got, err)
		}
	})
}

func TestWaitingCount(t *testing.T) {
	t.Run("counts the user's extracted public items only", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		ctx := context.Background()
		if _, err := st.SetPrivate(ctx, users[0], items[0][0], true); err != nil {
			t.Fatal(err)
		}
		capture(t, st, users[0], link("https://example.com/queued"), keepNote)
		if got, err := st.WaitingCount(ctx, users[0]); err != nil || got != 1 {
			t.Errorf("WaitingCount = %d, %v; want 1", got, err)
		}
	})
}

func TestDropPrivateFromBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("leaves out the items made private and lowers the item count", func(t *testing.T) {
		st, _, clock, users, items := twoUsers(t)
		otherBatch, _, err := st.SubmitBatch(ctx, users[1], testBatch, items[1])
		if err != nil {
			t.Fatal(err)
		}
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SetPrivate(ctx, users[0], items[0][1], true); err != nil {
			t.Fatal(err)
		}
		if _, err := st.SetPrivate(ctx, users[1], items[1][0], true); err != nil {
			t.Fatal(err)
		}
		before := item(t, st, users[0], items[0][1])
		clock.Advance(time.Minute)

		dropped, remaining, err := st.DropPrivateFromBatch(ctx, users[0], batchID)
		if err != nil || fmt.Sprint(dropped) != fmt.Sprint(items[0][1:]) || fmt.Sprint(remaining) != fmt.Sprint(items[0][:1]) {
			t.Fatalf("DropPrivateFromBatch = %v, %v, %v; want [%s], [%s]", dropped, remaining, err, items[0][1], items[0][0])
		}
		after := item(t, st, users[0], items[0][1])
		if after.Status != "extracted" || after.BatchID != nil || after.ChangeSeq <= before.ChangeSeq ||
			!after.UpdatedAt.After(before.UpdatedAt) || after.Attempts != before.Attempts || !after.IsPrivate {
			t.Errorf("dropped item = %s, batch %v, seq %d→%d, attempts %d", after.Status, after.BatchID, before.ChangeSeq, after.ChangeSeq, after.Attempts)
		}
		if kept := item(t, st, users[0], items[0][0]); kept.Status != "submitted" || kept.BatchID == nil || *kept.BatchID != batchID {
			t.Errorf("kept item = %s, batch %v; want submitted with %s", kept.Status, kept.BatchID, batchID)
		}
		if b := batches(t, st, users[0]); b[0].Status != store.BatchPending || b[0].ItemCount != 1 {
			t.Errorf("batch = %+v, want pending with 1 item", b[0])
		}
		// The other user's batch and private item are untouched (RG-90).
		if it := item(t, st, users[1], items[1][0]); it.Status != "submitted" {
			t.Errorf("the other user's item is %s, want submitted", it.Status)
		}
		if b := batches(t, st, users[1]); b[0].ID != otherBatch || b[0].ItemCount != 2 {
			t.Errorf("the other user's batch = %+v, want 2 items", b[0])
		}
		if _, _, err := st.DropPrivateFromBatch(ctx, users[1], batchID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("DropPrivateFromBatch of another user's batch = %v, want ErrNotFound", err)
		}
	})

	t.Run("writes nothing when no item was made private", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		seq := store.SequenceForTest(t, st)
		dropped, remaining, err := st.DropPrivateFromBatch(ctx, users[0], batchID)
		if err != nil || len(dropped) != 0 || fmt.Sprint(remaining) != fmt.Sprint(items[0]) || store.SequenceForTest(t, st) != seq {
			t.Errorf("DropPrivateFromBatch = %v, %v, %v; want nothing written, both remaining", dropped, remaining, err)
		}
	})

	t.Run("fails a batch whose items were all made private", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range items[0] {
			if _, err := st.SetPrivate(ctx, users[0], id, true); err != nil {
				t.Fatal(err)
			}
		}
		dropped, remaining, err := st.DropPrivateFromBatch(ctx, users[0], batchID)
		if err != nil || len(dropped) != 2 || len(remaining) != 0 {
			t.Fatalf("DropPrivateFromBatch = %v, %v, %v; want 2 dropped, none remaining", dropped, remaining, err)
		}
		if b := batches(t, st, users[0]); b[0].Status != store.BatchFailed {
			t.Errorf("batch is %s, want failed", b[0].Status)
		}
		for _, id := range items[0] {
			if it := item(t, st, users[0], id); it.Status != "extracted" || it.BatchID != nil {
				t.Errorf("item %s = %s, batch %v; want extracted without batch", id, it.Status, it.BatchID)
			}
		}
	})

	t.Run("refuses a batch that is not pending", func(t *testing.T) {
		st, _, _, users, items := twoUsers(t)
		batchID, _, err := st.SubmitBatch(ctx, users[0], testBatch, items[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := st.MarkBatchSent(ctx, users[0], batchID, "msgbatch_01"); err != nil {
			t.Fatal(err)
		}
		if _, err := st.SetPrivate(ctx, users[0], items[0][0], true); err != nil {
			t.Fatal(err)
		}
		if _, _, err := st.DropPrivateFromBatch(ctx, users[0], batchID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("DropPrivateFromBatch of a batch in progress = %v, want ErrNotFound", err)
		}
		if it := item(t, st, users[0], items[0][0]); it.Status != "submitted" {
			t.Errorf("item is %s, want submitted: the batch is sent", it.Status)
		}
	})
}
