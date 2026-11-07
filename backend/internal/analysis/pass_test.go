package analysis_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/analysistest"
	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/imagecopy"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// passStart is 10 January 2027 at 02:00 in Paris (01:00 UTC), the default schedule.
var passStart = time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC)

// fakeClock is the clock of the store, of the pass and of the fake AI service.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// passEnv is an env whose store, pass and fake AI service share one clock.
type passEnv struct {
	*env
	clock *fakeClock
	ai    *analysistest.Analyzer
}

func newPassEnv(t *testing.T) *passEnv {
	t.Helper()
	clock := &fakeClock{now: passStart}
	return &passEnv{env: newEnvWithClock(t, clock.Now), clock: clock, ai: analysistest.New(clock.Now)}
}

// passConfig is the configuration of a pass on the env, with the fake AI service, a budget of
// 5 dollars and the real image copier.
func (e *passEnv) passConfig(t *testing.T) analysis.PassConfig {
	t.Helper()
	p, err := prompt.Load()
	if err != nil {
		t.Fatal(err)
	}
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatal(err)
	}
	return analysis.PassConfig{
		Store: e.st, Analyzer: e.ai, Model: testModel, Prompt: p, BudgetUSD: 5, Location: paris, Now: e.clock.Now,
		Prices: analysis.Prices{InputPerMTok: 1, OutputPerMTok: 5},
		Assets: e.assets, Copier: imagecopy.NewCopier([]string{os.Args[0], imagecopy.ChildCommand}, e.limits), Temp: e.temp,
		Logger: e.logger(),
	}
}

func (e *env) logger() *slog.Logger { return slog.New(slog.NewJSONHandler(e.logs, nil)) }

// run runs a pass of cfg.
func (e *passEnv) run(t *testing.T, cfg analysis.PassConfig) {
	t.Helper()
	pass, err := analysis.NewPass(cfg)
	if err != nil {
		t.Fatalf("NewPass: %v", err)
	}
	if err := pass.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// reconcile reconciles as a restarted backend does: a new pass of cfg.
func (e *passEnv) reconcile(t *testing.T, cfg analysis.PassConfig) {
	t.Helper()
	pass, err := analysis.NewPass(cfg)
	if err != nil {
		t.Fatalf("NewPass: %v", err)
	}
	if err := pass.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
}

func (e *env) item(t *testing.T, userID, id string) store.Item {
	t.Helper()
	it, err := e.st.Item(context.Background(), userID, id)
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	return it
}

func (e *env) batches(t *testing.T, userID string) []store.Batch {
	t.Helper()
	b, err := e.st.Batches(context.Background(), userID)
	if err != nil {
		t.Fatalf("Batches: %v", err)
	}
	return b
}

// sentIDs are the custom_id of every request of every body received, in order.
func (e *passEnv) sentIDs() []string {
	var ids []string
	for _, b := range e.ai.Bodies() {
		ids = append(ids, b.IDs()...)
	}
	return ids
}

func (e *env) wantStatus(t *testing.T, userID, id, status string) {
	t.Helper()
	if got := e.item(t, userID, id).Status; got != status {
		t.Errorf("item %s is %s, want %s", id, got, status)
	}
}

func (e *env) wantLog(t *testing.T, want map[string]string) {
	t.Helper()
	if !e.hasLog(t, want) {
		t.Errorf("no log line with %v in:\n%s", want, e.logs.String())
	}
}

var full = store.Extraction{Mode: "full", Title: ptr("Un titre de page"), ContentText: ptr("Le contenu")}

func TestPassSelection(t *testing.T) {
	t.Run("Only extracted items are sent", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		queued, _, err := e.st.CaptureItem(context.Background(), userID, store.NewItem{
			Kind: "link", Source: "share", Status: "queued", Title: "example.com", URL: ptr("https://example.com/queued"),
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		extracted := e.link(t, userID, "https://example.com/extracted", nil, full)
		var others []string
		for _, status := range []string{"submitted", "analyzed", "failed"} {
			id := e.link(t, userID, "https://example.com/"+status, nil, full)
			storetest.SetAnalysisState(t, e.path, userID, id, status, nil, 0)
			others = append(others, id)
		}
		e.run(t, e.passConfig(t))
		if got := e.sentIDs(); len(e.ai.Bodies()) != 1 || fmt.Sprint(got) != fmt.Sprint([]string{extracted}) {
			t.Errorf("sent %v in %d bodies, want only %s", got, len(e.ai.Bodies()), extracted)
		}
		e.wantStatus(t, userID, queued.ID, "queued")
		for i, status := range []string{"submitted", "analyzed", "failed"} {
			e.wantStatus(t, userID, others[i], status)
		}
	})

	t.Run("Nothing waiting makes no call", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		storetest.SetAnalysisState(t, e.path, userID, id, "analyzed", &passStart, 1)
		e.run(t, e.passConfig(t))
		if e.ai.Calls() != 0 {
			t.Errorf("the AI service received %d calls, want none", e.ai.Calls())
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "nothing_to_send"})
	})

	t.Run("A deleted item is not sent", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		deleted := e.link(t, userID, "https://example.com/deleted", nil, full)
		storetest.DeleteItem(t, e.path, userID, deleted)
		kept := e.link(t, userID, "https://example.com/kept", nil, full)
		e.run(t, e.passConfig(t))
		if got := e.sentIDs(); slices.Contains(got, deleted) || !slices.Contains(got, kept) {
			t.Errorf("sent %v, want %s and not %s", got, kept, deleted)
		}
	})

	t.Run("Each user's batch holds only their items", func(t *testing.T) {
		e := newPassEnv(t)
		users := []string{e.user(t, "owner"), e.user(t, "other")}
		items := map[string][]string{}
		for _, u := range users {
			for j := range 2 {
				items[u] = append(items[u], e.link(t, u, fmt.Sprintf("https://example.com/%s/%d", u, j), nil, full))
			}
		}
		e.run(t, e.passConfig(t))
		bodies := e.ai.Bodies()
		if len(bodies) != 2 {
			t.Fatalf("%d batches sent, want 2", len(bodies))
		}
		for _, u := range users {
			found := false
			for _, b := range bodies {
				if fmt.Sprint(b.IDs()) == fmt.Sprint(items[u]) {
					found = true
				}
			}
			if !found {
				t.Errorf("no batch holds exactly %v", items[u])
			}
			batches := e.batches(t, u)
			if len(batches) != 1 {
				t.Fatalf("user has %d batches, want 1", len(batches))
			}
			for _, id := range items[u] {
				it := e.item(t, u, id)
				if it.Status != "submitted" || it.BatchID == nil || *it.BatchID != batches[0].ID {
					t.Errorf("item %s is %s with batch %v, want submitted with %s", id, it.Status, it.BatchID, batches[0].ID)
				}
			}
		}
	})

	t.Run("Beyond 200 items, the oldest leave first", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		// The items are written in an order that is not their capture order: item i is captured
		// at minute (37 i mod 203), 37 being prime with 203.
		captured := make([]string, 203) // by capture minute
		for i := range 203 {
			minute := (37 * i) % 203
			e.clock.Set(passStart.Add(-24*time.Hour + time.Duration(minute)*time.Minute))
			captured[minute] = e.note(t, userID, fmt.Sprintf("Note %d", i))
		}
		e.clock.Set(passStart)
		e.run(t, e.passConfig(t))
		if got := e.sentIDs(); fmt.Sprint(got) != fmt.Sprint(captured[:200]) {
			t.Errorf("sent %d items, want the 200 oldest in capture order", len(got))
		}
		for _, id := range captured[200:] {
			e.wantStatus(t, userID, id, "extracted")
		}
	})

	t.Run("Beyond the body limit, the surplus waits", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		var ids []string
		for i := range 3 {
			e.clock.Advance(time.Second)
			ids = append(ids, e.note(t, userID, fmt.Sprintf("Une note de même longueur %d", i)))
		}
		// The limit is the size of the body of the two oldest: the third does not fit.
		reqs := e.requests(t, userID, 0)
		queue, err := e.st.AnalysisQueue(context.Background(), userID, analysis.MaxBatchItems)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range queue {
			if _, err := reqs.Add(context.Background(), it); err != nil {
				t.Fatal(err)
			}
		}
		limit, err := reqs.WriteBody(&bytes.Buffer{}, ids[:2])
		if err != nil {
			t.Fatal(err)
		}

		e.clock.Set(passStart)
		cfg := e.passConfig(t)
		cfg.BodyLimit = limit
		e.run(t, cfg)
		if got := e.sentIDs(); fmt.Sprint(got) != fmt.Sprint(ids[:2]) {
			t.Errorf("sent %v, want the two oldest %v", got, ids[:2])
		}
		e.wantStatus(t, userID, ids[2], "extracted")
		if bodies := e.ai.Bodies(); len(bodies) != 1 {
			t.Fatalf("%d bodies, want 1", len(bodies))
		} else if size := len(bodies[0].Raw); int64(size) > limit {
			t.Errorf("body of %d bytes, above the limit %d", size, limit)
		}

		// The next night, once the first batch has its cost, the newest goes.
		batches := e.batches(t, userID)
		if len(batches) != 1 {
			t.Fatalf("%d batches, want 1", len(batches))
		}
		storetest.SetBatchState(t, e.path, userID, batches[0].ID, store.BatchEnded, ptr(0.01))
		e.clock.Advance(24 * time.Hour)
		e.run(t, cfg)
		if bodies := e.ai.Bodies(); len(bodies) != 2 || fmt.Sprint(bodies[1].IDs()) != fmt.Sprint(ids[2:]) {
			t.Errorf("the next pass sent %d bodies, want a second one with %s", len(bodies), ids[2])
		}
	})

	t.Run("A copier that cannot start sends nothing", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id, _ := e.image(t, userID, imagetest.JPEG(40, 30), "image/jpeg", nil)
		cfg := e.passConfig(t)
		cfg.Copier = imagecopy.NewCopier([]string{filepath.Join(t.TempDir(), "missing-binary"), imagecopy.ChildCommand}, e.limits)
		e.run(t, cfg)
		if e.ai.Creates() != 0 {
			t.Errorf("%d batches sent, want none", e.ai.Creates())
		}
		e.wantStatus(t, userID, id, "extracted")
		if got := e.batches(t, userID); len(got) != 0 {
			t.Errorf("batches = %+v, want none", got)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "copier_unavailable"})
	})
}

