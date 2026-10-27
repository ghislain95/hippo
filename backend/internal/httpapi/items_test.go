package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

const (
	testClientID    = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E"
	otherClientID   = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8F"
	privateClientID = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8G"
	noteClientID    = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8H"
	blankType       = "about:blank"
)

var ulidPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// capture sends POST /api/items with a JSON body.
func (f fixture) capture(t *testing.T, token, body string) *http.Response {
	t.Helper()
	return f.do(t, http.MethodPost, "/api/items", token, "application/json", body)
}

func (f fixture) itemCount(t *testing.T, userID string) int {
	t.Helper()
	return storetest.ItemCount(t, f.dbPath, userID)
}

// capturedItem checks the status of an item answer and returns its raw body and its members.
func capturedItem(t *testing.T, resp *http.Response, status int) ([]byte, map[string]any) {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d; body %s", resp.StatusCode, status, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var item map[string]any
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	return raw, item
}

func assertProblemType(t *testing.T, resp *http.Response, status int, problemType string) {
	t.Helper()
	if problem := assertProblem(t, resp, status); problem.Type != problemType {
		t.Errorf("type = %q, want %s", problem.Type, problemType)
	}
}

// assertMembers checks members of an item; a nil want means a JSON null.
func assertMembers(t *testing.T, item map[string]any, want map[string]any) {
	t.Helper()
	for name, value := range want {
		got, present := item[name]
		if !present {
			t.Errorf("%s is missing", name)
			continue
		}
		if got != value {
			t.Errorf("%s = %#v, want %#v", name, got, value)
		}
	}
}

func assertEmptyArray(t *testing.T, item map[string]any, name string) {
	t.Helper()
	if values, ok := item[name].([]any); !ok || len(values) != 0 {
		t.Errorf("%s = %#v, want an empty array", name, item[name])
	}
}

// itemSchema compiles the Item schema of the bundled contract.
func itemSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, err := os.ReadFile(bundledContract)
	if err != nil {
		t.Fatalf("read bundled contract: %v", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse bundled contract: %v", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource("openapi.json", doc); err != nil {
		t.Fatalf("load bundled contract: %v", err)
	}
	schema, err := compiler.Compile("openapi.json#/components/schemas/Item")
	if err != nil {
		t.Fatalf("compile Item schema: %v", err)
	}
	return schema
}

