package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// The tests of this file follow openspec/changes/apply-batch-results/specs/item-editing and
// item-capture: an answered Item carries the classification and the fiche of the item. The
// analysed items are written by the store functions of the pipeline (SubmitBatch, then
// WriteAnalysis or SettleItem), as a batch's results are.

// tipFiche is a valid tip fiche.
const tipFiche = `{"template":"tip","tip":"Une astuce fictive.","steps":["Faire ceci."],"materials":[],"warnings":[]}`

// submit extracts the item id of the owner and sends it in a batch; it returns the batch id.
func (f fixture) submit(t *testing.T, id string) string {
	t.Helper()
	ctx := context.Background()
	if item := f.storedItem(t, f.userID, id); item.Status == "queued" {
		if written, err := f.store.WriteExtraction(ctx, f.userID, id, store.Extraction{Mode: "fallback"}); err != nil || !written {
			t.Fatalf("WriteExtraction = %v, %v", written, err)
		}
	}
	batchID, moved, err := f.store.SubmitBatch(ctx, f.userID,
		store.NewBatch{Model: "claude-haiku-4-5", PromptVersion: 3, EnvelopeVersion: 1}, []string{id})
	if err != nil || len(moved) != 1 {
		t.Fatalf("SubmitBatch = %v, %v", moved, err)
	}
	return batchID
}

// analyse writes a valid response of typeKey, with fiche and tags, for the item id of the owner,
// filed under the category Dev, and returns the id of that category.
func (f fixture) analyse(t *testing.T, id, typeKey, fiche string, tags ...string) int64 {
	t.Helper()
	ctx := context.Background()
	batchID := f.submit(t, id)
	dev := f.categoryID(t, "Dev")
	var head struct {
		Template string `json:"template"`
	}
	if err := json.Unmarshal([]byte(fiche), &head); err != nil {
		t.Fatal(err)
	}
	written, err := f.store.WriteAnalysis(ctx, f.userID, batchID, id, store.Analysis{
		Type: store.TypeChoice{Key: typeKey}, Template: head.Template, Category: store.CategoryChoice{ID: dev},
		Tags: tags, Title: "Un titre fictif", FicheJSON: json.RawMessage(fiche), SearchText: "Une astuce fictive.",
		Language: "en", Freshness: "timeless", ContentOK: true, SchemaVersion: 1,
	})
	if err != nil || written.Settlement != store.SettledAnalyzed {
		t.Fatalf("WriteAnalysis = %+v, %v; want the item analysed", written, err)
	}
	return dev
}

func (f fixture) categoryID(t *testing.T, name string) int64 {
	t.Helper()
	categories, err := f.store.Categories(context.Background(), f.userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range categories {
		if c.Name == name {
			return c.ID
		}
	}
	t.Fatalf("no category %s", name)
	return 0
}

// tagIDs are the ids of the owner's tags names, in that order.
func (f fixture) tagIDs(t *testing.T, names ...string) []any {
	t.Helper()
	byName := storetest.TagIDs(t, f.dbPath, f.userID)
	ids := make([]any, len(names))
	for i, name := range names {
		id, ok := byName[name]
		if !ok {
			t.Fatalf("no tag %s", name)
		}
		ids[i] = float64(id)
	}
	return ids
}

// wantFiche checks that the fiche member of item is the fiche want.
func wantFiche(t *testing.T, item map[string]any, want string) {
	t.Helper()
	var fiche any
	if err := json.Unmarshal([]byte(want), &fiche); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(item["fiche"], fiche) {
		t.Errorf("fiche = %v, want %v", item["fiche"], fiche)
	}
}

// wantItemSchema checks that raw is an Item of the contract.
func wantItemSchema(t *testing.T, raw []byte) {
	t.Helper()
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse body: %v", err)
	}
	if err := itemSchema(t).Validate(instance); err != nil {
		t.Errorf("body does not match the Item schema: %v\n%s", err, raw)
	}
}