// endedBatch inserts an ended batch of userID created at createdAt, costing cost.
func endedBatch(t *testing.T, e *passEnv, userID string, n int, createdAt time.Time, cost float64) {
	t.Helper()
	storetest.InsertBatch(t, e.path, userID, storetest.Batch{
		ID: fmt.Sprintf("01JB8Z6Q4M1X2Y3Z4A5B6C7E%02d", n), Status: store.BatchEnded,
		AnthropicBatchID: ptr(fmt.Sprintf("msgbatch_ended_%s_%d", userID, n)), ItemCount: 1, CostUSD: &cost, CreatedAt: createdAt,
	})
}

func TestPassGuards(t *testing.T) {
	t.Run("No API key, no send", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		cfg := e.passConfig(t)
		cfg.Analyzer = nil
		e.run(t, cfg)
		if e.ai.Calls() != 0 {
			t.Errorf("%d calls, want none", e.ai.Calls())
		}
		e.wantStatus(t, userID, id, "extracted")
		e.wantLog(t, map[string]string{"outcome": "no_api_key"})
	})

	// The follow-up runs before the guard (apply-batch-results design D1): the AI service still
	// processes the batch in progress, and does not answer the state query of the ended one, so
	// that neither gets its cost before the guard.
	blocked := []struct {
		name, status string
		cost         *float64
		state        analysistest.State
	}{
		{"A batch without recorded cost blocks the next one", store.BatchInProgress, nil, analysistest.Processing()},
		{"An ended batch without cost blocks the next one", store.BatchEnded, nil, analysistest.QueryFailure(503, "overloaded_error")},
	}
	for _, tc := range blocked {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			userID := e.user(t, "owner")
			storetest.InsertBatch(t, e.path, userID, storetest.Batch{
				ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7E00", Status: tc.status, AnthropicBatchID: ptr("msgbatch_previous"),
				ItemCount: 1, CostUSD: tc.cost, CreatedAt: passStart.Add(-24 * time.Hour),
			})
			e.ai.ScriptStates("msgbatch_previous", tc.state)
			id := e.link(t, userID, "https://example.com/a", nil, full)
			e.run(t, e.passConfig(t))
			if e.ai.Creates() != 0 {
				t.Errorf("%d batches sent, want none", e.ai.Creates())
			}
			e.wantStatus(t, userID, id, "extracted")
			e.wantLog(t, map[string]string{"user_id": userID, "outcome": "cost_pending"})
		})
	}

	budget := []struct {
		name    string
		now     time.Time
		batches map[time.Time]float64
		sent    bool
	}{
		{
			"Budget reached, no send", passStart,
			map[time.Time]float64{passStart.Add(-48 * time.Hour): 2.5, passStart.Add(-24 * time.Hour): 2.5},
			false,
		},
		{
			"Spend below the budget allows the send", passStart,
			map[time.Time]float64{passStart.Add(-48 * time.Hour): 2.5, passStart.Add(-24 * time.Hour): 2.49},
			true,
		},
		// 23:30 in Paris on 31 January; the pass at 02:00 in Paris on 1 February.
		{
			"Last month's spend does not count", time.Date(2027, 2, 1, 1, 0, 0, 0, time.UTC),
			map[time.Time]float64{time.Date(2027, 1, 31, 22, 30, 0, 0, time.UTC): 5},
			true,
		},
		// 00:30 in Paris on 1 February (23:30 UTC on 31 January); the pass on 2 February.
		{
			"The month follows Paris time", time.Date(2027, 2, 2, 1, 0, 0, 0, time.UTC),
			map[time.Time]float64{time.Date(2027, 1, 31, 23, 30, 0, 0, time.UTC): 5},
			false,
		},
	}
	for _, tc := range budget {
		t.Run(tc.name, func(t *testing.T) {
			e := newPassEnv(t)
			userID := e.user(t, "owner")
			n := 0
			for created, cost := range tc.batches {
				endedBatch(t, e, userID, n, created, cost)
				n++
			}
			id := e.link(t, userID, "https://example.com/a", nil, full)
			e.clock.Set(tc.now)
			e.run(t, e.passConfig(t))
			if tc.sent {
				if got := e.sentIDs(); fmt.Sprint(got) != fmt.Sprint([]string{id}) {
					t.Errorf("sent %v, want %s", got, id)
				}
				return
			}
			if e.ai.Creates() != 0 {
				t.Errorf("%d batches sent, want none", e.ai.Creates())
			}
			e.wantStatus(t, userID, id, "extracted")
			e.wantLog(t, map[string]string{"user_id": userID, "outcome": "budget_reached"})
		})
	}

	t.Run("Another user's spend does not count", func(t *testing.T) {
		e := newPassEnv(t)
		owner, other := e.user(t, "owner"), e.user(t, "other")
		endedBatch(t, e, other, 0, passStart.Add(-24*time.Hour), 5)
		mine := e.link(t, owner, "https://example.com/mine", nil, full)
		theirs := e.link(t, other, "https://example.com/theirs", nil, full)
		e.run(t, e.passConfig(t))
		if got := e.sentIDs(); fmt.Sprint(got) != fmt.Sprint([]string{mine}) {
			t.Errorf("sent %v, want only %s", got, mine)
		}
		e.wantStatus(t, other, theirs, "extracted")
		e.wantLog(t, map[string]string{"user_id": other, "outcome": "budget_reached"})
	})

	t.Run("A failed send does not block the next night", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		storetest.InsertBatch(t, e.path, userID, storetest.Batch{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7E00", Status: store.BatchFailed, ItemCount: 1, CreatedAt: passStart.Add(-24 * time.Hour),
		})
		id := e.link(t, userID, "https://example.com/a", nil, full)
		e.run(t, e.passConfig(t))
		if got := e.sentIDs(); fmt.Sprint(got) != fmt.Sprint([]string{id}) {
			t.Errorf("sent %v, want %s", got, id)
		}
	})
}

