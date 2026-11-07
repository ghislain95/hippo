package analysis_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/analysis/analysistest"
	"github.com/ghislain95/hippo/backend/internal/analysis/prompt"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// The tests of this file play the results of a batch through the real pass (followBatch, the
// unit Pass.Follow runs for each batch), the fake AI service and the shared fake clock, on a
// real database (apply-batch-results, tasks 4.4 to 4.6).

// ficheExamples are the answers of docs/prompt/schemas/examples, one per fiche template.
const ficheExamples = "../../../docs/prompt/schemas/examples"

// exampleFiche is the fiche of the example of template.
func exampleFiche(t *testing.T, template string) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ficheExamples, template+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Fiche json.RawMessage `json:"fiche"`
	}
	if err := json.Unmarshal(raw, &example); err != nil {
		t.Fatal(err)
	}
	return example.Fiche
}

// tipFiche is a valid tip fiche.
var tipFiche = json.RawMessage(`{"template":"tip","tip":"Une astuce fictive.","steps":["Faire ceci."],"materials":[],"warnings":[]}`)

// input is a classify_item input, built as a map so that a test can remove or change a member.
type input map[string]any

// tipInput is a valid input choosing the type tip and the category categoryID.
func tipInput(categoryID int64) input {
	return input{
		"type": map[string]any{"key": "tip"}, "category": map[string]any{"id": categoryID},
		"tags": []any{"go", "testing"}, "title": "Un titre fictif", "fiche": tipFiche,
		"language": "en", "freshness": "timeless", "content_ok": true, "content_issue": nil,
	}
}

// with returns a copy of in with key set to value.
func (in input) with(key string, value any) input {
	out := maps.Clone(in)
	out[key] = value
	return out
}

// without returns a copy of in without key.
func (in input) without(key string) input {
	out := maps.Clone(in)
	delete(out, key)
	return out
}

func (in input) raw(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any(in))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// call is one classify_item call of in.
func (in input) call(t *testing.T) analysis.ToolCall {
	t.Helper()
	return analysis.ToolCall{Name: "classify_item", Input: in.raw(t)}
}

// succeeded is a succeeded line of itemID stopped at tool_use, with calls and 100 input tokens.
func succeeded(itemID string, calls ...analysis.ToolCall) analysis.Result {
	return analysis.Result{
		CustomID: itemID, Kind: analysis.ResultSucceeded, StopReason: "tool_use", ToolCalls: calls,
		Usage: analysis.Usage{Input: 100},
	}
}

// valid is a succeeded line of itemID with the single call of in.
func valid(t *testing.T, itemID string, in input) analysis.Result {
	t.Helper()
	return succeeded(itemID, in.call(t))
}

// resultsEnv is a pass env with one user and the configuration of their pass.
type resultsEnv struct {
	*passEnv
	userID string
	cfg    analysis.PassConfig
	prompt prompt.Prompt
}

func newResultsEnv(t *testing.T) *resultsEnv {
	t.Helper()
	e := newPassEnv(t)
	cfg := e.passConfig(t)
	return &resultsEnv{passEnv: e, userID: e.user(t, "owner"), cfg: cfg, prompt: cfg.Prompt}
}

// items captures n links of userID with their content, waiting for the batch.
func (e *resultsEnv) items(t *testing.T, userID string, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := range n {
		ids[i] = e.link(t, userID, fmt.Sprintf("https://example.com/%s/%d/%d", userID, len(e.batches(t, userID)), i), nil, full)
	}
	return ids
}

// sent submits ids of userID in a batch recorded with nb, accepted by the service, as the
// nightly pass leaves it, and returns the batch row.
func (e *resultsEnv) sent(t *testing.T, userID string, nb store.NewBatch, ids ...string) store.Batch {
	t.Helper()
	ctx := context.Background()
	batchID, moved, err := e.st.SubmitBatch(ctx, userID, nb, ids)
	if err != nil || len(moved) != len(ids) {
		t.Fatalf("SubmitBatch = %v, %v", moved, err)
	}
	if err := e.st.MarkBatchSent(ctx, userID, batchID, "msgbatch_"+batchID); err != nil {
		t.Fatalf("MarkBatchSent: %v", err)
	}
	return e.batch(t, userID, batchID)
}

// newBatch is the batch the running configuration sends.
func (e *resultsEnv) newBatch() store.NewBatch {
	return store.NewBatch{Model: testModel, PromptVersion: e.prompt.Version, EnvelopeVersion: e.prompt.EnvelopeVersion}
}

