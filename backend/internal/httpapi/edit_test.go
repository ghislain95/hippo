package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// unknownItemID matches no item of the fixture.
const unknownItemID = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8Z"

// patchItem sends PATCH /api/items/{id} with a JSON body.
func (f fixture) patchItem(t *testing.T, token, id, body string) *http.Response {
	t.Helper()
	return f.do(t, http.MethodPatch, "/api/items/"+id, token, "application/json", body)
}

// newLink captures a link for the owner of token and returns the id of the new item.
func (f fixture) newLink(t *testing.T, token string, private bool) string {
	t.Helper()
	body := `{"url": "https://example.com/a"}`
	if private {
		body = `{"url": "https://example.com/a", "is_private": true}`
	}
	_, item := capturedItem(t, f.capture(t, token, body), http.StatusCreated)
	id, _ := item["id"].(string)
	return id
}

// storedItem reads an item back through the store: change_seq is not part of the Item answer.
func (f fixture) storedItem(t *testing.T, userID, id string) store.Item {
	t.Helper()
	item, err := f.store.Item(context.Background(), userID, id)
	if err != nil {
		t.Fatalf("read item %s: %v", id, err)
	}
	return item
}

// analysedAt is the analysis date of the items analysed by SQL in these tests.
var analysedAt = time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC)

