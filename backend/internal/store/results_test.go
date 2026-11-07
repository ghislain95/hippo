package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// resultsEnv is a store with a clock and two users, for the tests of the results of a batch.
type resultsEnv struct {
	st    *store.Store
	path  string
	clock *testClock
	users [2]string
	n     int // links captured so far, for distinct URLs and Anthropic ids
}

func newResultsEnv(t *testing.T) *resultsEnv {
	t.Helper()
	clock := newClock()
	st, path := storetest.OpenWithClock(t, clock.Now)
	e := &resultsEnv{st: st, path: path, clock: clock}
	e.users[0], e.users[1] = newUser(t, st, "owner"), newUser(t, st, "other")
	return e
}

// submitted captures n extracted links of userID and submits them with a new batch accepted by
// Anthropic (in progress). It returns the batch id and the item ids, in submission order.
func (e *resultsEnv) submitted(t *testing.T, userID string, n int) (string, []string) {
	t.Helper()
	ctx := context.Background()
	full := store.Extraction{Mode: "full", ContentText: ptr("Body")}
	ids := make([]string, 0, n)
	for range n {
		e.n++
		e.clock.Advance(time.Second)
		ids = append(ids, extracted(t, e.st, userID, link(fmt.Sprintf("https://example.com/r/%d", e.n)), full).ID)
	}
	batchID, moved, err := e.st.SubmitBatch(ctx, userID, testBatch, ids)
	if err != nil || len(moved) != n {
		t.Fatalf("SubmitBatch = %v, %v; want %d moved", moved, err, n)
	}
	if err := e.st.MarkBatchSent(ctx, userID, batchID, fmt.Sprintf("msgbatch_%d_%s", e.n, batchID)); err != nil {
		t.Fatalf("MarkBatchSent: %v", err)
	}
	e.clock.Advance(time.Minute)
	return batchID, moved
}

func (e *resultsEnv) item(t *testing.T, userID, id string) store.Item {
	t.Helper()
	return item(t, e.st, userID, id)
}

// categoryID returns the id of the non-deleted category of userID named name.
func (e *resultsEnv) categoryID(t *testing.T, userID, name string) int64 {
	t.Helper()
	categories, err := e.st.Categories(context.Background(), userID)
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	for _, c := range categories {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("no category %q", name)
	return 0
}

func (e *resultsEnv) settle(t *testing.T, userID, batchID, itemID string, outcome store.Outcome) store.Settlement {
	t.Helper()
	settled, err := e.st.SettleItem(context.Background(), userID, batchID, itemID, outcome)
	if err != nil {
		t.Fatalf("SettleItem(%s): %v", outcome, err)
	}
	return settled
}

// assertWritten checks that the item was written after before: a greater change_seq, and an
// updated_at of the store's clock, later than its submission (the storetest helpers stamp
// their writes with the real clock, so before's updated_at is not compared).
func (e *resultsEnv) assertWritten(t *testing.T, before, after store.Item) {
	t.Helper()
	if after.ChangeSeq <= before.ChangeSeq || !after.UpdatedAt.Equal(e.clock.Now()) {
		t.Errorf("change_seq %d → %d, updated_at %v; want the sequence advanced and updated_at %v",
			before.ChangeSeq, after.ChangeSeq, after.UpdatedAt, e.clock.Now())
	}
}

// assertUnchanged checks that the item was not written.
func assertUnchanged(t *testing.T, before, after store.Item) {
	t.Helper()
	if after.ChangeSeq != before.ChangeSeq || after.Status != before.Status || after.Attempts != before.Attempts ||
		!after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("item changed: %+v, then %+v", before, after)
	}
}