// submitted captures n items of the user and sends them in one batch.
func (e *resultsEnv) submitted(t *testing.T, n int) (store.Batch, []string) {
	t.Helper()
	ids := e.items(t, e.userID, n)
	return e.sent(t, e.userID, e.newBatch(), ids...), ids
}

func (e *resultsEnv) batch(t *testing.T, userID, batchID string) store.Batch {
	t.Helper()
	for _, b := range e.batches(t, userID) {
		if b.ID == batchID {
			return b
		}
	}
	t.Fatalf("no batch %s", batchID)
	return store.Batch{}
}

// ended scripts the batch b as ended with a results file of results.
func (e *resultsEnv) ended(b store.Batch, results ...analysis.Result) {
	e.endedLines(b, analysistest.Lines(results...)...)
}

func (e *resultsEnv) endedLines(b store.Batch, lines ...analysistest.Line) {
	e.ai.ScriptStates(*b.AnthropicBatchID, analysistest.Ended(e.clock.Now().Add(-time.Minute)))
	e.ai.ScriptResults(*b.AnthropicBatchID, analysistest.Download{Lines: lines})
}

// follow follows the batch b of userID once, with a new pass of cfg, an hour later than the
// clock was.
func (e *resultsEnv) follow(t *testing.T, cfg analysis.PassConfig, userID string, b store.Batch) analysis.FollowReport {
	t.Helper()
	e.clock.Advance(time.Hour)
	pass, err := analysis.NewPass(cfg)
	if err != nil {
		t.Fatalf("NewPass: %v", err)
	}
	return analysis.FollowBatch(context.Background(), pass, userID, b)
}

// apply scripts b as ended with results and follows it with the user's configuration; it
// fails the test unless the batch ended.
func (e *resultsEnv) apply(t *testing.T, b store.Batch, results ...analysis.Result) analysis.FollowReport {
	t.Helper()
	e.ended(b, results...)
	report := e.follow(t, e.cfg, e.userID, b)
	if report.Outcome != "ended" {
		t.Fatalf("follow-up outcome %q (%v), want ended", report.Outcome, report.Err)
	}
	return report
}

// wantRejected checks that the item id of the user was rejected with attempts counted:
// extracted, without batch, last_error invalid_response, and nothing of the response written.
func (e *resultsEnv) wantRejected(t *testing.T, id string, attempts int64) {
	t.Helper()
	it := e.item(t, e.userID, id)
	if it.Status != "extracted" || it.Attempts != attempts || it.LastError == nil || *it.LastError != store.LastErrorInvalidResponse ||
		it.BatchID != nil {
		t.Errorf("item %s is %s with %d attempts, last_error %v, batch %v; want extracted, %d, invalid_response, no batch",
			id, it.Status, it.Attempts, it.LastError, it.BatchID, attempts)
	}
	e.wantNothingWritten(t, it)
}

// wantNothingWritten checks that it carries nothing of a response.
func (e *resultsEnv) wantNothingWritten(t *testing.T, it store.Item) {
	t.Helper()
	if it.FicheJSON != nil || it.TypeKey != nil || it.CategoryID != nil || len(it.TagIDs) != 0 || it.AnalyzedAt != nil ||
		it.SchemaVersion != nil || it.Language != nil {
		t.Errorf("item %s carries a response: fiche %s, type %v, category %v, tags %v", it.ID, it.FicheJSON, it.TypeKey,
			it.CategoryID, it.TagIDs)
	}
}

// tagNames are the names of the tags of it.
func (e *resultsEnv) tagNames(t *testing.T, it store.Item) []string {
	t.Helper()
	byID := map[int64]string{}
	for name, id := range storetest.TagIDs(t, e.path, it.UserID) {
		byID[id] = name
	}
	names := make([]string, 0, len(it.TagIDs))
	for _, id := range it.TagIDs {
		names = append(names, byID[id])
	}
	slices.Sort(names)
	return names
}

func (e *resultsEnv) typeKeys(t *testing.T) []string {
	t.Helper()
	types, err := e.st.ContentTypes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, len(types))
	for i, ct := range types {
		keys[i] = ct.Key
	}
	return keys
}