func TestPassSubmission(t *testing.T) {
	t.Run("Items become submitted", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		before := []store.Item{e.item(t, userID, ids[0]), e.item(t, userID, ids[1])}
		e.clock.Advance(time.Minute)
		e.run(t, e.passConfig(t))
		batches := e.batches(t, userID)
		if len(batches) != 1 {
			t.Fatalf("%d batches, want 1", len(batches))
		}
		b := batches[0]
		if b.Status != store.BatchInProgress || b.AnthropicBatchID == nil || *b.AnthropicBatchID != "msgbatch_fake001" ||
			b.ItemCount != 2 || b.Model != testModel || b.PromptVersion != 3 || b.EnvelopeVersion != 1 {
			t.Errorf("batch = %+v, want in progress with the service's id, 2 items, the model, versions 3 and 1", b)
		}
		for i, id := range ids {
			after := e.item(t, userID, id)
			if after.Status != "submitted" || after.ChangeSeq <= before[i].ChangeSeq || !after.UpdatedAt.After(before[i].UpdatedAt) ||
				after.BatchID == nil || *after.BatchID != b.ID {
				t.Errorf("item %d = %s, seq %d→%d, updated %v→%v, batch %v", i, after.Status, before[i].ChangeSeq, after.ChangeSeq,
					before[i].UpdatedAt, after.UpdatedAt, after.BatchID)
			}
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "sent", "batch_id": b.ID, "anthropic_batch_id": "msgbatch_fake001"})
	})

	t.Run("The items' attempts are unchanged", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		storetest.SetAnalysisState(t, e.path, userID, id, "extracted", nil, 1)
		e.run(t, e.passConfig(t))
		if it := e.item(t, userID, id); it.Status != "submitted" || it.Attempts != 1 {
			t.Errorf("item is %s with %d attempts, want submitted with 1", it.Status, it.Attempts)
		}
	})

	t.Run("The service refuses the batch", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		storetest.SetAnalysisState(t, e.path, userID, ids[0], "extracted", nil, 2)
		before := []store.Item{e.item(t, userID, ids[0]), e.item(t, userID, ids[1])}
		e.ai.Script(analysistest.Reject)
		e.run(t, e.passConfig(t))
		for i, id := range ids {
			after := e.item(t, userID, id)
			if after.Status != "extracted" || after.Attempts != before[i].Attempts || after.ChangeSeq <= before[i].ChangeSeq || after.BatchID != nil {
				t.Errorf("item %d = %s, attempts %d→%d, seq %d→%d, batch %v", i, after.Status, before[i].Attempts, after.Attempts,
					before[i].ChangeSeq, after.ChangeSeq, after.BatchID)
			}
		}
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchFailed {
			t.Errorf("batches = %+v, want one failed", b)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "rejected"})
	})

	t.Run("A lost answer is matched at the next start", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		e.ai.Script(analysistest.AcceptLost)
		cfg := e.passConfig(t)
		e.run(t, cfg)
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchPending {
			t.Fatalf("batches = %+v, want one pending", b)
		}
		e.clock.Advance(time.Hour)
		e.reconcile(t, cfg)
		b := e.batches(t, userID)
		if b[0].Status != store.BatchInProgress || b[0].AnthropicBatchID == nil || *b[0].AnthropicBatchID != "msgbatch_fake001" {
			t.Errorf("batch = %+v, want in progress with msgbatch_fake001", b[0])
		}
		for _, id := range ids {
			e.wantStatus(t, userID, id, "submitted")
		}
		e.wantLog(t, map[string]string{"batch_id": b[0].ID, "outcome": "adopted"})
	})

	t.Run("A send that never arrived puts the items back", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		e.ai.Script(analysistest.Unknown)
		cfg := e.passConfig(t)
		e.run(t, cfg)
		for _, id := range ids {
			e.wantStatus(t, userID, id, "submitted")
		}
		e.clock.Advance(time.Hour)
		e.reconcile(t, cfg)
		for _, id := range ids {
			e.wantStatus(t, userID, id, "extracted")
		}
		b := e.batches(t, userID)
		if len(b) != 1 || b[0].Status != store.BatchFailed {
			t.Errorf("batches = %+v, want one failed", b)
		}
		e.wantLog(t, map[string]string{"batch_id": b[0].ID, "outcome": "possible_double_send"})
	})

	t.Run("An unknown outcome blocks the user's next batch until reconciled", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		e.link(t, userID, "https://example.com/a", nil, full)
		e.ai.Script(analysistest.Unknown)
		cfg := e.passConfig(t)
		e.run(t, cfg)
		e.ai.FailLists(true)
		e.clock.Advance(24 * time.Hour)
		id := e.link(t, userID, "https://example.com/b", nil, full)
		e.run(t, cfg)
		if e.ai.Creates() != 1 {
			t.Errorf("%d batches sent, want only the first", e.ai.Creates())
		}
		e.wantStatus(t, userID, id, "extracted")
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "cost_pending"})
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "list_failed"})
	})
}