func TestSettleItem(t *testing.T) {
	t.Run("a requeued item goes back to extracted without batch nor attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetAnalysisState(t, e.path, e.users[0], ids[0], "submitted", nil, 1)
		storetest.SetLastError(t, e.path, e.users[0], ids[0], "request_errored")
		before := e.item(t, e.users[0], ids[0])

		if got := e.settle(t, e.users[0], batchID, ids[0], store.OutcomeRequeue); got != store.SettledExtracted {
			t.Errorf("settlement = %s, want extracted", got)
		}
		after := e.item(t, e.users[0], ids[0])
		if after.Status != "extracted" || after.BatchID != nil || after.Attempts != 1 ||
			after.LastError == nil || *after.LastError != "request_errored" {
			t.Errorf("item = %s, batch %v, attempts %d, last_error %v; want extracted without batch, attempts 1, request_errored",
				after.Status, after.BatchID, after.Attempts, after.LastError)
		}
		e.assertWritten(t, before, after)
	})

	t.Run("a first cut response is marked retried without attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetAnalysisState(t, e.path, e.users[0], ids[0], "submitted", nil, 1)
		before := e.item(t, e.users[0], ids[0])

		if got := e.settle(t, e.users[0], batchID, ids[0], store.OutcomeCut); got != store.SettledExtracted {
			t.Errorf("settlement = %s, want extracted", got)
		}
		after := e.item(t, e.users[0], ids[0])
		_, retried := store.AnalysisColumnsForTest(t, e.st, e.users[0], ids[0])
		if after.Status != "extracted" || after.BatchID != nil || after.Attempts != 1 || after.LastError != nil || !retried {
			t.Errorf("item = %s, batch %v, attempts %d, last_error %v, retried %v; want extracted, no batch, attempts 1, retried",
				after.Status, after.BatchID, after.Attempts, after.LastError, retried)
		}
		e.assertWritten(t, before, after)
	})

	t.Run("a second cut response fails the item as too long and keeps its batch", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetIncompleteRetried(t, e.path, e.users[0], ids[0])
		storetest.SetAnalysisState(t, e.path, e.users[0], ids[0], "submitted", nil, 1)
		before := e.item(t, e.users[0], ids[0])

		if got := e.settle(t, e.users[0], batchID, ids[0], store.OutcomeCut); got != store.SettledFailed {
			t.Errorf("settlement = %s, want failed", got)
		}
		after := e.item(t, e.users[0], ids[0])
		if after.Status != "failed" || after.Attempts != 1 || after.LastError == nil || *after.LastError != store.LastErrorTooLong ||
			after.BatchID == nil || *after.BatchID != batchID {
			t.Errorf("item = %s, attempts %d, last_error %v, batch %v; want failed, attempts 1, too_long, batch kept",
				after.Status, after.Attempts, after.LastError, after.BatchID)
		}
		e.assertWritten(t, before, after)
	})

	for _, tc := range []struct {
		outcome store.Outcome
		code    string
	}{
		{store.OutcomeInvalid, store.LastErrorInvalidResponse},
		{store.OutcomeErrored, store.LastErrorRequestErrored},
		{store.OutcomeStorageFailed, store.LastErrorStorageFailed},
	} {
		t.Run(fmt.Sprintf("an %s outcome counts an attempt and clears the batch", tc.outcome), func(t *testing.T) {
			e := newResultsEnv(t)
			batchID, ids := e.submitted(t, e.users[0], 1)
			before := e.item(t, e.users[0], ids[0])

			if got := e.settle(t, e.users[0], batchID, ids[0], tc.outcome); got != store.SettledExtracted {
				t.Errorf("settlement = %s, want extracted", got)
			}
			after := e.item(t, e.users[0], ids[0])
			if after.Status != "extracted" || after.BatchID != nil || after.Attempts != 1 || after.LastError == nil ||
				*after.LastError != tc.code {
				t.Errorf("item = %s, batch %v, attempts %d, last_error %v; want extracted, no batch, attempts 1, %s",
					after.Status, after.BatchID, after.Attempts, after.LastError, tc.code)
			}
			e.assertWritten(t, before, after)
		})

		t.Run(fmt.Sprintf("the third %s outcome fails the item and keeps its batch", tc.outcome), func(t *testing.T) {
			e := newResultsEnv(t)
			batchID, ids := e.submitted(t, e.users[0], 2)
			storetest.SetAnalysisState(t, e.path, e.users[0], ids[0], "submitted", nil, 2)
			storetest.SetAnalysisState(t, e.path, e.users[0], ids[1], "submitted", nil, 1)

			if got := e.settle(t, e.users[0], batchID, ids[0], tc.outcome); got != store.SettledFailed {
				t.Errorf("settlement at the third attempt = %s, want failed", got)
			}
			if got := e.settle(t, e.users[0], batchID, ids[1], tc.outcome); got != store.SettledExtracted {
				t.Errorf("settlement at the second attempt = %s, want extracted", got)
			}
			third, second := e.item(t, e.users[0], ids[0]), e.item(t, e.users[0], ids[1])
			if third.Status != "failed" || third.Attempts != 3 || third.LastError == nil || *third.LastError != tc.code ||
				third.BatchID == nil || *third.BatchID != batchID {
				t.Errorf("third attempt: %s, attempts %d, last_error %v, batch %v; want failed, 3, %s, batch kept",
					third.Status, third.Attempts, third.LastError, third.BatchID, tc.code)
			}
			if second.Status != "extracted" || second.Attempts != 2 {
				t.Errorf("second attempt: %s, attempts %d; want extracted, 2", second.Status, second.Attempts)
			}
		})
	}

	t.Run("an item no longer submitted in the batch is not written", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		e.settle(t, e.users[0], batchID, ids[0], store.OutcomeRequeue)
		before := e.item(t, e.users[0], ids[0])
		// The same item submitted with a later batch is not this batch's either.
		later, _, err := e.st.SubmitBatch(context.Background(), e.users[0], testBatch, ids)
		if err != nil {
			t.Fatalf("SubmitBatch: %v", err)
		}
		resubmitted := e.item(t, e.users[0], ids[0])

		for _, outcome := range []store.Outcome{
			store.OutcomeRequeue, store.OutcomeInvalid, store.OutcomeCut, store.OutcomeErrored, store.OutcomeStorageFailed,
		} {
			if got := e.settle(t, e.users[0], batchID, ids[0], outcome); got != store.SettledNotSubmitted {
				t.Errorf("%s: settlement = %s, want not_submitted", outcome, got)
			}
		}
		assertUnchanged(t, resubmitted, e.item(t, e.users[0], ids[0]))
		if before.Status != "extracted" || resubmitted.BatchID == nil || *resubmitted.BatchID != later {
			t.Errorf("set-up: item %s then batch %v, want extracted then %s", before.Status, resubmitted.BatchID, later)
		}
	})

	t.Run("another user's item is not written", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, _ := e.submitted(t, e.users[0], 1)
		otherBatch, otherIDs := e.submitted(t, e.users[1], 1)
		before := e.item(t, e.users[1], otherIDs[0])

		for _, batch := range []string{batchID, otherBatch} {
			if got := e.settle(t, e.users[0], batch, otherIDs[0], store.OutcomeInvalid); got != store.SettledNotSubmitted {
				t.Errorf("batch %s: settlement = %s, want not_submitted", batch, got)
			}
		}
		assertUnchanged(t, before, e.item(t, e.users[1], otherIDs[0]))
	})

	t.Run("a deleted item is not written", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.DeleteItem(t, e.path, e.users[0], ids[0])
		seq := store.SequenceForTest(t, e.st)

		if got := e.settle(t, e.users[0], batchID, ids[0], store.OutcomeInvalid); got != store.SettledDeleted {
			t.Errorf("settlement = %s, want deleted", got)
		}
		if got := store.SequenceForTest(t, e.st); got != seq {
			t.Errorf("sequence = %d after settling a deleted item, want %d", got, seq)
		}
	})

	t.Run("an unknown item is not written", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, _ := e.submitted(t, e.users[0], 1)
		if got := e.settle(t, e.users[0], batchID, "01JB8Z6Q4M1X2Y3Z4A5B6C7DZZ", store.OutcomeRequeue); got != store.SettledNotSubmitted {
			t.Errorf("settlement = %s, want not_submitted", got)
		}
	})
}