// categoryNamed returns the non-deleted categories of userID named name.
func (e *resultsEnv) categoryNamed(t *testing.T, userID, name string) []store.Category {
	t.Helper()
	categories, err := e.st.Categories(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	var found []store.Category
	for _, c := range categories {
		if c.Name == name {
			found = append(found, c)
		}
	}
	return found
}

// kubernetes is a proposal of the category « Kubernetes » under parentID.
func kubernetes(parentID int64) map[string]any {
	return map[string]any{"proposed": map[string]any{
		"name": "Kubernetes", "parent_id": parentID, "description": "Orchestration de conteneurs.", "reason": "Aucune ne convient.",
	}}
}

// event is a proposal of the type event.
var event = map[string]any{"proposed": map[string]any{"key": "event", "name": "Évènement", "reason": "Un évènement daté."}}

func TestResultValidation(t *testing.T) {
	t.Run("A response without tool call is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		report := e.apply(t, b, succeeded(ids[0]))
		e.wantRejected(t, ids[0], 1)
		if report.Counts.Invalid != 1 || report.Items[0].Reason != "tool_calls" {
			t.Errorf("report %+v, want one invalid response for its tool calls", report.Counts)
		}
	})

	t.Run("Two tool calls are rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		in := tipInput(e.categoryID(t, e.userID, "Dev"))
		e.apply(t, b, succeeded(ids[0], in.call(t), in.call(t)))
		e.wantRejected(t, ids[0], 1)
	})

	t.Run("A call to another tool is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		call := tipInput(e.categoryID(t, e.userID, "Dev")).call(t)
		call.Name = "other_tool"
		e.apply(t, b, succeeded(ids[0], call))
		e.wantRejected(t, ids[0], 1)
	})

	t.Run("Another stop reason is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		in := tipInput(e.categoryID(t, e.userID, "Dev"))
		endTurn, refusal := valid(t, ids[0], in), valid(t, ids[1], in)
		endTurn.StopReason, refusal.StopReason = "end_turn", "refusal"
		report := e.apply(t, b, endTurn, refusal)
		e.wantRejected(t, ids[0], 1)
		e.wantRejected(t, ids[1], 1)
		if report.Items[0].Reason != "stop_reason" || report.Items[1].Reason != "stop_reason" {
			t.Errorf("reasons %+v, want stop_reason", report.Items)
		}
	})

	t.Run("An input that breaks the schema is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		report := e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).without("title")))
		e.wantRejected(t, ids[0], 1)
		if report.Items[0].Reason != "schema" {
			t.Errorf("reason %q, want schema", report.Items[0].Reason)
		}
	})

	t.Run("A template that does not match the type is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		report := e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Cuisine")).with("fiche", exampleFiche(t, "recipe"))))
		e.wantRejected(t, ids[0], 1)
		if report.Items[0].Reason != store.RejectTemplate {
			t.Errorf("reason %q, want template", report.Items[0].Reason)
		}
	})

	t.Run("A generic fiche for a type with its own template is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("fiche", exampleFiche(t, "generic"))))
		e.wantRejected(t, ids[0], 1)
	})

	t.Run("A proposed type with its own template is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("type", event)))
		e.wantRejected(t, ids[0], 1)
		if slices.Contains(e.typeKeys(t), "event") {
			t.Error("a type event exists")
		}
	})

	t.Run("An unknown type is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		report := e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("type", map[string]any{"key": "book"})))
		e.wantRejected(t, ids[0], 1)
		if report.Items[0].Reason != store.RejectUnknownType {
			t.Errorf("reason %q, want unknown_type", report.Items[0].Reason)
		}
	})

	t.Run("A category that is not the user's is rejected", func(t *testing.T) {
		e := newResultsEnv(t)
		other := e.user(t, "other")
		plantes := e.categoryID(t, e.userID, "Plantes")
		storetest.DeleteCategory(t, e.path, e.userID, plantes)
		b, ids := e.submitted(t, 3)
		in := tipInput(0)
		report := e.apply(t, b,
			valid(t, ids[0], in.with("category", map[string]any{"id": e.categoryID(t, other, "Dev")})),
			valid(t, ids[1], in.with("category", map[string]any{"id": plantes})),
			valid(t, ids[2], in.with("category", map[string]any{"id": 999999})))
		for i, id := range ids {
			e.wantRejected(t, id, 1)
			if report.Items[i].Reason != store.RejectUnknownCategory {
				t.Errorf("reason %q, want unknown_category", report.Items[i].Reason)
			}
		}
	})

	t.Run("A rejected response creates no proposal", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		broken := json.RawMessage(`{"template":"generic","summary":"Résumé"}`)
		in := tipInput(0).with("category", kubernetes(e.categoryID(t, e.userID, "Infra & DevOps"))).with("type", event).
			with("tags", []any{"alpha", "beta"}).with("fiche", broken)
		e.apply(t, b, valid(t, ids[0], in))
		e.wantRejected(t, ids[0], 1)
		if len(e.categoryNamed(t, e.userID, "Kubernetes")) != 0 || slices.Contains(e.typeKeys(t), "event") ||
			len(storetest.TagIDs(t, e.path, e.userID)) != 0 {
			t.Error("a rejected response created a category, a type or a tag")
		}
	})

	t.Run("The third rejected response fails the item", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[0], "submitted", nil, 2)
		report := e.apply(t, b, succeeded(ids[0]))
		it := e.item(t, e.userID, ids[0])
		if it.Status != "failed" || it.Attempts != 3 || it.LastError == nil || *it.LastError != store.LastErrorInvalidResponse {
			t.Errorf("item is %s with %d attempts, last_error %v; want failed, 3, invalid_response", it.Status, it.Attempts, it.LastError)
		}
		if content, err := e.st.ItemContent(context.Background(), e.userID, ids[0]); err != nil || content == nil {
			t.Errorf("extracted content = %v, %v; want it kept", content, err)
		}
		if report.Counts.Failed != 1 || report.Counts.Invalid != 1 {
			t.Errorf("counts %+v, want one invalid and failed", report.Counts)
		}
	})
}