func TestItemClassification(t *testing.T) {
	t.Run("An answered item carries its classification and its fiche", func(t *testing.T) {
		t.Run("An analysed item is answered with its classification", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			// Created in this order, testing first: the answer sorts the ids, not the response.
			dev := f.analyse(t, id, "tip", tipFiche, "testing", "go")
			raw, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"content_type": "tip", "category_id": float64(dev), "status": "analyzed"})
			ids := f.tagIDs(t, "go", "testing")
			slices.SortFunc(ids, func(a, b any) int { return int(a.(float64) - b.(float64)) })
			if got := item["tag_ids"]; !reflect.DeepEqual(got, ids) {
				t.Errorf("tag_ids = %v, want %v in ascending order", got, ids)
			}
			wantFiche(t, item, tipFiche)
			if fiche, _ := item["fiche"].(map[string]any); fiche["template"] != "tip" {
				t.Errorf("fiche template = %v, want tip", fiche["template"])
			}
			wantItemSchema(t, raw)
		})

		t.Run("An item not analysed yet has no classification", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			if written, err := f.store.WriteExtraction(context.Background(), f.userID, id, store.Extraction{Mode: "fallback"}); err != nil || !written {
				t.Fatalf("WriteExtraction = %v, %v", written, err)
			}
			raw, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"status": "extracted", "content_type": nil, "category_id": nil})
			if ids, ok := item["tag_ids"].([]any); !ok || len(ids) != 0 {
				t.Errorf("tag_ids = %#v, want []", item["tag_ids"])
			}
			if fiche, present := item["fiche"]; present {
				t.Errorf("fiche = %v, want no fiche member", fiche)
			}
			wantItemSchema(t, raw)
		})

		t.Run("A deleted tag is not answered", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			f.analyse(t, id, "tip", tipFiche, "go", "golang")
			storetest.DeleteTag(t, f.dbPath, f.userID, storetest.TagIDs(t, f.dbPath, f.userID)["golang"])
			_, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			if got, want := item["tag_ids"], f.tagIDs(t, "go"); !reflect.DeepEqual(got, want) {
				t.Errorf("tag_ids = %v, want only the id of go %v", got, want)
			}
		})

		t.Run("A failed item answers its error code", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			ctx := context.Background()
			for range 2 { // a first cut response retried, then a second one
				if _, err := f.store.SettleItem(ctx, f.userID, f.submit(t, id), id, store.OutcomeCut); err != nil {
					t.Fatalf("SettleItem: %v", err)
				}
			}
			raw, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"status": "failed", "last_error": "too_long"})
			wantItemSchema(t, raw)
		})

		t.Run("an item failed because its response could not be stored answers storage_failed", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			for range 3 { // three nights whose valid response the store refused
				if _, err := f.store.SettleItem(context.Background(), f.userID, f.submit(t, id), id, store.OutcomeStorageFailed); err != nil {
					t.Fatalf("SettleItem: %v", err)
				}
			}
			raw, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"status": "failed", "last_error": "storage_failed", "attempts": float64(3)})
			wantItemSchema(t, raw)
		})

		t.Run("a stored fiche that does not decode answers 500 without quoting it", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			f.analyse(t, id, "tip", tipFiche, "go", "testing")
			const marker = "CORRUPT-FICHE-MARKER"
			storetest.SetFicheJSON(t, f.dbPath, f.userID, id, `{"template":"tip","tip":["`+marker+`"]}`)
			resp := f.patchItem(t, testToken, id, `{"is_private": true}`)
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusInternalServerError || resp.Header.Get("Content-Type") != "application/problem+json" {
				t.Errorf("status %d, %s; want 500 problem+json", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			if strings.Contains(string(body), marker) {
				t.Errorf("the answer quotes the fiche: %s", body)
			}
			if logs := f.logs.String(); strings.Contains(logs, marker) || !strings.Contains(logs, "stored item does not match the contract") {
				t.Errorf("logs quote the fiche, or lack the cause:\n%s", logs)
			}
		})
	})
}

func TestDuplicateOfAnalysedLink(t *testing.T) {
	t.Run("A duplicate of an analysed link answers its classification", func(t *testing.T) {
		t.Run("Duplicate of an analysed link", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			recipe := recipeFiche(t)
			dev := f.analyse(t, id, "recipe", recipe, "pates", "rapide")
			before := f.storedItem(t, f.userID, id)
			f.clock.Advance(time.Hour)
			raw, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"id": id, "content_type": "recipe", "category_id": float64(dev)})
			ids := f.tagIDs(t, "pates", "rapide")
			if got := item["tag_ids"]; !reflect.DeepEqual(got, ids) {
				t.Errorf("tag_ids = %v, want %v", got, ids)
			}
			wantFiche(t, item, recipe)
			wantItemSchema(t, raw)
			after := f.storedItem(t, f.userID, id)
			if !after.UpdatedAt.Equal(before.UpdatedAt) || after.ChangeSeq != before.ChangeSeq {
				t.Errorf("item updated_at %v, change_seq %d; want %v, %d unchanged", after.UpdatedAt, after.ChangeSeq, before.UpdatedAt, before.ChangeSeq)
			}
		})
	})
}

// recipeFiche is the fiche of the recipe example (docs/prompt/schemas/examples), compacted.
func recipeFiche(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ficheExamples, "recipe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var example struct {
		Fiche json.RawMessage `json:"fiche"`
	}
	if err := json.Unmarshal(raw, &example); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, example.Fiche); err != nil {
		t.Fatal(err)
	}
	return compact.String()
}