// tipFiche is a fiche of the tip template, as the analysis stores it (compacted).
var tipFiche = json.RawMessage(`{"template":"tip","summary":"A tip","steps":["Do it"]}`)

// genericFiche is a fiche of the generic template.
var genericFiche = json.RawMessage(`{"template":"generic","summary":"Something"}`)

// validTip is a valid response choosing the type tip, the category categoryID and two tags.
func validTip(categoryID int64, tags ...string) store.Analysis {
	if tags == nil {
		tags = []string{"go", "testing"}
	}
	return store.Analysis{
		Type:          store.TypeChoice{Key: "tip"},
		Template:      "tip",
		Category:      store.CategoryChoice{ID: categoryID},
		Tags:          tags,
		Title:         "Run the tests with the race detector",
		FicheJSON:     tipFiche,
		SearchText:    "A tip\nDo it",
		Language:      "en",
		Freshness:     "timeless",
		ContentOK:     true,
		SchemaVersion: 1,
	}
}

// withProposedCategory is a valid response proposing the category name under parentID.
func withProposedCategory(a store.Analysis, name string, parentID *int64) store.Analysis {
	a.Category = store.CategoryChoice{Proposed: &store.ProposedCategory{
		Name: name, ParentID: parentID, Description: "Description of " + name, Reason: "No category fits",
	}}
	return a
}

func (e *resultsEnv) write(t *testing.T, userID, batchID, itemID string, a store.Analysis) store.AnalysisWrite {
	t.Helper()
	written, err := e.st.WriteAnalysis(context.Background(), userID, batchID, itemID, a)
	if err != nil {
		t.Fatalf("WriteAnalysis: %v", err)
	}
	return written
}

// mustAnalyse writes a and fails the test unless the item is analysed.
func (e *resultsEnv) mustAnalyse(t *testing.T, userID, batchID, itemID string, a store.Analysis) store.Item {
	t.Helper()
	if got := e.write(t, userID, batchID, itemID, a); got.Settlement != store.SettledAnalyzed {
		t.Fatalf("WriteAnalysis = %+v, want analyzed", got)
	}
	return e.item(t, userID, itemID)
}

// categoryNamed returns the non-deleted category rows of userID named name (any case).
func categoryNamed(t *testing.T, st *store.Store, userID, name string) []store.CategoryRowForTest {
	t.Helper()
	var found []store.CategoryRowForTest
	for _, row := range store.CategoryRowsForTest(t, st, userID) {
		if row.Name == name && !row.Deleted {
			found = append(found, row)
		}
	}
	return found
}