func TestResultTags(t *testing.T) {
	analysed := func(t *testing.T, tags ...any) (*resultsEnv, store.Item) {
		t.Helper()
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("tags", tags)))
		return e, e.item(t, e.userID, ids[0])
	}
	wantTags := func(t *testing.T, e *resultsEnv, it store.Item, want ...string) {
		t.Helper()
		slices.Sort(want)
		if it.Status != "analyzed" || !slices.Equal(e.tagNames(t, it), want) {
			t.Errorf("item %s has the tags %v, want analyzed with %v", it.Status, e.tagNames(t, it), want)
		}
	}

	t.Run("Spaces and capitals are normalised", func(t *testing.T) {
		e, it := analysed(t, "Spring Boot", "Java")
		wantTags(t, e, it, "spring-boot", "java")
	})

	t.Run("Accents and underscores are normalised", func(t *testing.T) {
		e, it := analysed(t, "Électronique", "machine_learning")
		wantTags(t, e, it, "electronique", "machine-learning")
	})

	t.Run("A long tag is cut to 40 characters", func(t *testing.T) {
		long := strings.Repeat("a", 39) + "-bcdef" // 45 characters, the 40th a dash
		e, it := analysed(t, long, "go")
		wantTags(t, e, it, strings.Repeat("a", 39), "go")
	})

	t.Run("Duplicates after normalisation are dropped", func(t *testing.T) {
		e, it := analysed(t, "Go", "go", "golang")
		wantTags(t, e, it, "go", "golang")
	})

	t.Run("Fewer than two distinct tags make the response invalid", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		report := e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("tags", []any{"Go", "go"})))
		e.wantRejected(t, ids[0], 1)
		if len(storetest.TagIDs(t, e.path, e.userID)) != 0 || report.Items[0].Reason != "too_few_tags" {
			t.Errorf("tags %v, reason %q; want none created, too_few_tags", storetest.TagIDs(t, e.path, e.userID), report.Items[0].Reason)
		}
	})

	t.Run("A tag still outside the allowed form makes the response invalid", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		report := e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("tags", []any{"c++", "go"})))
		e.wantRejected(t, ids[0], 1)
		if len(storetest.TagIDs(t, e.path, e.userID)) != 0 || report.Items[0].Reason != "schema" {
			t.Errorf("tags %v, reason %q; want none created, schema", storetest.TagIDs(t, e.path, e.userID), report.Items[0].Reason)
		}
	})
}