// TestItemCapture follows openspec/specs/item-capture/spec.md and its delta in
// openspec/changes/capture-note/specs/item-capture/spec.md: one subtest per scenario, named
// after it, grouped by requirement.
func TestItemCapture(t *testing.T) {
	t.Run("A link capture creates a queued item", func(t *testing.T) {
		t.Run("Capture a link", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/article"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{
				"kind": "link", "status": "queued", "read_status": "to_read", "url": "https://example.com/article",
				"content_type": nil, "category_id": nil, "extracted_at": nil, "analyzed_at": nil,
			})
			assertEmptyArray(t, item, "tag_ids")
			if _, present := item["fiche"]; present {
				t.Errorf("fiche = %#v, want no fiche", item["fiche"])
			}
		})

		t.Run("Each capture gets its own identifier", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			_, second := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/b"}`), http.StatusCreated)
			firstID, _ := first["id"].(string)
			secondID, _ := second["id"].(string)
			if !ulidPattern.MatchString(firstID) || !ulidPattern.MatchString(secondID) || firstID == secondID {
				t.Errorf("ids %q and %q, want two different ULIDs", firstID, secondID)
			}
		})
	})

	t.Run("The provisional title of a link is its host name", func(t *testing.T) {
		t.Run("Title of a freshly captured link", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://Blog.Example.com:8443/posts/1?x=1"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": "blog.example.com"})
		})
	})

	t.Run("The capture note, privacy and source are recorded", func(t *testing.T) {
		t.Run("Capture with a note, private, from the app", func(t *testing.T) {
			f := newFixture(t)
			resp := f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "for the trip", "is_private": true, "source": "manual"}`)
			_, item := capturedItem(t, resp, http.StatusCreated)
			assertMembers(t, item, map[string]any{"user_note": "for the trip", "is_private": true, "source": "manual"})
		})

		t.Run("Defaults of a bare capture", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"user_note": nil, "is_private": false, "source": "share"})
		})

		t.Run("Unknown source is refused", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `{"url": "https://example.com/a", "source": "rss"}`), http.StatusBadRequest, blankType)
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})
	})

	t.Run("Surrounding whitespace is removed from a capture", func(t *testing.T) {
		t.Run("Link shared with a trailing line break", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "  https://example.com/a\n"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"url": "https://example.com/a"})
		})

		t.Run("Blank capture note", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "  \n "}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"user_note": nil})
		})
	})

	t.Run("A capture must be a valid link or a note", func(t *testing.T) {
		const invalidURL = "urn:hippo:problem:invalid-url"

		t.Run("Empty capture", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `{}`), http.StatusBadRequest, "urn:hippo:problem:empty-capture")
		})

		t.Run("JSON null body", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `null`), http.StatusBadRequest, "urn:hippo:problem:empty-capture")
		})

		t.Run("Blank URL alone", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `{"url": "   "}`), http.StatusBadRequest, invalidURL)
		})

		t.Run("URL with another scheme", func(t *testing.T) {
			f := newFixture(t)
			for _, body := range []string{
				`{"url": "ftp://example.com/file"}`, `{"url": "javascript:alert(1)"}`,
				`{"url": "data:text/html,hi"}`, `{"url": "mailto:someone@example.com"}`,
			} {
				t.Log(body)
				assertProblemType(t, f.capture(t, testToken, body), http.StatusBadRequest, invalidURL)
			}
		})

		t.Run("URL without a host", func(t *testing.T) {
			f := newFixture(t)
			for _, body := range []string{
				`{"url": "example.com/article"}`, `{"url": "https://"}`,
				`{"url": "https:example.com"}`, `{"url": "https://:443/a"}`,
			} {
				t.Log(body)
				assertProblemType(t, f.capture(t, testToken, body), http.StatusBadRequest, invalidURL)
			}
		})

		t.Run("Upper-case scheme is accepted as sent", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "HTTPS://Example.com/A"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"url": "HTTPS://Example.com/A", "title": "example.com"})
		})

		t.Run("A refused capture stores nothing", func(t *testing.T) {
			f := newFixture(t)
			resp := f.capture(t, testToken, `{"url": "ftp://example.com/file", "client_id": "`+testClientID+`"}`)
			assertProblemType(t, resp, http.StatusBadRequest, invalidURL)
			capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "client_id": "`+testClientID+`"}`), http.StatusCreated)
		})
	})

	t.Run("An ambiguous capture is refused", func(t *testing.T) {
		const ambiguous = "urn:hippo:problem:ambiguous-capture"

		t.Run("URL and note together", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `{"url": "https://example.com/a", "note": "buy milk"}`), http.StatusBadRequest, ambiguous)
		})

		t.Run("Note and capture note together", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `{"note": "buy milk", "user_note": "today"}`), http.StatusBadRequest, ambiguous)
		})

		t.Run("Ambiguity wins over an invalid URL", func(t *testing.T) {
			f := newFixture(t)
			for _, body := range []string{`{"url": "not a url", "note": "buy milk"}`, `{"url": "", "note": "buy milk"}`} {
				t.Log(body)
				assertProblemType(t, f.capture(t, testToken, body), http.StatusBadRequest, ambiguous)
			}
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})

		t.Run("A blank capture note does not make a note ambiguous", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"note": "buy milk", "user_note": "   "}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"kind": "note", "user_note": "buy milk"})
		})

		t.Run("Ambiguity wins over an empty note", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `{"note": "   ", "user_note": "today"}`), http.StatusBadRequest, ambiguous)
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})
	})

	t.Run("A replayed capture returns the existing item", func(t *testing.T) {
		t.Run("Replay of the same capture", func(t *testing.T) {
			f := newFixture(t)
			body := `{"url": "https://example.com/a", "client_id": "` + testClientID + `"}`
			first, _ := capturedItem(t, f.capture(t, testToken, body), http.StatusCreated)
			replay, _ := capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			if !bytes.Equal(replay, first) {
				t.Errorf("replay = %s\nwant the unchanged item %s", replay, first)
			}
			if got := f.itemCount(t, f.userID); got != 1 {
				t.Errorf("items = %d, want 1", got)
			}
		})

		t.Run("Replay with a different body", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "client_id": "`+testClientID+`"}`), http.StatusCreated)
			resp := f.capture(t, testToken, `{"url": "https://example.com/b", "user_note": "other", "client_id": "`+testClientID+`"}`)
			_, replay := capturedItem(t, resp, http.StatusOK)
			assertMembers(t, replay, map[string]any{"id": first["id"], "url": "https://example.com/a", "user_note": nil})
		})

		t.Run("Concurrent replays create one item", func(t *testing.T) {
			f := newFixture(t)
			const replays = 8
			body := `{"url": "https://example.com/a", "client_id": "` + testClientID + `"}`
			statuses, ids, errs := make([]int, replays), make([]string, replays), make([]error, replays)
			var wg sync.WaitGroup
			for i := range replays {
				wg.Go(func() { statuses[i], ids[i], errs[i] = postConcurrently(f, body) })
			}
			wg.Wait()
			created := 0
			for i := range replays {
				if errs[i] != nil {
					t.Fatalf("replay %d: %v", i, errs[i])
				}
				switch statuses[i] {
				case http.StatusCreated:
					created++
				case http.StatusOK:
				default:
					t.Errorf("replay %d: status %d, want 201 or 200", i, statuses[i])
				}
				if ids[i] != ids[0] {
					t.Errorf("replay %d: id %s, want %s", i, ids[i], ids[0])
				}
			}
			if created != 1 {
				t.Errorf("%d answers are 201, want exactly 1", created)
			}
			if got := f.itemCount(t, f.userID); got != 1 {
				t.Errorf("items = %d, want 1", got)
			}
		})

		t.Run("Same client_id from another user", func(t *testing.T) {
			f := newFixture(t)
			body := `{"url": "https://example.com/a", "client_id": "` + testClientID + `"}`
			_, mine := capturedItem(t, f.capture(t, testToken, body), http.StatusCreated)
			_, theirs := capturedItem(t, f.capture(t, otherToken, body), http.StatusCreated)
			if theirs["id"] == mine["id"] {
				t.Errorf("user B got user A's item %v", mine["id"])
			}
			if a, b := f.itemCount(t, f.userID), f.itemCount(t, f.otherID); a != 1 || b != 1 {
				t.Errorf("items of A, B = %d, %d; want 1 each", a, b)
			}
		})

		t.Run("Captures without client_id are never replays", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			_, second := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/b"}`), http.StatusCreated)
			if first["id"] == second["id"] {
				t.Errorf("both captures returned %v, want two items", first["id"])
			}
		})

		t.Run("Malformed client_id", func(t *testing.T) {
			f := newFixture(t)
			for _, body := range []string{
				`{"url": "https://example.com/a", "client_id": "not-a-ulid"}`,
				`{"url": "https://example.com/a", "client_id": ""}`,
			} {
				t.Log(body)
				assertProblemType(t, f.capture(t, testToken, body), http.StatusBadRequest, blankType)
			}
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})

		t.Run("Replay of a duplicate capture appends its note once", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			body := `{"url": "https://example.com/a?utm_source=x", "user_note": "to test", "client_id": "` + otherClientID + `"}`
			merged, item := capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			assertMembers(t, item, map[string]any{"id": first["id"], "user_note": "to test"})
			f.clock.Advance(24 * time.Hour)
			replay, _ := capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			if !bytes.Equal(replay, merged) {
				t.Errorf("replay = %s\nwant the item unchanged %s", replay, merged)
			}
		})
	})

	t.Run("Links are normalised for deduplication", func(t *testing.T) {
		t.Run("Tracking parameters do not make a new item", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/article?id=3"}`), http.StatusCreated)
			resp := f.capture(t, testToken, `{"url": "https://example.com/article?utm_source=x&id=3&fbclid=abc&UTM_Medium=y"}`)
			_, duplicate := capturedItem(t, resp, http.StatusOK)
			assertMembers(t, duplicate, map[string]any{"id": first["id"], "url": "https://example.com/article?id=3"})
		})

		t.Run("Fragment, trailing slash and host case", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/docs/page"}`), http.StatusCreated)
			_, duplicate := capturedItem(t, f.capture(t, testToken, `{"url": "HTTPS://Example.COM/docs/page/#section-2"}`), http.StatusOK)
			assertMembers(t, duplicate, map[string]any{"id": first["id"]})
		})

		t.Run("Root, empty query and repeated parameters", func(t *testing.T) {
			f := newFixture(t)
			_, root := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com"}`), http.StatusCreated)
			_, list := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/list?tag=b&tag=a&x=1"}`), http.StatusCreated)
			_, rootAgain := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/?"}`), http.StatusOK)
			_, listAgain := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/list/?x=1&&tag=b&ref=home&tag=a"}`), http.StatusOK)
			assertMembers(t, rootAgain, map[string]any{"id": root["id"]})
			assertMembers(t, listAgain, map[string]any{"id": list["id"]})
		})

		t.Run("Parameter order does not matter", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/search?b=2&a=1"}`), http.StatusCreated)
			_, duplicate := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/search?a=1&b=2"}`), http.StatusOK)
			assertMembers(t, duplicate, map[string]any{"id": first["id"]})
		})

		t.Run("Other differences make another item", func(t *testing.T) {
			f := newFixture(t)
			ids := map[any]bool{}
			for _, url := range []string{
				"https://example.com/a", "http://example.com/a", "https://www.example.com/a",
				"https://example.com:443/a", "https://example.com/A", "https://example.com/a?id=4",
			} {
				t.Log(url)
				_, item := capturedItem(t, f.capture(t, testToken, `{"url": "`+url+`"}`), http.StatusCreated)
				ids[item["id"]] = true
			}
			if len(ids) != 6 {
				t.Errorf("%d different ids, want 6", len(ids))
			}
		})

		t.Run("Duplicates are per user", func(t *testing.T) {
			f := newFixture(t)
			_, mine := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			_, theirs := capturedItem(t, f.capture(t, otherToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			if theirs["id"] == mine["id"] {
				t.Errorf("user B got user A's item %v", mine["id"])
			}
			if a, b := f.itemCount(t, f.userID), f.itemCount(t, f.otherID); a != 1 || b != 1 {
				t.Errorf("items of A, B = %d, %d; want 1 each", a, b)
			}
		})
	})

	t.Run("A duplicate link returns the existing item", func(t *testing.T) {
		t.Run("Duplicate without note", func(t *testing.T) {
			f := newFixture(t)
			first, _ := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			f.clock.Advance(time.Hour)
			duplicate, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a/", "source": "manual"}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"source": "share"})
			if !bytes.Equal(duplicate, first) {
				t.Errorf("200 body = %s\nwant the item as it was %s", duplicate, first)
			}
		})

		t.Run("Concurrent captures of one link create one item", func(t *testing.T) {
			f := newFixture(t)
			const captures = 8
			const clientIDPrefix = "01JB8Z6Q4M1X2Y3Z4A5B6C7D9" // + one Crockford digit: a ULID each
			statuses, ids, errs := make([]int, captures), make([]string, captures), make([]error, captures)
			bodies := make([]string, captures)
			for i := range captures {
				bodies[i] = `{"url": "https://example.com/a", "client_id": "` + clientIDPrefix + string(rune('0'+i)) + `"}`
			}
			var wg sync.WaitGroup
			for i := range captures {
				wg.Go(func() { statuses[i], ids[i], errs[i] = postConcurrently(f, bodies[i]) })
			}
			wg.Wait()
			created := 0
			for i := range captures {
				if errs[i] != nil {
					t.Fatalf("capture %d: %v", i, errs[i])
				}
				switch statuses[i] {
				case http.StatusCreated:
					created++
				case http.StatusOK:
				default:
					t.Errorf("capture %d: status %d, want 201 or 200", i, statuses[i])
				}
				if ids[i] != ids[0] {
					t.Errorf("capture %d: id %s, want %s", i, ids[i], ids[0])
				}
			}
			if created != 1 {
				t.Errorf("%d answers are 201, want exactly 1", created)
			}
			for i := range captures {
				replay := strings.Replace(bodies[i], "https://example.com/a", "https://example.com/other", 1)
				_, item := capturedItem(t, f.capture(t, testToken, replay), http.StatusOK)
				assertMembers(t, item, map[string]any{"id": ids[0]})
			}
			if got := f.itemCount(t, f.userID); got != 1 {
				t.Errorf("items = %d, want 1", got)
			}
		})
	})

	t.Run("The note of a duplicate is appended, dated", func(t *testing.T) {
		t.Run("First note of an item", func(t *testing.T) {
			f := newFixture(t)
			capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "to test"}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"user_note": "to test"})
		})

		t.Run("Further note is dated", func(t *testing.T) {
			f := newFixture(t)
			capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "for the trip"}`), http.StatusCreated)
			f.clock.Set(time.Date(2026, 10, 27, 22, 30, 0, 0, time.UTC)) // 23:30 in Paris
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "  to test "}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"user_note": "for the trip\n\n2026-10-27 — to test"})
		})

		t.Run("The day is the day in Paris", func(t *testing.T) {
			f := newFixture(t)
			capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "for the trip"}`), http.StatusCreated)
			f.clock.Set(time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC)) // 00:30 on 28 October in Paris
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "to test"}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"user_note": "for the trip\n\n2026-10-28 — to test"})
		})

		t.Run("Same note is not repeated", func(t *testing.T) {
			f := newFixture(t)
			capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "for the trip"}`), http.StatusCreated)
			f.clock.Set(time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC))
			before, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "to test"}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"user_note": "for the trip\n\n2026-10-27 — to test"})
			f.clock.Advance(48 * time.Hour)
			for _, note := range []string{"to test", "for the trip"} {
				t.Log(note)
				after, _ := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "`+note+`"}`), http.StatusOK)
				if !bytes.Equal(after, before) {
					t.Errorf("200 body = %s\nwant the item unchanged %s", after, before)
				}
			}
		})

		t.Run("A note contained in another one is still appended", func(t *testing.T) {
			f := newFixture(t)
			capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "buy milk and bread"}`), http.StatusCreated)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "buy milk"}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"user_note": "buy milk and bread\n\n2026-10-27 — buy milk"})
		})

		t.Run("A note change is synchronised", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			f.clock.Advance(time.Minute)
			_, noted := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "user_note": "to test"}`), http.StatusOK)
			if !laterThan(t, noted["updated_at"], first["updated_at"]) {
				t.Errorf("updated_at = %v, want later than %v", noted["updated_at"], first["updated_at"])
			}
			f.clock.Advance(time.Minute)
			_, unchanged := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusOK)
			assertMembers(t, unchanged, map[string]any{"updated_at": noted["updated_at"]})
		})
	})

	t.Run("A duplicate can only make an item private", func(t *testing.T) {
		t.Run("Duplicate marked private", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "is_private": true}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"id": first["id"], "is_private": true, "status": first["status"]})
		})

		t.Run("Replaying an old private capture does not make the item private again", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			body := `{"url": "https://example.com/a", "is_private": true, "client_id": "` + privateClientID + `"}`
			_, item := capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			assertMembers(t, item, map[string]any{"is_private": true})
			id, _ := first["id"].(string)
			if _, err := f.store.SetPrivate(context.Background(), f.userID, id, false); err != nil {
				t.Fatalf("SetPrivate: %v", err)
			}
			_, replay := capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			assertMembers(t, replay, map[string]any{"id": first["id"], "is_private": false})
		})

		t.Run("Unmarked duplicate keeps an item private", func(t *testing.T) {
			f := newFixture(t)
			capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "is_private": true}`), http.StatusCreated)
			_, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a", "is_private": false}`), http.StatusOK)
			assertMembers(t, item, map[string]any{"is_private": true})
		})
	})

	t.Run("The normalised URL is never logged", func(t *testing.T) {
		t.Run("Duplicate capture logs no content", func(t *testing.T) {
			f := newFixture(t)
			body := `{"url": "https://secret.example.com/a?utm_source=x", "user_note": "private thought"}`
			capturedItem(t, f.capture(t, testToken, body), http.StatusCreated)
			capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			logs := f.logs.String()
			for _, leak := range []string{"secret.example.com", "private thought"} {
				if strings.Contains(logs, leak) {
					t.Errorf("logs contain %q:\n%s", leak, logs)
				}
			}
			for _, status := range []string{`"status":201`, `"status":200`} {
				if !strings.Contains(logs, status) {
					t.Errorf("logs have no request line with %s:\n%s", status, logs)
				}
			}
		})
	})

	t.Run("The capture body follows the contract strictly", func(t *testing.T) {
		t.Run("Misspelt member", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, `{"url": "https://example.com/a", "isPrivate": true}`), http.StatusBadRequest, blankType)
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})

		t.Run("Data after the body", func(t *testing.T) {
			f := newFixture(t)
			resp := f.capture(t, testToken, `{"url": "https://example.com/a"} {"url": "https://example.com/b"}`)
			assertProblemType(t, resp, http.StatusBadRequest, blankType)
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})

		t.Run("Empty body", func(t *testing.T) {
			f := newFixture(t)
			assertProblemType(t, f.capture(t, testToken, ""), http.StatusBadRequest, blankType)
		})
	})

	t.Run("The answer is a complete item", func(t *testing.T) {
		t.Run("The answer matches the contract", func(t *testing.T) {
			f := newFixture(t)
			raw, item := capturedItem(t, f.capture(t, testToken, `{"url": "https://example.com/a"}`), http.StatusCreated)
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse body: %v", err)
			}
			if err := itemSchema(t).Validate(instance); err != nil {
				t.Errorf("201 body does not match the Item schema: %v\n%s", err, raw)
			}
			for _, name := range []string{"tag_ids", "assets", "user_edited"} {
				assertEmptyArray(t, item, name)
			}
		})

		t.Run("A replay returns the same body", func(t *testing.T) {
			f := newFixture(t)
			body := `{"url": "https://example.com/a", "user_note": "for the trip", "client_id": "` + testClientID + `"}`
			first, _ := capturedItem(t, f.capture(t, testToken, body), http.StatusCreated)
			replay, _ := capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			if !bytes.Equal(replay, first) {
				t.Errorf("200 body = %s\nwant the 201 body %s", replay, first)
			}
		})
	})

	t.Run("Image captures are not available yet", func(t *testing.T) {
		t.Run("Image capture", func(t *testing.T) {
			f := newFixture(t)
			resp := f.do(t, http.MethodPost, "/api/items", testToken, "multipart/form-data; boundary="+multipartBoundary, multipartImage)
			assertProblemType(t, resp, http.StatusNotImplemented, notImplementedType)
		})

		t.Run("Unsupported body format", func(t *testing.T) {
			f := newFixture(t)
			resp := f.do(t, http.MethodPost, "/api/items", testToken, "text/plain", "https://example.com/a")
			assertProblemType(t, resp, http.StatusUnsupportedMediaType, blankType)
		})
	})

	t.Run("A note capture creates a queued note item", func(t *testing.T) {
		t.Run("Capture a note", func(t *testing.T) {
			f := newFixture(t)
			resp := f.capture(t, testToken, `{"note": "Buy the 10 mm socket\nfor the bike", "is_private": true, "source": "manual"}`)
			_, item := capturedItem(t, resp, http.StatusCreated)
			assertMembers(t, item, map[string]any{
				"kind": "note", "status": "queued", "read_status": "to_read", "url": nil,
				"user_note": "Buy the 10 mm socket\nfor the bike", "is_private": true, "source": "manual",
			})
			if got := f.itemCount(t, f.userID); got != 1 {
				t.Errorf("items = %d, want 1", got)
			}
		})

		t.Run("Same text twice makes two notes", func(t *testing.T) {
			f := newFixture(t)
			_, first := capturedItem(t, f.capture(t, testToken, `{"note": "call the plumber"}`), http.StatusCreated)
			_, second := capturedItem(t, f.capture(t, testToken, `{"note": "call the plumber"}`), http.StatusCreated)
			if first["id"] == second["id"] {
				t.Errorf("both captures returned %v, want two items", first["id"])
			}
			if got := f.itemCount(t, f.userID); got != 2 {
				t.Errorf("items = %d, want 2", got)
			}
		})

		t.Run("Replayed note capture", func(t *testing.T) {
			f := newFixture(t)
			body := `{"note": "call the plumber", "client_id": "` + noteClientID + `"}`
			first, _ := capturedItem(t, f.capture(t, testToken, body), http.StatusCreated)
			replay, _ := capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			if !bytes.Equal(replay, first) {
				t.Errorf("replay = %s\nwant the same item %s", replay, first)
			}
			if got := f.itemCount(t, f.userID); got != 1 {
				t.Errorf("items = %d, want 1", got)
			}
		})

		t.Run("The answer matches the contract for a note", func(t *testing.T) {
			f := newFixture(t)
			raw, item := capturedItem(t, f.capture(t, testToken, `{"note": "buy milk"}`), http.StatusCreated)
			instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("parse body: %v", err)
			}
			if err := itemSchema(t).Validate(instance); err != nil {
				t.Errorf("201 body does not match the Item schema: %v\n%s", err, raw)
			}
			for _, name := range []string{"tag_ids", "assets", "user_edited"} {
				assertEmptyArray(t, item, name)
			}
		})
	})

	t.Run("The text of a note is trimmed and must not be empty", func(t *testing.T) {
		t.Run("Note with surrounding whitespace", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"note": "\n  first line\n\n  second line  \n"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"user_note": "first line\n\n  second line"})
		})

		t.Run("Blank note", func(t *testing.T) {
			f := newFixture(t)
			for _, body := range []string{`{"note": ""}`, `{"note": " \n\t "}`} {
				t.Log(body)
				assertProblemType(t, f.capture(t, testToken, body), http.StatusBadRequest, "urn:hippo:problem:empty-note")
			}
			if got := f.itemCount(t, f.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})
	})

	t.Run("The provisional title of a note is its first line", func(t *testing.T) {
		t.Run("Short first line", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"note": "Shopping\n- eggs\n- milk"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": "Shopping"})
		})

		t.Run("Long first line", func(t *testing.T) {
			f := newFixture(t)
			line := strings.Repeat("abcdefghij", 10)
			_, item := capturedItem(t, f.capture(t, testToken, `{"note": "`+line+`\nmore"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": line[:80] + "…"})
		})

		t.Run("Exactly 80 characters", func(t *testing.T) {
			f := newFixture(t)
			line := strings.Repeat("é", 40) + strings.Repeat("a", 40) // 80 characters, 120 bytes
			_, item := capturedItem(t, f.capture(t, testToken, `{"note": "`+line+`"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": line})
		})

		t.Run("Leading blank lines", func(t *testing.T) {
			f := newFixture(t)
			_, item := capturedItem(t, f.capture(t, testToken, `{"note": "   \n\n  Idea: a shelf  \nmore"}`), http.StatusCreated)
			assertMembers(t, item, map[string]any{"title": "Idea: a shelf"})
		})
	})

	t.Run("A note capture keeps its text out of the logs", func(t *testing.T) {
		t.Run("Note text is not logged", func(t *testing.T) {
			f := newFixture(t)
			capturedItem(t, f.capture(t, testToken, `{"note": "private thought about the plumber"}`), http.StatusCreated)
			logs := f.logs.String()
			if strings.Contains(logs, "private thought") {
				t.Errorf("logs contain the note text:\n%s", logs)
			}
			if !strings.Contains(logs, `"status":201`) {
				t.Errorf("logs have no request line with status 201:\n%s", logs)
			}
		})
	})

	t.Run("Capture requires the token and keeps the content out of the logs", func(t *testing.T) {
		t.Run("Capture without token", func(t *testing.T) {
			f := newFixture(t)
			body := `{"url": "https://example.com/a", "client_id": "` + testClientID + `"}`
			assertProblem(t, f.capture(t, "", body), http.StatusUnauthorized)
			capturedItem(t, f.capture(t, testToken, body), http.StatusCreated)
		})

		t.Run("The captured URL is not logged", func(t *testing.T) {
			f := newFixture(t)
			body := `{"url": "https://secret.example.com/path", "user_note": "private thought", "client_id": "` + testClientID + `"}`
			capturedItem(t, f.capture(t, testToken, body), http.StatusCreated)
			capturedItem(t, f.capture(t, testToken, body), http.StatusOK)
			resp := f.capture(t, testToken, `{"url": "https://secret.example.com/%zz", "user_note": "private thought"}`)
			assertProblemType(t, resp, http.StatusBadRequest, "urn:hippo:problem:invalid-url")

			logs := f.logs.String()
			for _, leak := range []string{"secret.example.com", "private thought"} {
				if strings.Contains(logs, leak) {
					t.Errorf("logs contain %q:\n%s", leak, logs)
				}
			}
			for _, status := range []string{`"status":201`, `"status":200`, `"status":400`} {
				if !strings.Contains(logs, status) {
					t.Errorf("logs have no request line with %s:\n%s", status, logs)
				}
			}
		})
	})
}

// laterThan tells whether the date-time later is after earlier.
func laterThan(t *testing.T, later, earlier any) bool {
	t.Helper()
	parse := func(value any) time.Time {
		text, _ := value.(string)
		parsed, err := time.Parse(time.RFC3339Nano, text)
		if err != nil {
			t.Fatalf("parse date %v: %v", value, err)
		}
		return parsed
	}
	return parse(later).After(parse(earlier))
}

// postConcurrently sends a capture from a goroutine, where t.Fatal cannot be called.
func postConcurrently(f fixture, body string) (status int, id string, err error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, f.server.URL+"/api/items", strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.server.Client().Do(req)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var item struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&item); err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, item.ID, nil
}