func TestMatchPending(t *testing.T) {
	created := time.Date(2027, 1, 10, 1, 0, 0, 0, time.UTC)
	row := func(id string, at time.Time, count int) store.Batch {
		return store.Batch{ID: id, Status: store.BatchPending, ItemCount: count, CreatedAt: at}
	}
	listed := func(id string, at time.Time, count int) analysis.BatchInfo {
		return analysis.BatchInfo{ID: id, CreatedAt: at, RequestCount: count}
	}
	for _, tc := range []struct {
		name     string
		pending  []store.Batch
		listed   []analysis.BatchInfo
		recorded map[string]bool
		want     []analysis.Match
	}{
		{
			"adopts a batch created 10 minutes before",
			[]store.Batch{row("b1", created, 2)},
			[]analysis.BatchInfo{listed("a1", created.Add(-10*time.Minute), 2)},
			nil,
			[]analysis.Match{{BatchID: "b1", AnthropicID: "a1"}},
		},
		{
			"adopts a batch created 20 minutes after",
			[]store.Batch{row("b1", created, 2)},
			[]analysis.BatchInfo{listed("a1", created.Add(20*time.Minute), 2)},
			nil,
			[]analysis.Match{{BatchID: "b1", AnthropicID: "a1"}},
		},
		{
			"leaves a batch created just before the window",
			[]store.Batch{row("b1", created, 2)},
			[]analysis.BatchInfo{listed("a1", created.Add(-10*time.Minute-time.Millisecond), 2)},
			nil,
			[]analysis.Match{{BatchID: "b1"}},
		},
		{
			"leaves a batch created just after the window",
			[]store.Batch{row("b1", created, 2)},
			[]analysis.BatchInfo{listed("a1", created.Add(20*time.Minute+time.Millisecond), 2)},
			nil,
			[]analysis.Match{{BatchID: "b1"}},
		},
		{
			"leaves a batch of another count",
			[]store.Batch{row("b1", created, 2)},
			[]analysis.BatchInfo{listed("a1", created, 3)},
			nil,
			[]analysis.Match{{BatchID: "b1"}},
		},
		{
			"adopts none of two candidates",
			[]store.Batch{row("b1", created, 2)},
			[]analysis.BatchInfo{listed("a1", created, 2), listed("a2", created.Add(time.Minute), 2)},
			nil,
			[]analysis.Match{{BatchID: "b1"}},
		},
		{
			"leaves a batch already recorded",
			[]store.Batch{row("b1", created, 2)},
			[]analysis.BatchInfo{listed("a1", created, 2), listed("a2", created.Add(time.Minute), 2)},
			map[string]bool{"a1": true},
			[]analysis.Match{{BatchID: "b1", AnthropicID: "a2"}},
		},
		{
			"an adopted batch leaves the pool for the next row",
			[]store.Batch{row("b1", created, 2), row("b2", created.Add(time.Minute), 2)},
			[]analysis.BatchInfo{listed("a1", created, 2)},
			nil,
			[]analysis.Match{{BatchID: "b1", AnthropicID: "a1"}, {BatchID: "b2"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := analysis.MatchPending(tc.pending, tc.listed, tc.recorded)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("MatchPending = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// privateItem captures a private link of userID with distinctive values, at status.
func privateItem(t *testing.T, e *passEnv, userID, status string) (id string, values []string) {
	t.Helper()
	tag := "PRIVATE-" + strings.ToUpper(status) + "-7f3a"
	title, note, content, url := tag+"-TITLE", tag+"-NOTE", tag+"-CONTENT", "https://"+strings.ToLower(tag)+".example/path"
	item, _, err := e.st.CaptureItem(context.Background(), userID, store.NewItem{
		Kind: "link", Source: "share", Status: "queued", Title: title, URL: &url, UserNote: &note, IsPrivate: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != "queued" {
		if written, err := e.st.WriteExtraction(context.Background(), userID, item.ID, store.Extraction{
			Mode: "full", Title: &title, SiteName: ptr(tag + "-SITE"), ContentText: &content,
		}); err != nil || !written {
			t.Fatalf("WriteExtraction = %v, %v", written, err)
		}
	}
	if status != "queued" && status != "extracted" {
		storetest.SetAnalysisState(t, e.path, userID, item.ID, status, nil, 1)
	}
	return item.ID, []string{item.ID, title, note, content, url, tag}
}

func TestPassPrivateItems(t *testing.T) {
	t.Run("Private items at every status are never sent", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		var secrets, privateIDs, privateAssets []string
		for _, status := range []string{"queued", "extracted", "submitted", "analyzed", "failed"} {
			id, values := privateItem(t, e, userID, status)
			secrets = append(secrets, values...)
			privateIDs = append(privateIDs, id)
		}
		// A private note and a private image, extracted and submitted.
		for _, status := range []string{"extracted", "submitted"} {
			text := "PRIVATE-NOTE-TEXT-" + strings.ToUpper(status) + "-4c1d\nsecond line"
			noteID := e.capture(t, userID, store.NewItem{
				Kind: "note", Source: "manual", Status: "queued", Title: "PRIVATE-NOTE-TITLE-" + status, UserNote: &text, IsPrivate: true,
			}, store.Extraction{Mode: "none"})
			imageNote := "PRIVATE-IMAGE-NOTE-" + strings.ToUpper(status) + "-8e2f"
			imageID, file := e.imageWith(t, userID, imagetest.JPEG(40, 30), "image/jpeg", &imageNote, true)
			if status == "submitted" {
				storetest.SetAnalysisState(t, e.path, userID, noteID, status, nil, 1)
				storetest.SetAnalysisState(t, e.path, userID, imageID, status, nil, 1)
			}
			secrets = append(secrets, noteID, imageID, "PRIVATE-NOTE-TEXT-"+strings.ToUpper(status), "PRIVATE-NOTE-TITLE-"+status, imageNote)
			privateIDs = append(privateIDs, noteID, imageID)
			privateAssets = append(privateAssets, strings.TrimSuffix(filepath.Base(file), ".jpg"))
		}
		public := e.link(t, userID, "https://example.com/public", nil, full)
		publicImage, publicFile := e.image(t, userID, imagetest.JPEG(40, 30), "image/jpeg", nil)
		cfg := e.passConfig(t)
		copier := &recordingCopier{inner: cfg.Copier}
		cfg.Copier = copier
		e.run(t, cfg)

		bodies := e.ai.Bodies()
		if len(bodies) != 1 || fmt.Sprint(bodies[0].IDs()) != fmt.Sprint([]string{public, publicImage}) {
			t.Fatalf("sent %v, want only the public items %s and %s", e.sentIDs(), public, publicImage)
		}
		for _, id := range e.sentIDs() {
			if slices.Contains(privateIDs, id) {
				t.Errorf("a request carries the private item %s", id)
			}
		}
		for _, b := range bodies {
			for _, secret := range secrets {
				if bytes.Contains(b.Raw, []byte(secret)) {
					t.Errorf("a body contains %q", secret)
				}
			}
		}
		// Only the public image was copied: no image of a private item was even decoded.
		copied := copier.names()
		if len(copied) != 1 || !strings.Contains(copied[0], strings.TrimSuffix(filepath.Base(publicFile), ".jpg")) {
			t.Errorf("copied %v, want only the public image", copied)
		}
		for _, name := range copied {
			for _, asset := range privateAssets {
				if strings.Contains(name, asset) {
					t.Errorf("the image of a private item was copied: %s", name)
				}
			}
		}
	})

	t.Run("An item made private after selection is not sent", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		madePrivate := e.link(t, userID, "https://example.com/made-private-9c2e", ptr("NOTE-MADE-PRIVATE-9c2e"), full)
		other := e.link(t, userID, "https://example.com/other", nil, full)
		cfg := e.passConfig(t)
		analysis.WithBeforeSubmit(&cfg, func(ctx context.Context, user string, ids []string) {
			if !slices.Contains(ids, madePrivate) {
				t.Errorf("the selection did not hold %s: %v", madePrivate, ids)
			}
			if _, err := e.st.SetPrivate(ctx, user, madePrivate, true); err != nil {
				t.Errorf("SetPrivate: %v", err)
			}
		})
		e.run(t, cfg)
		bodies := e.ai.Bodies()
		if len(bodies) != 1 || fmt.Sprint(bodies[0].IDs()) != fmt.Sprint([]string{other}) {
			t.Fatalf("sent %v, want only %s", e.sentIDs(), other)
		}
		for _, secret := range []string{madePrivate, "made-private-9c2e", "NOTE-MADE-PRIVATE-9c2e"} {
			if bytes.Contains(bodies[0].Raw, []byte(secret)) {
				t.Errorf("the body contains %q", secret)
			}
		}
		e.wantStatus(t, userID, madePrivate, "extracted")
		if b := e.batches(t, userID); len(b) != 1 || b[0].ItemCount != 1 {
			t.Errorf("batches = %+v, want one of 1 item", b)
		}
	})

	t.Run("A private item reaching the request building stops the batch", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		cfg := e.passConfig(t)
		analysis.WithSelected(&cfg, func(_ string, queue []store.AnalysisQueueItem) []store.AnalysisQueueItem {
			queue[1].IsPrivate = true // as if the selection had let a private item through
			return queue
		})
		e.run(t, cfg)
		if e.ai.Calls() != 0 {
			t.Errorf("%d calls, want none", e.ai.Calls())
		}
		for _, id := range ids {
			e.wantStatus(t, userID, id, "extracted")
		}
		if b := e.batches(t, userID); len(b) != 0 {
			t.Errorf("batches = %+v, want none", b)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "private_item", "level": "ERROR"})
	})
}

// recordingCopier records the stored image each copy opens, then copies it with inner.
type recordingCopier struct {
	inner  analysis.ImageCopier
	mu     sync.Mutex
	copied []string
}

func (c *recordingCopier) Copy(ctx context.Context, asset, out *os.File) (imagecopy.Result, error) {
	c.mu.Lock()
	c.copied = append(c.copied, asset.Name())
	c.mu.Unlock()
	return c.inner.Copy(ctx, asset, out)
}

func (c *recordingCopier) names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.copied)
}

// failingTemp creates unnamed files with inner, and fails the failAt-th creation (from 1).
type failingTemp struct {
	inner  analysis.TempFiles
	failAt int
	n      int
}

func (f *failingTemp) NewFile() (w, r *os.File, err error) {
	f.n++
	if f.n == f.failAt {
		return nil, nil, errors.New("no space left on device")
	}
	return f.inner.NewFile()
}

// logStatus tells whether a log line with outcome carries the HTTP status status.
func (e *env) logStatus(t *testing.T, outcome string, status int) bool {
	t.Helper()
	for _, line := range e.logLines(t) {
		if line["outcome"] == outcome && line["status"] == float64(status) {
			return true
		}
	}
	return false
}

func pendingBatch(t *testing.T, e *passEnv, userID string) store.Batch {
	t.Helper()
	pending, err := e.st.PendingBatches(context.Background(), userID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("PendingBatches = %+v, %v; want one", pending, err)
	}
	return pending[0]
}

func TestPassSendOutcomes(t *testing.T) {
	t.Run("A server error leaves the batch pending until reconciled", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		e.ai.Script(analysistest.ServerFail)
		e.run(t, e.passConfig(t))
		pendingBatch(t, e, userID)
		e.wantStatus(t, userID, id, "submitted")
		if !e.logStatus(t, "send_unknown", 503) {
			t.Errorf("no send_unknown line with status 503 in:\n%s", e.logs.String())
		}
	})

	t.Run("A batch accepted while the pass stops is recorded", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.ai.OnCreate(func(context.Context) { cancel() }) // the stop arrives during the send
		pass, err := analysis.NewPass(e.passConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		_ = pass.Run(ctx) // stopped: its error is the context's
		b := e.batches(t, userID)
		if len(b) != 1 || b[0].Status != store.BatchInProgress || b[0].AnthropicBatchID == nil {
			t.Errorf("batches = %+v, want one in progress with its Anthropic id", b)
		}
		e.wantStatus(t, userID, id, "submitted")
	})

	t.Run("A pass stopped before the send puts the items back", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cfg := e.passConfig(t)
		analysis.WithAfterSubmit(&cfg, func(context.Context, string, string) { cancel() })
		pass, err := analysis.NewPass(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_ = pass.Run(ctx) // stopped: its error is the context's
		if e.ai.Calls() != 0 {
			t.Errorf("%d calls, want none", e.ai.Calls())
		}
		e.wantStatus(t, userID, id, "extracted")
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchFailed {
			t.Errorf("batches = %+v, want one failed", b)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "stopped"})
	})

	t.Run("A body that cannot be written puts the items back", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		cfg := e.passConfig(t)
		cfg.Temp = &failingTemp{inner: e.temp, failAt: 2} // the segments, then the body
		e.run(t, cfg)
		if e.ai.Calls() != 0 {
			t.Errorf("%d calls, want none", e.ai.Calls())
		}
		e.wantStatus(t, userID, id, "extracted")
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchFailed {
			t.Errorf("batches = %+v, want one failed", b)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "storage_unavailable"})
	})

	t.Run("A batch closed during its send is logged record_failed", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		e.link(t, userID, "https://example.com/a", nil, full)
		e.ai.OnCreate(func(ctx context.Context) {
			if _, err := e.st.FailBatch(ctx, userID, pendingBatch(t, e, userID).ID); err != nil {
				t.Errorf("FailBatch: %v", err)
			}
		})
		e.run(t, e.passConfig(t))
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "record_failed", "level": "ERROR"})
	})

	t.Run("An Anthropic id another row holds leaves the batch to the reconciliation, which puts the items back", func(t *testing.T) {
		e := newPassEnv(t)
		owner, other := e.user(t, "owner"), e.user(t, "other")
		// Another user's row on the same key already holds the id the fake gives next.
		storetest.InsertBatch(t, e.path, other, storetest.Batch{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7E00", Status: store.BatchEnded, AnthropicBatchID: ptr("msgbatch_fake001"),
			ItemCount: 1, CostUSD: ptr(0.01), CreatedAt: passStart.Add(-time.Minute),
		})
		id := e.link(t, owner, "https://example.com/a", nil, full)
		cfg := e.passConfig(t)
		e.run(t, cfg)
		pending := pendingBatch(t, e, owner)
		e.wantLog(t, map[string]string{"user_id": owner, "outcome": "anthropic_id_recorded"})

		e.clock.Advance(time.Hour)
		e.reconcile(t, cfg)
		if b := e.batches(t, owner); b[0].Status != store.BatchFailed {
			t.Errorf("batch is %s, want failed rather than pending for good", b[0].Status)
		}
		e.wantStatus(t, owner, id, "extracted")
		e.wantLog(t, map[string]string{"batch_id": pending.ID, "outcome": "possible_double_send"})
	})

	t.Run("A batch closed during the reconciliation is logged not_pending", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		e.link(t, userID, "https://example.com/a", nil, full)
		e.ai.Script(analysistest.AcceptLost)
		cfg := e.passConfig(t)
		e.run(t, cfg)
		batchID := pendingBatch(t, e, userID).ID
		e.ai.OnList(func(ctx context.Context) {
			if _, err := e.st.FailBatch(ctx, userID, batchID); err != nil {
				t.Errorf("FailBatch: %v", err)
			}
		})
		e.reconcile(t, cfg)
		e.wantLog(t, map[string]string{"batch_id": batchID, "outcome": "not_pending"})
	})

	t.Run("A list that never answers does not hold the pass", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		e.link(t, userID, "https://example.com/a", nil, full)
		e.ai.Script(analysistest.Unknown)
		cfg := e.passConfig(t)
		e.run(t, cfg)
		e.ai.BlockLists()
		cfg.ListTimeout = 50 * time.Millisecond
		e.clock.Advance(24 * time.Hour)
		id := e.link(t, userID, "https://example.com/b", nil, full)
		done := make(chan struct{})
		go func() {
			defer close(done)
			e.run(t, cfg)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the pass is still waiting for the list")
		}
		e.wantStatus(t, userID, id, "extracted")
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "list_failed"})
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "cost_pending"})
	})

	t.Run("A reconciliation waits for a send in flight", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		id := e.link(t, userID, "https://example.com/a", nil, full)
		cfg := e.passConfig(t)
		pass, err := analysis.NewPass(cfg)
		if err != nil {
			t.Fatal(err)
		}
		reconciled := make(chan error, 1)
		e.ai.OnCreate(func(context.Context) {
			// The start-up reconciliation arrives while the batch is being sent.
			go func() { reconciled <- pass.Reconcile(context.Background()) }()
			select {
			case err := <-reconciled:
				t.Errorf("the reconciliation ran during the send (%v)", err)
				reconciled <- err
			case <-time.After(200 * time.Millisecond):
			}
		})
		if err := pass.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := <-reconciled; err != nil {
			t.Errorf("Reconcile: %v", err)
		}
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchInProgress {
			t.Errorf("batches = %+v, want one in progress", b)
		}
		e.wantStatus(t, userID, id, "submitted")
	})
}