func TestResultApplication(t *testing.T) {
	ctx := context.Background()

	t.Run("A valid response analyses the item", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		before := e.item(t, e.userID, ids[0])
		dev := e.categoryID(t, e.userID, "Dev")
		report := e.apply(t, b, valid(t, ids[0], tipInput(dev)))
		it := e.item(t, e.userID, ids[0])
		if it.Status != "analyzed" || it.TypeKey == nil || *it.TypeKey != "tip" || it.CategoryID == nil || *it.CategoryID != dev ||
			it.Title != "Un titre fictif" || string(it.FicheJSON) != string(tipFiche) || it.Language == nil || *it.Language != "en" ||
			it.Freshness == nil || *it.Freshness != "timeless" || it.ContentOK == nil || !*it.ContentOK || it.ContentIssue != nil {
			t.Errorf("item = %+v", it)
		}
		if got := e.tagNames(t, it); !slices.Equal(got, []string{"go", "testing"}) {
			t.Errorf("tags %v, want go and testing", got)
		}
		if it.SchemaVersion == nil || *it.SchemaVersion != 1 || it.AnalyzedAt == nil || !it.UpdatedAt.After(before.UpdatedAt) ||
			it.ChangeSeq <= before.ChangeSeq {
			t.Errorf("schema %v, analyzed_at %v, updated_at %s after %s, change_seq %d after %d", it.SchemaVersion, it.AnalyzedAt,
				it.UpdatedAt, before.UpdatedAt, it.ChangeSeq, before.ChangeSeq)
		}
		if report.Counts.Analyzed != 1 || report.Items[0].Outcome != "analyzed" {
			t.Errorf("counts %+v", report.Counts)
		}
	})

	t.Run("The versions come from the batch", func(t *testing.T) {
		e := newResultsEnv(t)
		ids := e.items(t, e.userID, 1)
		b := e.sent(t, e.userID, store.NewBatch{Model: "claude-haiku-4-5", PromptVersion: 3, EnvelopeVersion: 1}, ids...)
		cfg := e.cfg
		cfg.Model = "claude-another-model"
		e.ended(b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		if report := e.follow(t, cfg, e.userID, b); report.Outcome != "ended" {
			t.Fatalf("outcome %q", report.Outcome)
		}
		it := e.item(t, e.userID, ids[0])
		if it.Model == nil || *it.Model != "claude-haiku-4-5" || it.PromptVersion == nil || *it.PromptVersion != 3 ||
			it.EnvelopeVersion == nil || *it.EnvelopeVersion != 1 {
			t.Errorf("item recorded model %v, prompt %v, envelope %v", it.Model, it.PromptVersion, it.EnvelopeVersion)
		}
	})

	t.Run("A poor content is recorded as partial", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("content_ok", false).with("content_issue", "paywall")))
		it := e.item(t, e.userID, ids[0])
		if it.Status != "analyzed" || it.ContentOK == nil || *it.ContentOK || it.ContentIssue == nil || *it.ContentIssue != "paywall" {
			t.Errorf("item %s, content_ok %v, issue %v", it.Status, it.ContentOK, it.ContentIssue)
		}
	})

	t.Run("Failure counters are reset by a success", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[0], "submitted", nil, 2)
		storetest.SetLastError(t, e.path, e.userID, ids[0], store.LastErrorInvalidResponse)
		storetest.SetIncompleteRetried(t, e.path, e.userID, ids[0])
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		it := e.item(t, e.userID, ids[0])
		if it.Status != "analyzed" || it.Attempts != 0 || it.LastError != nil {
			t.Errorf("item %s with %d attempts, last_error %v", it.Status, it.Attempts, it.LastError)
		}
		if storetest.ItemAnalysisColumns(t, e.path, e.userID, ids[0]).IncompleteRetried {
			t.Error("the cut-retry mark is kept: the next request would carry the 8000 cap")
		}
	})

	t.Run("The search text is the flattened fiche", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		fiche := json.RawMessage(`{"template":"generic","summary":"Résumé A","sections":[{"heading":"Titre B","body":"Corps C",` +
			`"code":[{"language":"bash","code":"ls -la"}]}],"key_points":["Point D"]}`)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("type", event).with("fiche", fiche)))
		text := storetest.ItemAnalysisColumns(t, e.path, e.userID, ids[0]).SearchText
		if text == nil || *text != "Résumé A\nTitre B\nCorps C\nls -la\nPoint D" {
			t.Errorf("search text %q", ptrString(text))
		}
	})

	t.Run("A field corrected by hand is kept and proposes nothing", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetUserEdited(t, e.path, e.userID, ids[0], "category")
		before := e.item(t, e.userID, ids[0])
		e.apply(t, b, valid(t, ids[0], tipInput(0).with("category", kubernetes(e.categoryID(t, e.userID, "Infra & DevOps")))))
		it := e.item(t, e.userID, ids[0])
		if it.Status != "analyzed" || fmt.Sprint(it.CategoryID) != fmt.Sprint(before.CategoryID) {
			t.Errorf("item %s with category %v, want analyzed keeping %v", it.Status, it.CategoryID, before.CategoryID)
		}
		if len(e.categoryNamed(t, e.userID, "Kubernetes")) != 0 {
			t.Error("a category Kubernetes was created")
		}
		if it.TypeKey == nil || *it.TypeKey != "tip" || len(it.TagIDs) != 2 || it.Title != "Un titre fictif" || it.FicheJSON == nil {
			t.Errorf("type %v, tags %v, title %q, fiche %s; want them written", it.TypeKey, it.TagIDs, it.Title, it.FicheJSON)
		}
	})

	t.Run("Tags corrected by hand are kept", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.AttachTag(t, e.path, e.userID, ids[0], storetest.AddTag(t, e.path, e.userID, "perso"))
		storetest.SetUserEdited(t, e.path, e.userID, ids[0], "tags")
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("tags", []any{"alpha", "beta"})))
		it := e.item(t, e.userID, ids[0])
		tags := storetest.TagIDs(t, e.path, e.userID)
		if got := e.tagNames(t, it); it.Status != "analyzed" || !slices.Equal(got, []string{"perso"}) {
			t.Errorf("item %s with tags %v, want analyzed with perso", it.Status, got)
		}
		if _, ok := tags["alpha"]; ok || len(tags) != 1 {
			t.Errorf("tags of the user %v, want perso only", tags)
		}
	})

	t.Run("A first cut response is retried with a doubled cap", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[0], "submitted", nil, 1)
		cut := valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))
		cut.StopReason = "max_tokens"
		report := e.apply(t, b, cut)
		it := e.item(t, e.userID, ids[0])
		if it.Status != "extracted" || it.Attempts != 1 || it.BatchID != nil || it.LastError != nil {
			t.Errorf("item %s with %d attempts, batch %v, last_error %v", it.Status, it.Attempts, it.BatchID, it.LastError)
		}
		e.wantNothingWritten(t, it)
		if report.Counts.Cut != 1 {
			t.Errorf("counts %+v, want one cut", report.Counts)
		}
		e.run(t, e.cfg)
		bodies := e.ai.Bodies()
		if len(bodies) != 1 || len(bodies[0].Requests) != 1 || bodies[0].Requests[0].CustomID != ids[0] {
			t.Fatalf("the next pass sent %d bodies, want the item", len(bodies))
		}
		var params struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.Unmarshal(bodies[0].Requests[0].Params, &params); err != nil || params.MaxTokens != 8000 {
			t.Errorf("max_tokens = %d (%v), want 8000", params.MaxTokens, err)
		}
	})

	t.Run("A second cut response fails the item", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetIncompleteRetried(t, e.path, e.userID, ids[0])
		cut := valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))
		cut.StopReason = "max_tokens"
		report := e.apply(t, b, cut)
		it := e.item(t, e.userID, ids[0])
		if it.Status != "failed" || it.LastError == nil || *it.LastError != store.LastErrorTooLong || it.Attempts != 0 {
			t.Errorf("item %s with %d attempts, last_error %v; want failed, 0, too_long", it.Status, it.Attempts, it.LastError)
		}
		if content, err := e.st.ItemContent(ctx, e.userID, ids[0]); err != nil || content == nil {
			t.Errorf("extracted content = %v, %v; want it kept", content, err)
		}
		if report.Counts.TooLong != 1 || report.Counts.Failed != 1 {
			t.Errorf("counts %+v, want one too_long and failed", report.Counts)
		}
	})

	t.Run("An errored request counts an attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		report := e.apply(t, b, analysis.Result{CustomID: ids[0], Kind: analysis.ResultErrored, ErrorType: "invalid_request_error"})
		it := e.item(t, e.userID, ids[0])
		if it.Status != "extracted" || it.Attempts != 1 || it.LastError == nil || *it.LastError != store.LastErrorRequestErrored {
			t.Errorf("item %s with %d attempts, last_error %v", it.Status, it.Attempts, it.LastError)
		}
		if report.Items[0].ErrorType != "invalid_request_error" || report.Counts.Errored != 1 {
			t.Errorf("report %+v", report.Items)
		}
	})

	t.Run("The third errored request fails the item", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[0], "submitted", nil, 2)
		e.apply(t, b, analysis.Result{CustomID: ids[0], Kind: analysis.ResultErrored, ErrorType: "api_error"})
		it := e.item(t, e.userID, ids[0])
		if it.Status != "failed" || it.Attempts != 3 || it.LastError == nil || *it.LastError != store.LastErrorRequestErrored {
			t.Errorf("item %s with %d attempts, last_error %v", it.Status, it.Attempts, it.LastError)
		}
	})

	t.Run("An expired request goes back without attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.SetAnalysisState(t, e.path, e.userID, ids[0], "submitted", nil, 1)
		e.apply(t, b, analysis.Result{CustomID: ids[0], Kind: analysis.ResultExpired})
		it := e.item(t, e.userID, ids[0])
		if it.Status != "extracted" || it.BatchID != nil || it.Attempts != 1 {
			t.Errorf("item %s, batch %v, %d attempts", it.Status, it.BatchID, it.Attempts)
		}
		e.run(t, e.cfg)
		if got := e.sentIDs(); !slices.Equal(got, ids) {
			t.Errorf("the next pass sent %v, want %v", got, ids)
		}
	})

	t.Run("A canceled request goes back without attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, analysis.Result{CustomID: ids[0], Kind: analysis.ResultCanceled})
		it := e.item(t, e.userID, ids[0])
		if it.Status != "extracted" || it.BatchID != nil || it.Attempts != 0 {
			t.Errorf("item %s, batch %v, %d attempts", it.Status, it.BatchID, it.Attempts)
		}
	})

	t.Run("A batch sent with another envelope version goes back without attempt", func(t *testing.T) {
		e := newResultsEnv(t)
		if e.prompt.EnvelopeVersion != 1 {
			t.Fatalf("embedded envelope version %d, the scenario assumes 1", e.prompt.EnvelopeVersion)
		}
		ids := e.items(t, e.userID, 1)
		nb := e.newBatch()
		nb.EnvelopeVersion = 2
		b := e.sent(t, e.userID, nb, ids...)
		report := e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		it := e.item(t, e.userID, ids[0])
		if it.Status != "extracted" || it.BatchID != nil || it.Attempts != 0 || it.LastError != nil {
			t.Errorf("item %s, batch %v, %d attempts, last_error %v", it.Status, it.BatchID, it.Attempts, it.LastError)
		}
		e.wantNothingWritten(t, it)
		closing := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if closing.Status != "ended" || closing.Input == nil || *closing.Input != 100 || report.Counts.Requeued != 1 {
			t.Errorf("batch %+v, counts %+v; want ended with the 100 input tokens of the line", closing, report.Counts)
		}
	})

	t.Run("An item made private after its batch left gets its result", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		if _, err := e.st.SetPrivate(ctx, e.userID, ids[0], true); err != nil {
			t.Fatal(err)
		}
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev"))))
		it := e.item(t, e.userID, ids[0])
		if it.Status != "analyzed" || it.FicheJSON == nil || !it.IsPrivate {
			t.Errorf("item %s, fiche %s, private %v; want analyzed with its fiche, still private", it.Status, it.FicheJSON, it.IsPrivate)
		}
	})

	t.Run("A cut response of an item made private is not sent again", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		if _, err := e.st.SetPrivate(ctx, e.userID, ids[0], true); err != nil {
			t.Fatal(err)
		}
		cut := valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")))
		cut.StopReason = "max_tokens"
		e.apply(t, b, cut)
		it := e.item(t, e.userID, ids[0])
		if it.Status != "extracted" || !it.IsPrivate {
			t.Errorf("item %s, private %v; want extracted, private", it.Status, it.IsPrivate)
		}
		other := e.items(t, e.userID, 1)
		e.run(t, e.cfg)
		if got := e.sentIDs(); slices.Contains(got, ids[0]) || !slices.Equal(got, other) {
			t.Errorf("the next pass sent %v, want %v only", got, other)
		}
	})

	t.Run("A deleted item's result is dropped", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		storetest.DeleteItem(t, e.path, e.userID, ids[0])
		in := tipInput(0).with("category", kubernetes(e.categoryID(t, e.userID, "Infra & DevOps"))).with("tags", []any{"alpha", "go"})
		report := e.apply(t, b, valid(t, ids[0], in))
		if _, err := e.st.Item(ctx, e.userID, ids[0]); err == nil {
			t.Error("the deleted item is read back")
		}
		if len(e.categoryNamed(t, e.userID, "Kubernetes")) != 0 || len(storetest.TagIDs(t, e.path, e.userID)) != 0 {
			t.Error("the result of a deleted item created a category or a tag")
		}
		closing := storetest.BatchClosingOf(t, e.path, e.userID, b.ID)
		if closing.Input == nil || *closing.Input != 100 || report.Counts.Deleted != 1 {
			t.Errorf("batch input %v, counts %+v; want the 100 tokens of the line, one deleted", closing.Input, report.Counts)
		}
	})

	t.Run("Two items of a batch proposing the same category get one", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		in := tipInput(0).with("category", kubernetes(e.categoryID(t, e.userID, "Infra & DevOps")))
		e.apply(t, b, valid(t, ids[0], in), valid(t, ids[1], in))
		created := e.categoryNamed(t, e.userID, "Kubernetes")
		if len(created) != 1 || created[0].Status != "proposed" {
			t.Fatalf("categories Kubernetes %+v, want one proposed", created)
		}
		for _, id := range ids {
			if it := e.item(t, e.userID, id); it.CategoryID == nil || *it.CategoryID != created[0].ID {
				t.Errorf("item %s in %v, want %d", id, it.CategoryID, created[0].ID)
			}
		}
	})

	t.Run("Two items of a batch proposing the same type get one", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		in := tipInput(e.categoryID(t, e.userID, "Dev")).with("type", event).with("fiche", exampleFiche(t, "generic"))
		e.apply(t, b, valid(t, ids[0], in), valid(t, ids[1], in))
		keys := e.typeKeys(t)
		if n := len(slices.DeleteFunc(slices.Clone(keys), func(k string) bool { return k != "event" })); n != 1 {
			t.Errorf("types %v, want one event", keys)
		}
		for _, id := range ids {
			if it := e.item(t, e.userID, id); it.TypeKey == nil || *it.TypeKey != "event" {
				t.Errorf("item %s of type %v, want event", id, it.TypeKey)
			}
		}
	})

	t.Run("A new tag of two items of a batch is created once", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 2)
		in := tipInput(e.categoryID(t, e.userID, "Dev")).with("tags", []any{"kubernetes", "go"})
		e.apply(t, b, valid(t, ids[0], in), valid(t, ids[1], in))
		tags := storetest.TagIDs(t, e.path, e.userID)
		id, ok := tags["kubernetes"]
		if !ok || len(tags) != 2 {
			t.Fatalf("tags %v, want one kubernetes", tags)
		}
		for _, item := range ids {
			if it := e.item(t, e.userID, item); !slices.Contains(it.TagIDs, id) {
				t.Errorf("item %s carries %v, want %d", item, it.TagIDs, id)
			}
		}
	})

	t.Run("A proposed type is offered to every user", func(t *testing.T) {
		e := newResultsEnv(t)
		second := e.user(t, "second")
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(e.categoryID(t, e.userID, "Dev")).with("type", event).with("fiche", exampleFiche(t, "generic"))))
		if got := refBlock(t, e.referentielOf(t, second), "types"); indexOf(got, "event | Évènement | proposed") < 0 {
			t.Errorf("the second user's <types> = %q, want the line of event", got)
		}
	})

	t.Run("A proposed category is offered at the next batch", func(t *testing.T) {
		e := newResultsEnv(t)
		b, ids := e.submitted(t, 1)
		e.apply(t, b, valid(t, ids[0], tipInput(0).with("category", kubernetes(e.categoryID(t, e.userID, "Infra & DevOps")))))
		created := e.categoryNamed(t, e.userID, "Kubernetes")
		if len(created) != 1 {
			t.Fatalf("categories Kubernetes %+v, want one", created)
		}
		want := fmt.Sprintf("%d | Infra & DevOps › Kubernetes | proposed | Orchestration de conteneurs.", created[0].ID)
		if got := refBlock(t, e.referentielOf(t, e.userID), "categories"); indexOf(got, want) < 0 {
			t.Errorf("<categories> lacks %q:\n%s", want, strings.Join(got, "\n"))
		}
	})
}

func ptrString(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestPassPrices(t *testing.T) {
	t.Run("a pass refuses a price that is not above zero", func(t *testing.T) {
		e := newResultsEnv(t)
		for _, prices := range []analysis.Prices{{}, {InputPerMTok: 1}, {OutputPerMTok: 5}, {InputPerMTok: -1, OutputPerMTok: 5}} {
			cfg := e.cfg
			cfg.Prices = prices
			if _, err := analysis.NewPass(cfg); err == nil {
				t.Errorf("NewPass accepted the prices %+v", prices)
			}
		}
	})
}