func TestWriteAnalysis(t *testing.T) {
	t.Run("A valid response analyses the item", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetIncompleteRetried(t, e.path, e.users[0], ids[0])
		storetest.SetAnalysisState(t, e.path, e.users[0], ids[0], "submitted", nil, 2)
		storetest.SetLastError(t, e.path, e.users[0], ids[0], "invalid_response")
		storetest.SetBatchEnvelopeVersion(t, e.path, e.users[0], batchID, 1)
		dev := e.categoryID(t, e.users[0], "Dev")
		before := e.item(t, e.users[0], ids[0])
		issue := "paywall"
		a := validTip(dev)
		a.ContentOK, a.ContentIssue = false, &issue

		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], a)
		if got.Status != "analyzed" || got.TypeKey == nil || *got.TypeKey != "tip" || got.CategoryID == nil || *got.CategoryID != dev ||
			got.Title != a.Title || string(got.FicheJSON) != string(tipFiche) {
			t.Errorf("item = %s, type %v, category %v, title %q, fiche %s; want analyzed tip in Dev with the response's title and fiche",
				got.Status, got.TypeKey, got.CategoryID, got.Title, got.FicheJSON)
		}
		if got.Language == nil || *got.Language != "en" || got.Freshness == nil || *got.Freshness != "timeless" ||
			got.ContentOK == nil || *got.ContentOK || got.ContentIssue == nil || *got.ContentIssue != "paywall" {
			t.Errorf("language %v, freshness %v, content_ok %v, issue %v; want en, timeless, false, paywall",
				got.Language, got.Freshness, got.ContentOK, got.ContentIssue)
		}
		if got.Model == nil || *got.Model != testBatch.Model || got.PromptVersion == nil || *got.PromptVersion != 3 ||
			got.EnvelopeVersion == nil || *got.EnvelopeVersion != 1 || got.SchemaVersion == nil || *got.SchemaVersion != 1 {
			t.Errorf("model %v, prompt %v, envelope %v, schema %v; want the batch's %s, 3, 1 and schema 1",
				got.Model, got.PromptVersion, got.EnvelopeVersion, got.SchemaVersion, testBatch.Model)
		}
		if got.Attempts != 0 || got.LastError != nil || got.AnalyzedAt == nil || !got.AnalyzedAt.Equal(e.clock.Now()) ||
			got.BatchID == nil || *got.BatchID != batchID {
			t.Errorf("attempts %d, last_error %v, analyzed_at %v, batch %v; want 0, nil, now, batch kept",
				got.Attempts, got.LastError, got.AnalyzedAt, got.BatchID)
		}
		searchText, retried := store.AnalysisColumnsForTest(t, e.st, e.users[0], ids[0])
		if searchText != a.SearchText || retried {
			t.Errorf("search_text %q, incomplete_retried %v; want %q, false", searchText, retried, a.SearchText)
		}
		tags := store.TagRowsForTest(t, e.st, e.users[0])
		if len(tags) != 2 || len(got.TagIDs) != 2 || got.TagIDs[0] != tags[0].ID || got.TagIDs[1] != tags[1].ID ||
			tags[0].Name != "go" || tags[1].Name != "testing" {
			t.Errorf("tags = %+v, item tags %v; want go and testing, both on the item", tags, got.TagIDs)
		}
		e.assertWritten(t, before, got)
	})

	t.Run("The versions come from the batch", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetBatchEnvelopeVersion(t, e.path, e.users[0], batchID, 4)
		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev")))
		if *got.Model != "claude-haiku-4-5" || *got.PromptVersion != 3 || *got.EnvelopeVersion != 4 {
			t.Errorf("model %s, prompt %d, envelope %d; want claude-haiku-4-5, 3, 4", *got.Model, *got.PromptVersion, *got.EnvelopeVersion)
		}
	})

	t.Run("A proposed category is created", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		infra := e.categoryID(t, e.users[0], "Infra & DevOps")
		seq := store.SequenceForTest(t, e.st)

		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], withProposedCategory(validTip(0), "Kubernetes", &infra))
		created := categoryNamed(t, e.st, e.users[0], "Kubernetes")
		if len(created) != 1 {
			t.Fatalf("categories Kubernetes = %d, want 1", len(created))
		}
		c := created[0]
		if c.ParentID == nil || *c.ParentID != infra || c.Status != "proposed" || c.Description == nil ||
			*c.Description != "Description of Kubernetes" || c.ProposalReason == nil || *c.ProposalReason != "No category fits" ||
			c.Color != nil || c.ChangeSeq <= seq {
			t.Errorf("category = %+v, want proposed under Infra & DevOps with its description, reason, no colour, a new change_seq", c)
		}
		if got.CategoryID == nil || *got.CategoryID != c.ID {
			t.Errorf("item category = %v, want %d", got.CategoryID, c.ID)
		}
	})

	t.Run("A proposed root category is created", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], withProposedCategory(validTip(0), "Photo", nil))
		created := categoryNamed(t, e.st, e.users[0], "Photo")
		if len(created) != 1 || created[0].ParentID != nil || created[0].Status != "proposed" || *got.CategoryID != created[0].ID {
			t.Errorf("Photo = %+v, item category %v; want one proposed root holding the item", created, got.CategoryID)
		}
	})

	parentCases := []struct {
		name, category string
		parent         func(t *testing.T, e *resultsEnv) *int64
		wantParent     func(t *testing.T, e *resultsEnv) *int64
	}{
		{
			name: "A sub-category as parent is replaced by its root", category: "Kubernetes",
			parent: func(t *testing.T, e *resultsEnv) *int64 {
				return ptr(e.categoryID(t, e.users[0], "Conteneurs & orchestration"))
			},
			wantParent: func(t *testing.T, e *resultsEnv) *int64 { return ptr(e.categoryID(t, e.users[0], "Infra & DevOps")) },
		},
		{
			name: "An unknown parent makes a root", category: "Photo",
			parent:     func(*testing.T, *resultsEnv) *int64 { return ptr(int64(99999)) },
			wantParent: func(*testing.T, *resultsEnv) *int64 { return nil },
		},
		{
			name: "A deleted parent makes a root", category: "Bonsaï",
			parent: func(t *testing.T, e *resultsEnv) *int64 {
				plantes := e.categoryID(t, e.users[0], "Plantes")
				storetest.DeleteCategory(t, e.path, e.users[0], plantes)
				return &plantes
			},
			wantParent: func(*testing.T, *resultsEnv) *int64 { return nil },
		},
		{
			name: "Another user's category as parent makes a root", category: "Photo",
			parent:     func(t *testing.T, e *resultsEnv) *int64 { return ptr(e.categoryID(t, e.users[1], "Dev")) },
			wantParent: func(*testing.T, *resultsEnv) *int64 { return nil },
		},
		{
			name: "A sub-category whose root is deleted as parent makes a root", category: "Bonsaï",
			parent: func(t *testing.T, e *resultsEnv) *int64 {
				maison := e.categoryID(t, e.users[0], "Maison & quotidien")
				menage := e.categoryID(t, e.users[0], "Ménage")
				storetest.DeleteCategory(t, e.path, e.users[0], maison)
				return &menage
			},
			wantParent: func(*testing.T, *resultsEnv) *int64 { return nil },
		},
	}
	for _, tc := range parentCases {
		t.Run(tc.name, func(t *testing.T) {
			e := newResultsEnv(t)
			batchID, ids := e.submitted(t, e.users[0], 1)
			parent := tc.parent(t, e)
			want := tc.wantParent(t, e)
			others := store.CategoryRowsForTest(t, e.st, e.users[1])

			got := e.mustAnalyse(t, e.users[0], batchID, ids[0], withProposedCategory(validTip(0), tc.category, parent))
			created := categoryNamed(t, e.st, e.users[0], tc.category)
			if len(created) != 1 {
				t.Fatalf("categories %q = %d, want 1", tc.category, len(created))
			}
			if (want == nil) != (created[0].ParentID == nil) || (want != nil && *want != *created[0].ParentID) {
				t.Errorf("parent = %v, want %v", created[0].ParentID, want)
			}
			if got.Status != "analyzed" || *got.CategoryID != created[0].ID {
				t.Errorf("item = %s in %v, want analyzed in %d", got.Status, got.CategoryID, created[0].ID)
			}
			if after := store.CategoryRowsForTest(t, e.st, e.users[1]); len(after) != len(others) {
				t.Errorf("other user's categories = %d, want %d", len(after), len(others))
			}
		})
	}

	matchCases := []struct {
		name       string
		setUp      func(t *testing.T, e *resultsEnv) // optional
		proposed   string
		parent     string // "" for a root
		reuse      string // the category the item is filed in, "" when one is created
		wantParent string // the parent of the created category, "" for a root
	}{
		{name: "An existing active category is reused", proposed: " ménage", parent: "Maison & quotidien", reuse: "Ménage"},
		{
			name: "An existing proposed category is reused", proposed: "KUBERNETES", parent: "Infra & DevOps", reuse: "Kubernetes",
			setUp: func(t *testing.T, e *resultsEnv) {
				storetest.AddCategory(t, e.path, e.users[0], storetest.NewCategory{
					Name: "Kubernetes", ParentID: ptr(e.categoryID(t, e.users[0], "Infra & DevOps")), Status: "proposed",
				})
			},
		},
		{name: "Inner spaces are collapsed", proposed: "Java  &  Spring", parent: "Dev", reuse: "Java & Spring"},
		{name: "Different words make another category", proposed: "Java et Spring", parent: "Dev", wantParent: "Dev"},
		{
			name: "The same name under another parent makes another category", proposed: "Kubernetes",
			setUp: func(t *testing.T, e *resultsEnv) {
				storetest.AddCategory(t, e.path, e.users[0], storetest.NewCategory{
					Name: "Kubernetes", ParentID: ptr(e.categoryID(t, e.users[0], "Infra & DevOps")), Status: "proposed",
				})
			},
		},
		{
			name: "The corrected parent is used for the match", proposed: "Kubernetes", parent: "Conteneurs & orchestration",
			reuse: "Kubernetes",
			setUp: func(t *testing.T, e *resultsEnv) {
				storetest.AddCategory(t, e.path, e.users[0], storetest.NewCategory{
					Name: "Kubernetes", ParentID: ptr(e.categoryID(t, e.users[0], "Infra & DevOps")), Status: "proposed",
				})
			},
		},
		{
			name: "A deleted category of the same name is not reused", proposed: "Plantes",
			setUp: func(t *testing.T, e *resultsEnv) {
				storetest.DeleteCategory(t, e.path, e.users[0], e.categoryID(t, e.users[0], "Plantes"))
			},
		},
	}
	for _, tc := range matchCases {
		t.Run(tc.name, func(t *testing.T) {
			e := newResultsEnv(t)
			batchID, ids := e.submitted(t, e.users[0], 1)
			if tc.setUp != nil {
				tc.setUp(t, e)
			}
			var parent *int64
			if tc.parent != "" {
				parent = ptr(e.categoryID(t, e.users[0], tc.parent))
			}
			var reused int64
			if tc.reuse != "" {
				reused = e.categoryID(t, e.users[0], tc.reuse)
			}
			before := store.CategoryRowsForTest(t, e.st, e.users[0])

			got := e.mustAnalyse(t, e.users[0], batchID, ids[0], withProposedCategory(validTip(0), tc.proposed, parent))
			after := store.CategoryRowsForTest(t, e.st, e.users[0])
			switch {
			case tc.reuse != "":
				if len(after) != len(before) || *got.CategoryID != reused {
					t.Errorf("categories %d → %d, item in %d; want none created and the item in %q (%d)",
						len(before), len(after), *got.CategoryID, tc.reuse, reused)
				}
			default:
				if len(after) != len(before)+1 {
					t.Fatalf("categories %d → %d, want one created", len(before), len(after))
				}
				created := after[len(after)-1]
				if created.Name != tc.proposed || created.Status != "proposed" || created.Deleted || *got.CategoryID != created.ID {
					t.Errorf("created %+v, item in %d; want a proposed %q holding the item", created, *got.CategoryID, tc.proposed)
				}
				switch {
				case tc.wantParent == "" && created.ParentID != nil:
					t.Errorf("created under %d, want a root", *created.ParentID)
				case tc.wantParent != "" && (created.ParentID == nil || *created.ParentID != e.categoryID(t, e.users[0], tc.wantParent)):
					t.Errorf("created under %v, want under %q", created.ParentID, tc.wantParent)
				}
			}
			for i := range before {
				if before[i].ChangeSeq != after[i].ChangeSeq || before[i].Deleted != after[i].Deleted {
					t.Errorf("existing category %d changed", before[i].ID)
				}
			}
		})
	}

	t.Run("Two items of a batch proposing the same category get one", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 2)
		infra := e.categoryID(t, e.users[0], "Infra & DevOps")
		first := e.mustAnalyse(t, e.users[0], batchID, ids[0], withProposedCategory(validTip(0), "Kubernetes", &infra))
		second := e.mustAnalyse(t, e.users[0], batchID, ids[1], withProposedCategory(validTip(0), "Kubernetes", &infra))
		if created := categoryNamed(t, e.st, e.users[0], "Kubernetes"); len(created) != 1 ||
			*first.CategoryID != created[0].ID || *second.CategoryID != created[0].ID {
			t.Errorf("Kubernetes = %+v, items in %d and %d; want one holding both", created, *first.CategoryID, *second.CategoryID)
		}
	})

	t.Run("A proposed type is created", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		seq := store.SequenceForTest(t, e.st)
		a := validTip(e.categoryID(t, e.users[0], "Dev"))
		a.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "event", Name: "Évènement", Reason: "A meetup"}}
		a.Template, a.FicheJSON = "generic", genericFiche

		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], a)
		rows := store.ContentTypeRowsForTest(t, e.st)
		event := rows[len(rows)-1]
		if len(rows) != 8 || event.Key != "event" || event.Name != "Évènement" || event.Status != "proposed" || event.HasTemplate != 0 ||
			event.ProposalReason == nil || *event.ProposalReason != "A meetup" || event.ChangeSeq <= seq || event.Deleted {
			t.Errorf("types = %d, last %+v; want 8, event proposed without template with its name, reason and change_seq", len(rows), event)
		}
		if *got.TypeKey != "event" || string(got.FicheJSON) != string(genericFiche) {
			t.Errorf("item type %s, fiche %s; want event with its generic fiche", *got.TypeKey, got.FicheJSON)
		}
	})

	t.Run("Two items of a batch proposing the same type get one", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 2)
		a := validTip(e.categoryID(t, e.users[0], "Dev"))
		a.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "event", Name: "Évènement", Reason: "A meetup"}}
		a.Template, a.FicheJSON = "generic", genericFiche
		e.mustAnalyse(t, e.users[0], batchID, ids[0], a)
		a.Type.Proposed.Name = "Évènement à venir"
		second := e.mustAnalyse(t, e.users[0], batchID, ids[1], a)
		rows := store.ContentTypeRowsForTest(t, e.st)
		if len(rows) != 8 || rows[7].Name != "Évènement" || *second.TypeKey != "event" {
			t.Errorf("types = %d, event named %q, second item type %s; want 8, the first name, event", len(rows), rows[7].Name, *second.TypeKey)
		}
	})

	t.Run("A proposed type whose key exists is filed under the existing type", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		before := store.ContentTypeRowsForTest(t, e.st)
		a := validTip(e.categoryID(t, e.users[0], "Cuisine"))
		a.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "recipe", Name: "Recette maison", Reason: "Homemade"}}
		a.Template, a.FicheJSON = "generic", genericFiche

		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], a)
		after := store.ContentTypeRowsForTest(t, e.st)
		if len(after) != len(before) || after[3] != before[3] {
			t.Errorf("types %d → %d, recipe %+v → %+v; want none created, recipe unchanged", len(before), len(after), before[3], after[3])
		}
		if *got.TypeKey != "recipe" || string(got.FicheJSON) != string(genericFiche) {
			t.Errorf("item type %s, fiche %s; want recipe with its generic fiche", *got.TypeKey, got.FicheJSON)
		}
	})

	t.Run("Existing classification rows are left as they were", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 2)
		goID := storetest.AddTag(t, e.path, e.users[0], "go")
		storetest.DeleteTag(t, e.path, e.users[0], storetest.AddTag(t, e.path, e.users[0], "old"))
		maison := e.categoryID(t, e.users[0], "Maison & quotidien")
		categories, types := store.CategoryRowsForTest(t, e.st, e.users[0]), store.ContentTypeRowsForTest(t, e.st)
		tags := store.TagRowsForTest(t, e.st, e.users[0])

		reuse := withProposedCategory(validTip(0, "go", "testing"), "MÉNAGE", &maison)
		reuse.Category.Proposed.Description = "Another description"
		e.mustAnalyse(t, e.users[0], batchID, ids[0], reuse)
		recipe := validTip(e.categoryID(t, e.users[0], "Cuisine"), "go", "cuisine")
		recipe.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "recipe", Name: "Autre nom", Reason: "Other"}}
		recipe.Template, recipe.FicheJSON = "generic", genericFiche
		e.mustAnalyse(t, e.users[0], batchID, ids[1], recipe)

		afterCategories, afterTypes := store.CategoryRowsForTest(t, e.st, e.users[0]), store.ContentTypeRowsForTest(t, e.st)
		afterTags := store.TagRowsForTest(t, e.st, e.users[0])
		if len(afterCategories) != len(categories) || len(afterTypes) != len(types) {
			t.Fatalf("categories %d → %d, types %d → %d; want none created", len(categories), len(afterCategories), len(types), len(afterTypes))
		}
		for i := range categories {
			b, a := categories[i], afterCategories[i]
			if b.Name != a.Name || b.Status != a.Status || b.Deleted != a.Deleted || b.ChangeSeq != a.ChangeSeq ||
				b.UpdatedAt != a.UpdatedAt || (b.Description == nil) != (a.Description == nil) ||
				(b.Description != nil && *b.Description != *a.Description) || (b.ParentID == nil) != (a.ParentID == nil) ||
				(b.Color == nil) != (a.Color == nil) {
				t.Errorf("category %d changed: %+v, then %+v", b.ID, b, a)
			}
		}
		for i := range types {
			if (types[i].ProposalReason == nil) != (afterTypes[i].ProposalReason == nil) || types[i].Name != afterTypes[i].Name ||
				types[i].ChangeSeq != afterTypes[i].ChangeSeq || types[i].HasTemplate != afterTypes[i].HasTemplate ||
				types[i].Status != afterTypes[i].Status || types[i].Deleted != afterTypes[i].Deleted {
				t.Errorf("type %s changed: %+v, then %+v", types[i].Key, types[i], afterTypes[i])
			}
		}
		for i := range tags {
			if tags[i] != afterTags[i] {
				t.Errorf("tag %d changed: %+v, then %+v", tags[i].ID, tags[i], afterTags[i])
			}
		}
		if first := e.item(t, e.users[0], ids[0]); len(first.TagIDs) != 2 || first.TagIDs[0] != goID {
			t.Errorf("first item tags = %v, want the existing go (%d) first", first.TagIDs, goID)
		}
	})

	t.Run("An existing tag is reused", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		goID := storetest.AddTag(t, e.path, e.users[0], "go")
		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev"), "go", "testing"))
		tags := store.TagRowsForTest(t, e.st, e.users[0])
		if len(tags) != 2 || tags[0].ID != goID || tags[1].Name != "testing" || len(got.TagIDs) != 2 ||
			got.TagIDs[0] != goID || got.TagIDs[1] != tags[1].ID {
			t.Errorf("tags = %+v, item tags %v; want the existing go and a new testing on the item", tags, got.TagIDs)
		}
	})

	t.Run("A deleted tag of the same name is not reused", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		deleted := storetest.AddTag(t, e.path, e.users[0], "go")
		storetest.DeleteTag(t, e.path, e.users[0], deleted)
		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev"), "go", "testing"))
		if len(got.TagIDs) != 2 || got.TagIDs[0] == deleted || got.TagIDs[1] == deleted {
			t.Errorf("item tags = %v, want two new tags, not the deleted %d", got.TagIDs, deleted)
		}
	})

	t.Run("A new tag of two items of a batch is created once", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 2)
		dev := e.categoryID(t, e.users[0], "Dev")
		first := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(dev, "kubernetes", "go"))
		second := e.mustAnalyse(t, e.users[0], batchID, ids[1], validTip(dev, "kubernetes", "helm"))
		tags := store.TagRowsForTest(t, e.st, e.users[0])
		if len(tags) != 3 || tags[0].Name != "kubernetes" || first.TagIDs[0] != tags[0].ID || second.TagIDs[0] != tags[0].ID {
			t.Errorf("tags = %+v, items %v and %v; want one kubernetes on both", tags, first.TagIDs, second.TagIDs)
		}
	})

	t.Run("Another user's tag is not reused", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		otherGo := storetest.AddTag(t, e.path, e.users[1], "go")
		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev"), "go", "testing"))
		own := store.TagRowsForTest(t, e.st, e.users[0])
		if len(own) != 2 || own[0].Name != "go" || own[0].ID == otherGo || got.TagIDs[0] != own[0].ID {
			t.Errorf("user's tags = %+v, item tags %v; want an own go", own, got.TagIDs)
		}
		if n := store.ItemTagRowsForTest(t, e.st, e.users[1]); n != 0 {
			t.Errorf("other user's item_tags = %d, want 0", n)
		}
	})

	t.Run("the item's tags are replaced by the response's", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		old := storetest.AddTag(t, e.path, e.users[0], "old")
		storetest.AttachTag(t, e.path, e.users[0], ids[0], old)
		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev"), "go", "testing"))
		if len(got.TagIDs) != 2 || got.TagIDs[0] == old || got.TagIDs[1] == old {
			t.Errorf("item tags = %v, want go and testing only", got.TagIDs)
		}
	})

	// The database steps of the validation: an invalid response counts an attempt and writes
	// nothing else, whatever it proposes.
	rejections := []struct {
		name, reason string
		response     func(t *testing.T, e *resultsEnv) store.Analysis
	}{
		{"an unknown type is rejected", store.RejectUnknownType, func(t *testing.T, e *resultsEnv) store.Analysis {
			a := validTip(e.categoryID(t, e.users[0], "Dev"))
			a.Type = store.TypeChoice{Key: "book"}
			return a
		}},
		{"a template that does not match the type is rejected", store.RejectTemplate, func(t *testing.T, e *resultsEnv) store.Analysis {
			a := validTip(e.categoryID(t, e.users[0], "Dev"))
			a.Template = "recipe"
			return a
		}},
		{"a generic fiche for a type with its own template is rejected", store.RejectTemplate, func(t *testing.T, e *resultsEnv) store.Analysis {
			a := validTip(e.categoryID(t, e.users[0], "Dev"))
			a.Template, a.FicheJSON = "generic", genericFiche
			return a
		}},
		{"a proposed type with its own template is rejected", store.RejectTemplate, func(t *testing.T, e *resultsEnv) store.Analysis {
			a := validTip(e.categoryID(t, e.users[0], "Dev"))
			a.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "event", Name: "Évènement", Reason: "A meetup"}}
			return a
		}},
		{"a proposed type of an existing key with its own template is rejected", store.RejectTemplate, func(t *testing.T, e *resultsEnv) store.Analysis {
			a := validTip(e.categoryID(t, e.users[0], "Dev"))
			a.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "tip", Name: "Astuce", Reason: "Same"}}
			return a
		}},
		{"a category of another user is rejected", store.RejectUnknownCategory, func(t *testing.T, e *resultsEnv) store.Analysis {
			return validTip(e.categoryID(t, e.users[1], "Dev"))
		}},
		{"a deleted category is rejected", store.RejectUnknownCategory, func(t *testing.T, e *resultsEnv) store.Analysis {
			plantes := e.categoryID(t, e.users[0], "Plantes")
			storetest.DeleteCategory(t, e.path, e.users[0], plantes)
			return validTip(plantes)
		}},
		{"an unknown category is rejected", store.RejectUnknownCategory, func(*testing.T, *resultsEnv) store.Analysis {
			return validTip(99999)
		}},
		{"a rejected response creates no proposal", store.RejectUnknownCategory, func(t *testing.T, e *resultsEnv) store.Analysis {
			// A proposed type and new tags, with a category that is not the user's: the type and
			// tags would be created before the category step if the writes did not wait.
			a := validTip(e.categoryID(t, e.users[1], "Dev"), "alpha", "beta")
			a.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "event", Name: "Évènement", Reason: "A meetup"}}
			a.Template, a.FicheJSON = "generic", genericFiche
			return a
		}},
	}
	for _, tc := range rejections {
		t.Run(tc.name, func(t *testing.T) {
			e := newResultsEnv(t)
			batchID, ids := e.submitted(t, e.users[0], 1)
			a := tc.response(t, e)
			before := e.item(t, e.users[0], ids[0])
			categories, types := len(store.CategoryRowsForTest(t, e.st, e.users[0])), len(store.ContentTypeRowsForTest(t, e.st))

			got := e.write(t, e.users[0], batchID, ids[0], a)
			if got.Settlement != store.SettledExtracted || got.Reason != tc.reason {
				t.Errorf("WriteAnalysis = %+v, want extracted, reason %s", got, tc.reason)
			}
			after := e.item(t, e.users[0], ids[0])
			if after.Status != "extracted" || after.Attempts != 1 || after.LastError == nil || *after.LastError != store.LastErrorInvalidResponse ||
				after.FicheJSON != nil || after.TypeKey != nil || after.CategoryID != nil || after.Title != before.Title || after.BatchID != nil {
				t.Errorf("item = %+v; want extracted, attempts 1, invalid_response, nothing else written", after)
			}
			if n := len(store.CategoryRowsForTest(t, e.st, e.users[0])); n != categories {
				t.Errorf("categories %d → %d, want none created", categories, n)
			}
			if n := len(store.ContentTypeRowsForTest(t, e.st)); n != types {
				t.Errorf("types %d → %d, want none created", types, n)
			}
			if tags := store.TagRowsForTest(t, e.st, e.users[0]); len(tags) != 0 {
				t.Errorf("tags = %+v, want none", tags)
			}
		})
	}

	t.Run("the third rejected response fails the item", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetAnalysisState(t, e.path, e.users[0], ids[0], "submitted", nil, 2)
		got := e.write(t, e.users[0], batchID, ids[0], validTip(99999))
		after := e.item(t, e.users[0], ids[0])
		if got.Settlement != store.SettledFailed || after.Status != "failed" || after.Attempts != 3 || after.BatchID == nil {
			t.Errorf("WriteAnalysis = %+v, item %s attempts %d batch %v; want failed, 3, batch kept", got, after.Status, after.Attempts, after.BatchID)
		}
	})

	t.Run("a field corrected by hand is kept and proposes nothing", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetUserEdited(t, e.path, e.users[0], ids[0], "category", "content_type")
		categories := len(store.CategoryRowsForTest(t, e.st, e.users[0]))
		a := withProposedCategory(validTip(0), "Kubernetes", nil)
		a.Type = store.TypeChoice{Proposed: &store.ProposedType{Key: "event", Name: "Évènement", Reason: "A meetup"}}
		a.Template, a.FicheJSON = "generic", genericFiche

		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], a)
		if got.CategoryID != nil || got.TypeKey != nil || string(got.FicheJSON) != string(genericFiche) || got.Title != a.Title ||
			len(got.TagIDs) != 2 {
			t.Errorf("item category %v, type %v, fiche %s, title %q, tags %v; want category and type kept (none), the rest written",
				got.CategoryID, got.TypeKey, got.FicheJSON, got.Title, got.TagIDs)
		}
		if n := len(store.CategoryRowsForTest(t, e.st, e.users[0])); n != categories {
			t.Errorf("categories %d → %d, want none created", categories, n)
		}
		if n := len(store.ContentTypeRowsForTest(t, e.st)); n != 7 {
			t.Errorf("types = %d, want 7", n)
		}
	})

	t.Run("a category corrected by hand is still validated", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetUserEdited(t, e.path, e.users[0], ids[0], "category")
		if got := e.write(t, e.users[0], batchID, ids[0], validTip(99999)); got.Reason != store.RejectUnknownCategory {
			t.Errorf("WriteAnalysis = %+v, want rejected as unknown_category", got)
		}
	})

	t.Run("a type corrected by hand is still validated", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		storetest.SetUserEdited(t, e.path, e.users[0], ids[0], "content_type")
		a := validTip(e.categoryID(t, e.users[0], "Dev"))
		a.Type = store.TypeChoice{Key: "book"}
		got := e.write(t, e.users[0], batchID, ids[0], a)
		after := e.item(t, e.users[0], ids[0])
		if got.Settlement != store.SettledExtracted || got.Reason != store.RejectUnknownType || after.Attempts != 1 ||
			after.LastError == nil || *after.LastError != store.LastErrorInvalidResponse || after.FicheJSON != nil {
			t.Errorf("WriteAnalysis = %+v, item attempts %d, last_error %v; want rejected as unknown_type with one attempt",
				got, after.Attempts, after.LastError)
		}
	})

	t.Run("Tags corrected by hand are kept", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 1)
		perso := storetest.AddTag(t, e.path, e.users[0], "perso")
		storetest.AttachTag(t, e.path, e.users[0], ids[0], perso)
		storetest.SetUserEdited(t, e.path, e.users[0], ids[0], "tags")

		got := e.mustAnalyse(t, e.users[0], batchID, ids[0], validTip(e.categoryID(t, e.users[0], "Dev"), "alpha", "beta"))
		if len(got.TagIDs) != 1 || got.TagIDs[0] != perso {
			t.Errorf("item tags = %v, want only perso (%d)", got.TagIDs, perso)
		}
		if tags := store.TagRowsForTest(t, e.st, e.users[0]); len(tags) != 1 {
			t.Errorf("tags = %+v, want only perso", tags)
		}
	})

	t.Run("an item not submitted in the batch, another user's or deleted is not written", func(t *testing.T) {
		e := newResultsEnv(t)
		batchID, ids := e.submitted(t, e.users[0], 2)
		_, otherIDs := e.submitted(t, e.users[1], 1)
		e.settle(t, e.users[0], batchID, ids[0], store.OutcomeRequeue)
		storetest.DeleteItem(t, e.path, e.users[0], ids[1])
		infra := e.categoryID(t, e.users[0], "Infra & DevOps")
		a := withProposedCategory(validTip(0, "alpha", "beta"), "Kubernetes", &infra)
		seq := store.SequenceForTest(t, e.st)

		for _, tc := range []struct {
			itemID string
			want   store.Settlement
		}{{ids[0], store.SettledNotSubmitted}, {ids[1], store.SettledDeleted}, {otherIDs[0], store.SettledNotSubmitted}} {
			if got := e.write(t, e.users[0], batchID, tc.itemID, a); got.Settlement != tc.want {
				t.Errorf("item %s: WriteAnalysis = %+v, want %s", tc.itemID, got, tc.want)
			}
		}
		if got := store.SequenceForTest(t, e.st); got != seq {
			t.Errorf("sequence %d → %d, want nothing written", seq, got)
		}
		if found := categoryNamed(t, e.st, e.users[0], "Kubernetes"); len(found) != 0 {
			t.Errorf("Kubernetes created for a dropped result: %+v", found)
		}
	})
}