// TestItemEditing follows openspec/changes/mark-private/specs/item-editing/spec.md: one subtest
// per scenario, named after it, grouped by requirement. Statuses no code writes yet (analyzed,
// failed) are set by SQL on the test database (storetest.SetAnalysisState).
func TestItemEditing(t *testing.T) {
	t.Run("The privacy flag of an item can be changed", func(t *testing.T) {
		t.Run("An item is made private", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			f.clock.Advance(time.Minute)
			raw, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"id": id, "is_private": true, "status": "queued"})
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse body: %v", err)
			}
			if err := itemSchema(t).Validate(instance); err != nil {
				t.Errorf("200 body does not match the Item schema: %v\n%s", err, raw)
			}
			after := f.storedItem(t, f.userID, id)
			if !after.IsPrivate || !after.UpdatedAt.Equal(f.clock.Now()) || !after.UpdatedAt.After(before.UpdatedAt) {
				t.Errorf("stored item private %v, updated_at %v; want private, updated at %v", after.IsPrivate, after.UpdatedAt, f.clock.Now())
			}
			if after.ChangeSeq <= before.ChangeSeq {
				t.Errorf("change_seq = %d, want above %d", after.ChangeSeq, before.ChangeSeq)
			}
		})

		t.Run("A private item is made public", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, true)
			_, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": false}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"id": id, "is_private": false})
			if f.storedItem(t, f.userID, id).IsPrivate {
				t.Error("stored item is private, want public")
			}
		})

		t.Run("The same value writes nothing", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, true)
			before := f.storedItem(t, f.userID, id)
			f.clock.Advance(time.Minute)
			_, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"is_private": true})
			after := f.storedItem(t, f.userID, id)
			if !after.UpdatedAt.Equal(before.UpdatedAt) || after.ChangeSeq != before.ChangeSeq {
				t.Errorf("updated_at, change_seq = %v, %d; want %v, %d unchanged", after.UpdatedAt, after.ChangeSeq, before.UpdatedAt, before.ChangeSeq)
			}
		})
	})

	t.Run("Changing the privacy flag never changes the processing status", func(t *testing.T) {
		t.Run("A private extracted item made public is back in the analysis queue", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, true)
			written, err := f.store.WriteExtraction(context.Background(), f.userID, id, store.Extraction{Mode: "fallback"})
			if err != nil || !written {
				t.Fatalf("WriteExtraction = %v, %v; want the item extracted", written, err)
			}
			_, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": false}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"status": "extracted", "is_private": false})
			if stored := f.storedItem(t, f.userID, id); stored.Status != "extracted" || stored.IsPrivate {
				t.Errorf("stored item %s, private %v; want extracted and public", stored.Status, stored.IsPrivate)
			}
		})

		t.Run("An analysed item made private keeps its status", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			storetest.SetAnalysisState(t, f.dbPath, f.userID, id, "analyzed", &analysedAt, 1)
			_, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"is_private": true, "status": "analyzed", "analyzed_at": "2026-10-28T02:00:00Z"})
			stored := f.storedItem(t, f.userID, id)
			if !stored.IsPrivate || stored.Status != "analyzed" || stored.AnalyzedAt == nil || !stored.AnalyzedAt.Equal(analysedAt) {
				t.Errorf("stored item private %v, %s, analyzed_at %v; want private, analyzed at %v", stored.IsPrivate, stored.Status, stored.AnalyzedAt, analysedAt)
			}
		})

		t.Run("An analysed item made public is not reanalysed", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, true)
			storetest.SetAnalysisState(t, f.dbPath, f.userID, id, "analyzed", &analysedAt, 1)
			_, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": false}`), http.StatusOK)
			assertMembers(t, item, map[string]any{
				"is_private": false, "status": "analyzed", "analyzed_at": "2026-10-28T02:00:00Z", "attempts": float64(1),
			})
			stored := f.storedItem(t, f.userID, id)
			if stored.IsPrivate || stored.Status != "analyzed" || stored.AnalyzedAt == nil || !stored.AnalyzedAt.Equal(analysedAt) || stored.Attempts != 1 {
				t.Errorf("stored item private %v, %s, analyzed_at %v, attempts %d; want public, analyzed at %v, 1 attempt",
					stored.IsPrivate, stored.Status, stored.AnalyzedAt, stored.Attempts, analysedAt)
			}
		})

		t.Run("A failed item made public stays failed", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, true)
			storetest.SetAnalysisState(t, f.dbPath, f.userID, id, "failed", nil, 3)
			_, item := capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": false}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"is_private": false, "status": "failed", "attempts": float64(3)})
			if stored := f.storedItem(t, f.userID, id); stored.IsPrivate || stored.Status != "failed" {
				t.Errorf("stored item private %v, %s; want public and failed", stored.IsPrivate, stored.Status)
			}
		})
	})

	t.Run("Only the owner can change an item", func(t *testing.T) {
		t.Run("Another user's item", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, otherToken, false)
			before := f.storedItem(t, f.otherID, id)
			assertProblemType(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusNotFound, blankType)
			after := f.storedItem(t, f.otherID, id)
			if after.IsPrivate || after.ChangeSeq != before.ChangeSeq {
				t.Errorf("other user's item private %v, change_seq %d; want public, %d", after.IsPrivate, after.ChangeSeq, before.ChangeSeq)
			}
		})

		t.Run("Unknown item", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.patchItem(t, testToken, unknownItemID, `{"is_private": true}`), http.StatusNotFound, blankType)
		})
	})

	t.Run("The edit body follows the contract strictly", func(t *testing.T) {
		t.Run("Undeclared member", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			assertProblemType(t, f.patchItem(t, testToken, id, `{"is_private": true, "favorite": true}`), http.StatusBadRequest, blankType)
			f.assertUnchanged(t, id, before)
		})

		t.Run("Empty object", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			assertProblemType(t, f.patchItem(t, testToken, id, `{}`), http.StatusBadRequest, blankType)
		})

		t.Run("Wrong type or null", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			for _, body := range []string{`{"is_private": "yes"}`, `{"is_private": null}`} {
				assertProblemType(t, f.patchItem(t, testToken, id, body), http.StatusBadRequest, blankType)
			}
			f.assertUnchanged(t, id, before)
		})

		// The members the contract does not declare nullable: encoding/json alone would take
		// their null for an absent member and apply is_private.
		t.Run("null for a member that is not nullable", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			for _, body := range []string{
				`{"is_private": true, "is_pinned": null}`,
				`{"is_private": true, "tags": null}`,
				`{"is_private": true, "content_type": null}`,
				`{"is_private": true, "content_type": null, "category_id": null}`,
			} {
				resp := f.patchItem(t, testToken, id, body)
				if resp.StatusCode != http.StatusBadRequest {
					t.Errorf("%s: status = %d, want 400", body, resp.StatusCode)
				}
			}
			f.assertUnchanged(t, id, before)
		})

		// The generated decoder reads this body as JSON whatever its Content-Type: the strict
		// decoding must not depend on the header.
		t.Run("an undeclared member without the JSON content type", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			for _, contentType := range []string{"", "text/plain"} {
				resp := f.do(t, http.MethodPatch, "/api/items/"+id, testToken, contentType, `{"is_private": true, "favorite": true}`)
				assertProblemType(t, resp, http.StatusBadRequest, blankType)
			}
			f.assertUnchanged(t, id, before)
		})

		t.Run("data after the JSON value", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			assertProblemType(t, f.patchItem(t, testToken, id, `{"is_private": true} {"is_private": false}`), http.StatusBadRequest, blankType)
			f.assertUnchanged(t, id, before)
		})
	})

	t.Run("Members other than the privacy flag answer 501 until implemented", func(t *testing.T) {
		t.Run("Pin not implemented yet", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			assertProblemType(t, f.patchItem(t, testToken, id, `{"is_pinned": true}`), http.StatusNotImplemented, notImplementedType)
		})

		t.Run("A mixed request applies nothing", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			f.clock.Advance(time.Minute)
			assertProblemType(t, f.patchItem(t, testToken, id, `{"is_private": true, "tags": ["travel"]}`), http.StatusNotImplemented, notImplementedType)
			f.assertUnchanged(t, id, before)
		})

		t.Run("every other member answers 501, null included", func(t *testing.T) {
			f := newFixture(t)
			id := f.newLink(t, testToken, false)
			before := f.storedItem(t, f.userID, id)
			for _, body := range []string{
				`{"is_private": true, "content_type": "recipe"}`,
				`{"is_private": true, "category_id": 1}`,
				`{"is_private": true, "category_id": null}`,
				`{"is_private": true, "tags": []}`,
				`{"is_private": true, "is_pinned": false}`,
				`{"is_private": true, "annotations": "x"}`,
				`{"is_private": true, "annotations": null}`,
				`{"is_private": true, "user_note": "x"}`,
				`{"is_private": true, "user_note": null}`,
			} {
				resp := f.patchItem(t, testToken, id, body)
				if resp.StatusCode != http.StatusNotImplemented {
					t.Errorf("%s: status = %d, want 501", body, resp.StatusCode)
				}
			}
			f.assertUnchanged(t, id, before)
		})
	})

	t.Run("logs neither the URL nor the note of the item", func(t *testing.T) {
		f := newFixture(t)
		_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://secret.example.com/a", "user_note": "private thought"}`), http.StatusCreated)
		id, _ := item["id"].(string)
		capturedItem(t, f.patchItem(t, testToken, id, `{"is_private": true}`), http.StatusOK)
		logs := f.logs.String()
		for _, leak := range []string{"secret.example.com", "private thought"} {
			if strings.Contains(logs, leak) {
				t.Errorf("logs contain %q:\n%s", leak, logs)
			}
		}
		if !strings.Contains(logs, `"route":"PATCH /api/items/{item_id}"`) {
			t.Errorf("logs have no request line of the edit:\n%s", logs)
		}
	})
}

// assertUnchanged checks that a refused request left the owner's item as it was.
func (f fixture) assertUnchanged(t *testing.T, id string, before store.Item) {
	t.Helper()
	after := f.storedItem(t, f.userID, id)
	if after.IsPrivate != before.IsPrivate || !after.UpdatedAt.Equal(before.UpdatedAt) || after.ChangeSeq != before.ChangeSeq {
		t.Errorf("item private %v, updated_at %v, change_seq %d; want %v, %v, %d unchanged",
			after.IsPrivate, after.UpdatedAt, after.ChangeSeq, before.IsPrivate, before.UpdatedAt, before.ChangeSeq)
	}
}
