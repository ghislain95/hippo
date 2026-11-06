package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

// extracted captures in for userID and writes its extraction, so that it waits for the nightly
// batch.
func extracted(t *testing.T, st *store.Store, userID string, in store.NewItem, e store.Extraction) store.Item {
	t.Helper()
	item, _ := capture(t, st, userID, in, keepNote)
	written, err := st.WriteExtraction(context.Background(), userID, item.ID, e)
	if err != nil || !written {
		t.Fatalf("WriteExtraction = %v, %v; want written", written, err)
	}
	return item
}

func queueIDs(t *testing.T, st *store.Store, userID string, limit int) []string {
	t.Helper()
	queue, err := st.AnalysisQueue(context.Background(), userID, limit)
	if err != nil {
		t.Fatalf("AnalysisQueue: %v", err)
	}
	ids := make([]string, len(queue))
	for i, item := range queue {
		ids[i] = item.ID
	}
	return ids
}

func TestAnalysisQueue(t *testing.T) {
	ctx := context.Background()
	full := store.Extraction{Mode: "full", ContentText: ptr("Body")}

	t.Run("lists the oldest capture first, whatever the order the fixture wrote them in", func(t *testing.T) {
		clock := newClock()
		st, _ := storetest.OpenWithClock(t, clock.Now)
		userID := newUser(t, st, "owner")

		clock.Advance(3 * time.Minute)
		newest := extracted(t, st, userID, link("https://example.com/newest"), full)
		clock.Advance(-2 * time.Minute)
		oldest := extracted(t, st, userID, link("https://example.com/oldest"), full)
		clock.Advance(time.Minute)
		middle := extracted(t, st, userID, link("https://example.com/middle"), full)

		got := queueIDs(t, st, userID, 200)
		want := []string{oldest.ID, middle.ID, newest.ID}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("AnalysisQueue = %v, want %v (oldest first)", got, want)
		}
	})

	t.Run("returns at most limit items, the oldest", func(t *testing.T) {
		clock := newClock()
		st, _ := storetest.OpenWithClock(t, clock.Now)
		userID := newUser(t, st, "owner")
		const total = 203
		ids := make([]string, total)
		// Written newest first: the order of the queue comes from the creation dates.
		for i := total - 1; i >= 0; i-- {
			clock.Advance(-time.Second)
			ids[i] = extracted(t, st, userID, note(fmt.Sprintf("note %d", i)), store.Extraction{Mode: "none"}).ID
		}

		got := queueIDs(t, st, userID, 200)
		if fmt.Sprint(got) != fmt.Sprint(ids[:200]) {
			t.Errorf("AnalysisQueue(200) = %d items, want the 200 oldest in order", len(got))
		}
	})

	t.Run("refuses a limit below one rather than list the whole queue", func(t *testing.T) {
		st := storetest.Open(t)
		userID := newUser(t, st, "owner")
		extracted(t, st, userID, note("waiting"), store.Extraction{Mode: "none"})
		for _, limit := range []int{0, -1} {
			if got, err := st.AnalysisQueue(ctx, userID, limit); err == nil {
				t.Errorf("AnalysisQueue(%d) = %d items, nil error; want an error", limit, len(got))
			}
		}
	})

	t.Run("lists only extracted public items that are not deleted", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		userID := newUser(t, st, "owner")
		waiting := extracted(t, st, userID, link("https://example.com/waiting"), full)
		capture(t, st, userID, link("https://example.com/queued"), keepNote)
		for _, status := range []string{"submitted", "analyzed", "failed"} {
			item := extracted(t, st, userID, link("https://example.com/"+status), full)
			storetest.SetAnalysisState(t, path, userID, item.ID, status, nil, 1)
		}
		private := link("https://example.com/private")
		private.IsPrivate = true
		extracted(t, st, userID, private, full)
		madePrivate := extracted(t, st, userID, link("https://example.com/made-private"), full)
		if _, err := st.SetPrivate(ctx, userID, madePrivate.ID, true); err != nil {
			t.Fatalf("SetPrivate: %v", err)
		}
		deleted := extracted(t, st, userID, link("https://example.com/deleted"), full)
		storetest.DeleteItem(t, path, userID, deleted.ID)

		if got := queueIDs(t, st, userID, 200); fmt.Sprint(got) != fmt.Sprint([]string{waiting.ID}) {
			t.Errorf("AnalysisQueue = %v, want only %s", got, waiting.ID)
		}
	})

	t.Run("lists only the user's items", func(t *testing.T) {
		st := storetest.Open(t)
		first, second := newUser(t, st, "first"), newUser(t, st, "second")
		mine := extracted(t, st, first, link("https://example.com/a"), full)
		theirs := extracted(t, st, second, link("https://example.com/a"), full)

		if got := queueIDs(t, st, first, 200); fmt.Sprint(got) != fmt.Sprint([]string{mine.ID}) {
			t.Errorf("first user's queue = %v, want %s", got, mine.ID)
		}
		if got := queueIDs(t, st, second, 200); fmt.Sprint(got) != fmt.Sprint([]string{theirs.ID}) {
			t.Errorf("second user's queue = %v, want %s", got, theirs.ID)
		}
	})

	t.Run("returns what the item message needs, without the content", func(t *testing.T) {
		st, path := storetest.OpenWithPath(t)
		userID := newUser(t, st, "owner")
		withNote := link("https://example.com/page")
		withNote.UserNote = ptr("to read")
		page := extracted(t, st, userID, withNote, store.Extraction{
			Mode: "full", Title: ptr("A page"), SiteName: ptr("Example"), ContentText: ptr("Body"), ContentTruncated: true,
		})
		storetest.SetIncompleteRetried(t, path, userID, page.ID)
		idea := extracted(t, st, userID, note("An idea"), store.Extraction{Mode: "none"})
		picture := extracted(t, st, userID, image(t, st, userID, nil), store.Extraction{Mode: "none"})

		queue, err := st.AnalysisQueue(ctx, userID, 200)
		if err != nil || len(queue) != 3 {
			t.Fatalf("AnalysisQueue = %d items (%v), want 3", len(queue), err)
		}
		got := queue[0]
		if got.ID != page.ID || got.Kind != "link" || *got.URL != "https://example.com/page" || *got.SiteName != "Example" ||
			got.Title != "A page" || *got.ExtractionMode != "full" || !got.ContentTruncated || *got.UserNote != "to read" ||
			!got.IncompleteRetried || got.IsPrivate || len(got.Assets) != 0 {
			t.Errorf("link = %+v, want its url, site, title, mode, truncation, note and retry flag", got)
		}
		got = queue[1]
		if got.ID != idea.ID || got.Kind != "note" || got.URL != nil || got.SiteName != nil || *got.ExtractionMode != "none" ||
			got.ContentTruncated || *got.UserNote != "An idea" || got.IncompleteRetried || len(got.Assets) != 0 {
			t.Errorf("note = %+v, want its note and no url", got)
		}
		got = queue[2]
		if got.ID != picture.ID || got.Kind != "image" || len(got.Assets) != 1 || got.Assets[0].ID != picture.Assets[0].ID ||
			got.Assets[0].MimeType != "image/png" {
			t.Errorf("image = %+v, want its asset %s", got, picture.Assets[0].ID)
		}
	})
}