func TestPassPrivateAfterSubmit(t *testing.T) {
	t.Run("An item made private after submission and before the send is left out of the send", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		private := e.link(t, userID, "https://example.com/late-private-6b0e", ptr("NOTE-LATE-PRIVATE-6b0e"), store.Extraction{
			Mode: "full", Title: ptr("TITLE-LATE-PRIVATE-6b0e"), ContentText: ptr("CONTENT-LATE-PRIVATE-6b0e"),
		})
		other := e.link(t, userID, "https://example.com/other", nil, full)
		cfg := e.passConfig(t)
		analysis.WithAfterSubmit(&cfg, func(ctx context.Context, user, _ string) {
			if it := e.item(t, user, private); it.Status != "submitted" {
				t.Errorf("the item is %s at the hook, want submitted", it.Status)
			}
			if _, err := e.st.SetPrivate(ctx, user, private, true); err != nil {
				t.Errorf("SetPrivate: %v", err)
			}
		})
		e.run(t, cfg)

		bodies := e.ai.Bodies()
		if len(bodies) != 1 || fmt.Sprint(bodies[0].IDs()) != fmt.Sprint([]string{other}) {
			t.Fatalf("sent %v in %d bodies, want one batch with only %s", e.sentIDs(), len(bodies), other)
		}
		for _, secret := range []string{private, "late-private-6b0e", "NOTE-LATE-PRIVATE", "TITLE-LATE-PRIVATE", "CONTENT-LATE-PRIVATE"} {
			if bytes.Contains(bodies[0].Raw, []byte(secret)) {
				t.Errorf("the body contains %q", secret)
			}
		}
		if it := e.item(t, userID, private); it.Status != "extracted" || it.BatchID != nil || !it.IsPrivate {
			t.Errorf("the private item is %s with batch %v, want extracted without batch", it.Status, it.BatchID)
		}
		b := e.batches(t, userID)
		if len(b) != 1 || b[0].Status != store.BatchInProgress || b[0].ItemCount != 1 {
			t.Fatalf("batches = %+v, want one in progress with 1 item", b)
		}
		if it := e.item(t, userID, other); it.Status != "submitted" || it.BatchID == nil || *it.BatchID != b[0].ID {
			t.Errorf("the other item is %s with batch %v, want submitted with %s", it.Status, it.BatchID, b[0].ID)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "private_after_submit"})
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "sent"})
		for _, line := range e.logLines(t) {
			if line["outcome"] == "private_after_submit" && (line["removed"] != float64(1) || line["remaining"] != float64(1)) {
				t.Errorf("log line %v, want removed 1 and remaining 1", line)
			}
		}
	})

	t.Run("When every item of the batch is made private, nothing is sent", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		cfg := e.passConfig(t)
		analysis.WithAfterSubmit(&cfg, func(ctx context.Context, user, _ string) {
			for _, id := range ids {
				if _, err := e.st.SetPrivate(ctx, user, id, true); err != nil {
					t.Errorf("SetPrivate: %v", err)
				}
			}
		})
		e.run(t, cfg)
		if e.ai.Calls() != 0 {
			t.Errorf("%d calls, want none", e.ai.Calls())
		}
		for _, id := range ids {
			e.wantStatus(t, userID, id, "extracted")
		}
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchFailed {
			t.Errorf("batches = %+v, want one failed", b)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "private_after_submit"})
		if !e.hasCounts(t, 2, 0) {
			t.Errorf("no private_after_submit line with removed 2 and remaining 0 in:\n%s", e.logs.String())
		}
	})

	t.Run("An item checked Privé during a rewrite is left out too", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{
			e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full),
			e.link(t, userID, "https://example.com/c", nil, full),
		}
		cfg := e.passConfig(t)
		analysis.WithAfterSubmit(&cfg, func(ctx context.Context, user, _ string) {
			if _, err := e.st.SetPrivate(ctx, user, ids[0], true); err != nil {
				t.Errorf("SetPrivate: %v", err)
			}
		})
		rewrites := 0
		analysis.WithBeforeRewrite(&cfg, func(ctx context.Context, user, _ string) error {
			rewrites++
			if rewrites == 1 { // after the first round left ids[0] out
				if _, err := e.st.SetPrivate(ctx, user, ids[1], true); err != nil {
					t.Errorf("SetPrivate: %v", err)
				}
			}
			return nil
		})
		e.run(t, cfg)
		bodies := e.ai.Bodies()
		if len(bodies) != 1 || fmt.Sprint(bodies[0].IDs()) != fmt.Sprint(ids[2:]) {
			t.Fatalf("sent %v, want only %s", e.sentIDs(), ids[2])
		}
		for _, id := range ids[:2] {
			if bytes.Contains(bodies[0].Raw, []byte(id)) {
				t.Errorf("the body contains %s", id)
			}
			e.wantStatus(t, userID, id, "extracted")
		}
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchInProgress || b[0].ItemCount != 1 {
			t.Errorf("batches = %+v, want one in progress with 1 item", b)
		}
		if rewrites != 2 || !e.hasCounts(t, 1, 2) || !e.hasCounts(t, 1, 1) {
			t.Errorf("%d rewrites, logs:\n%s; want two rounds of one item", rewrites, e.logs.String())
		}
	})

	t.Run("Items checked Privé over many rounds are all left out, and the others are sent the same night", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		var ids []string
		for i := range 9 {
			ids = append(ids, e.link(t, userID, fmt.Sprintf("https://example.com/%d", i), nil, full))
		}
		// One item checked Privé after the submission, then one more before each rewrite: six
		// rounds leave an item out each, then a seventh leaves nothing out.
		const rounds = 6
		next := 0
		makePrivate := func(ctx context.Context, user string) {
			if next >= rounds {
				return
			}
			if _, err := e.st.SetPrivate(ctx, user, ids[next], true); err != nil {
				t.Errorf("SetPrivate: %v", err)
			}
			next++
		}
		cfg := e.passConfig(t)
		analysis.WithAfterSubmit(&cfg, func(ctx context.Context, user, _ string) { makePrivate(ctx, user) })
		analysis.WithBeforeRewrite(&cfg, func(ctx context.Context, user, _ string) error {
			makePrivate(ctx, user)
			return nil
		})
		e.run(t, cfg)

		removed, remaining := ids[:rounds], ids[rounds:]
		bodies := e.ai.Bodies()
		if len(bodies) != 1 || fmt.Sprint(bodies[0].IDs()) != fmt.Sprint(remaining) {
			t.Fatalf("sent %v, want exactly %v", e.sentIDs(), remaining)
		}
		for _, id := range removed {
			if bytes.Contains(bodies[0].Raw, []byte(id)) {
				t.Errorf("the body contains %s", id)
			}
			if it := e.item(t, userID, id); it.Status != "extracted" || it.BatchID != nil {
				t.Errorf("item %s is %s with batch %v, want extracted without batch", id, it.Status, it.BatchID)
			}
		}
		b := e.batches(t, userID)
		if len(b) != 1 || b[0].Status != store.BatchInProgress || b[0].ItemCount != len(remaining) {
			t.Fatalf("batches = %+v, want one in progress with %d items", b, len(remaining))
		}
		for _, id := range remaining {
			if it := e.item(t, userID, id); it.Status != "submitted" || it.BatchID == nil || *it.BatchID != b[0].ID {
				t.Errorf("item %s is %s with batch %v, want submitted with %s", id, it.Status, it.BatchID, b[0].ID)
			}
		}
		for round := range rounds {
			if !e.hasCounts(t, 1, len(ids)-round-1) {
				t.Errorf("no private_after_submit line for round %d in:\n%s", round+1, e.logs.String())
			}
		}
	})

	t.Run("A pass stopped during a rewrite puts the items back", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cfg := e.passConfig(t)
		analysis.WithAfterSubmit(&cfg, func(ctx context.Context, user, _ string) {
			if _, err := e.st.SetPrivate(ctx, user, ids[0], true); err != nil {
				t.Errorf("SetPrivate: %v", err)
			}
		})
		analysis.WithBeforeRewrite(&cfg, func(context.Context, string, string) error {
			cancel() // the stop arrives during the rewrite
			return nil
		})
		pass, err := analysis.NewPass(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_ = pass.Run(ctx) // stopped: its error is the context's
		if e.ai.Calls() != 0 {
			t.Errorf("%d calls, want none", e.ai.Calls())
		}
		for _, id := range ids {
			e.wantStatus(t, userID, id, "extracted")
		}
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchFailed {
			t.Errorf("batches = %+v, want one failed", b)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "stopped"})
	})

	t.Run("A body that cannot be rewritten puts the items back", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		cfg := e.passConfig(t)
		analysis.WithAfterSubmit(&cfg, func(ctx context.Context, user, _ string) {
			if _, err := e.st.SetPrivate(ctx, user, ids[0], true); err != nil {
				t.Errorf("SetPrivate: %v", err)
			}
		})
		analysis.WithBeforeRewrite(&cfg, func(context.Context, string, string) error {
			return errors.New("no space left on device")
		})
		e.run(t, cfg)
		if e.ai.Calls() != 0 {
			t.Errorf("%d calls, want none", e.ai.Calls())
		}
		for _, id := range ids {
			e.wantStatus(t, userID, id, "extracted")
		}
		if b := e.batches(t, userID); len(b) != 1 || b[0].Status != store.BatchFailed {
			t.Errorf("batches = %+v, want one failed", b)
		}
		e.wantLog(t, map[string]string{"user_id": userID, "outcome": "storage_unavailable"})
	})

	t.Run("An item checked Privé during the send stays submitted in its batch", func(t *testing.T) {
		e := newPassEnv(t)
		userID := e.user(t, "owner")
		ids := []string{e.link(t, userID, "https://example.com/a", nil, full), e.link(t, userID, "https://example.com/b", nil, full)}
		e.ai.OnCreate(func(ctx context.Context) {
			if _, err := e.st.SetPrivate(ctx, userID, ids[0], true); err != nil {
				t.Errorf("SetPrivate: %v", err)
			}
		})
		e.run(t, e.passConfig(t))
		b := e.batches(t, userID)
		if len(b) != 1 || b[0].Status != store.BatchInProgress || b[0].ItemCount != 2 {
			t.Fatalf("batches = %+v, want one in progress with 2 items", b)
		}
		if it := e.item(t, userID, ids[0]); !it.IsPrivate || it.Status != "submitted" || it.BatchID == nil || *it.BatchID != b[0].ID {
			t.Errorf("item private %v, %s, batch %v; want private, submitted with %s", it.IsPrivate, it.Status, it.BatchID, b[0].ID)
		}
	})
}

// hasCounts tells whether a private_after_submit line carries removed and remaining.
func (e *env) hasCounts(t *testing.T, removed, remaining int) bool {
	t.Helper()
	for _, line := range e.logLines(t) {
		if line["outcome"] == "private_after_submit" && line["removed"] == float64(removed) && line["remaining"] == float64(remaining) {
			return true
		}
	}
	return false
}
