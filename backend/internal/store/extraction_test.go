package store_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
)

func TestMigrationExtractionAttempts(t *testing.T) {
	t.Run("refuses an attempt row for another user's item", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		item, _ := capture(t, st, owner, link("https://example.com/a"), keepNote)
		if err := st.StartExtractionAttempt(context.Background(), other, item.ID); err == nil {
			t.Fatal("an attempt of another user than its item's was stored")
		}
		if err := st.StartExtractionAttempt(context.Background(), owner, item.ID); err != nil {
			t.Fatalf("an attempt of the item's user was refused: %v", err)
		}
		if err := st.StartExtractionAttempt(context.Background(), other, item.ID); err == nil {
			t.Fatal("another user restarted the attempt of the owner's item")
		}
	})
}

func TestUserIDs(t *testing.T) {
	t.Run("lists every user", func(t *testing.T) {
		st, _ := openWithClock(t)
		first, second := newUser(t, st, "a"), newUser(t, st, "b")
		got, err := st.UserIDs(context.Background())
		if err != nil {
			t.Fatalf("UserIDs: %v", err)
		}
		slices.Sort(got)
		want := []string{first, second}
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("UserIDs = %v, want %v", got, want)
		}
	})
}

func TestQueuedItems(t *testing.T) {
	ctx := context.Background()

	t.Run("lists the queued items of the user, oldest first, without deferred PDFs", func(t *testing.T) {
		st, clock := openWithClock(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		first, _ := capture(t, st, owner, link("https://example.com/first"), keepNote)
		clock.Advance(time.Second)
		pdf, _ := capture(t, st, owner, link("https://example.com/doc.pdf"), keepNote)
		clock.Advance(time.Second)
		started, _ := capture(t, st, owner, link("https://example.com/started"), keepNote)
		clock.Advance(time.Second)
		noteItem, _ := capture(t, st, owner, note("buy milk"), nil)
		clock.Advance(time.Second)
		done, _ := capture(t, st, owner, link("https://example.com/done"), keepNote)
		capture(t, st, other, link("https://example.com/other"), keepNote)

		if err := st.StartExtractionAttempt(ctx, owner, pdf.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		if err := st.DeferExtraction(ctx, owner, pdf.ID); err != nil {
			t.Fatalf("DeferExtraction: %v", err)
		}
		if err := st.StartExtractionAttempt(ctx, owner, started.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		if _, err := st.WriteExtraction(ctx, owner, done.ID, store.Extraction{Mode: "fallback"}); err != nil {
			t.Fatalf("WriteExtraction: %v", err)
		}

		got, err := st.QueuedItems(ctx, owner, "", 10)
		if err != nil {
			t.Fatalf("QueuedItems: %v", err)
		}
		var ids []string
		for _, item := range got {
			ids = append(ids, item.ID)
		}
		if want := []string{first.ID, started.ID, noteItem.ID}; !slices.Equal(ids, want) {
			t.Fatalf("QueuedItems = %v, want %v", ids, want)
		}
		if got[0].Kind != "link" || got[0].URL == nil || *got[0].URL != "https://example.com/first" || got[0].Title != "example.com" {
			t.Errorf("first item = %+v, want the link with its url and provisional title", got[0])
		}
		if got[0].AttemptStarted || !got[1].AttemptStarted || got[2].AttemptStarted {
			t.Errorf("attempt started = %v %v %v, want only the second", got[0].AttemptStarted, got[1].AttemptStarted, got[2].AttemptStarted)
		}
		if got[2].Kind != "note" || got[2].URL != nil {
			t.Errorf("third item = %+v, want the note without url", got[2])
		}
	})

	t.Run("pages after an id and within a limit", func(t *testing.T) {
		st, clock := openWithClock(t)
		owner := newUser(t, st, "owner")
		var ids []string
		for _, path := range []string{"a", "b", "c"} {
			item, _ := capture(t, st, owner, link("https://example.com/"+path), keepNote)
			ids = append(ids, item.ID)
			clock.Advance(time.Second)
		}
		page, err := st.QueuedItems(ctx, owner, ids[0], 1)
		if err != nil {
			t.Fatalf("QueuedItems: %v", err)
		}
		if len(page) != 1 || page[0].ID != ids[1] {
			t.Errorf("QueuedItems after the first = %+v, want the second only", page)
		}
	})
}

func TestExtractionAttempts(t *testing.T) {
	ctx := context.Background()

	t.Run("a released attempt no longer marks the item and keeps no download under way", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		item, _ := capture(t, st, owner, link("https://example.com/a"), keepNote)
		if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		before := store.SequenceForTest(t, st)
		if err := st.ReleaseExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("ReleaseExtractionAttempt: %v", err)
		}
		got, err := st.QueuedItems(ctx, owner, "", 10)
		if err != nil {
			t.Fatalf("QueuedItems: %v", err)
		}
		if len(got) != 1 || got[0].AttemptStarted || got[0].NetworkRetries != 0 {
			t.Errorf("QueuedItems = %+v, want the item without attempt under way nor retry", got)
		}
		if after := store.SequenceForTest(t, st); after != before {
			t.Errorf("sequence moved from %d to %d: attempts are not synced", before, after)
		}
		// A released attempt starts again.
		if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("StartExtractionAttempt after a release: %v", err)
		}
		if got, _ := st.QueuedItems(ctx, owner, "", 10); len(got) != 1 || !got[0].AttemptStarted {
			t.Errorf("QueuedItems = %+v, want the attempt under way again", got)
		}
	})

	t.Run("a postponed attempt counts the network retries it is told to and keeps them across attempts", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		item, _ := capture(t, st, owner, link("https://example.com/a"), keepNote)
		if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		if err := st.PostponeExtraction(ctx, owner, item.ID, false); err != nil {
			t.Fatalf("PostponeExtraction: %v", err)
		}
		if got, _ := st.QueuedItems(ctx, owner, "", 10); len(got) != 1 || got[0].AttemptStarted || got[0].NetworkRetries != 0 {
			t.Errorf("after an uncounted postponement: QueuedItems = %+v, want no retry and no attempt under way", got)
		}
		for want := 1; want <= 2; want++ {
			if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
				t.Fatalf("StartExtractionAttempt: %v", err)
			}
			if err := st.PostponeExtraction(ctx, owner, item.ID, true); err != nil {
				t.Fatalf("PostponeExtraction: %v", err)
			}
			got, err := st.QueuedItems(ctx, owner, "", 10)
			if err != nil {
				t.Fatalf("QueuedItems: %v", err)
			}
			if len(got) != 1 || got[0].AttemptStarted || got[0].NetworkRetries != want {
				t.Errorf("after postponement %d: QueuedItems = %+v, want %d retries and no attempt under way", want, got, want)
			}
		}
		if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		if err := st.ReleaseExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("ReleaseExtractionAttempt: %v", err)
		}
		if got, _ := st.QueuedItems(ctx, owner, "", 10); len(got) != 1 || got[0].NetworkRetries != 2 {
			t.Errorf("QueuedItems = %+v, want the 2 retries kept by a stop", got)
		}
	})

	t.Run("a deferred PDF is no longer under way", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		item, _ := capture(t, st, owner, link("https://example.com/doc.pdf"), keepNote)
		if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		if err := st.DeferExtraction(ctx, owner, item.ID); err != nil {
			t.Fatalf("DeferExtraction: %v", err)
		}
		if store.AttemptUnderWayForTest(t, st, owner, item.ID) {
			t.Error("started_at is still set on a deferred PDF")
		}
	})

	t.Run("another user cannot defer, release or postpone an attempt", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		item, _ := capture(t, st, owner, link("https://example.com/a"), keepNote)
		if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		if err := st.DeferExtraction(ctx, other, item.ID); err != nil {
			t.Fatalf("DeferExtraction: %v", err)
		}
		if err := st.ReleaseExtractionAttempt(ctx, other, item.ID); err != nil {
			t.Fatalf("ReleaseExtractionAttempt: %v", err)
		}
		if err := st.PostponeExtraction(ctx, other, item.ID, true); err != nil {
			t.Fatalf("PostponeExtraction: %v", err)
		}
		got, err := st.QueuedItems(ctx, owner, "", 10)
		if err != nil {
			t.Fatalf("QueuedItems: %v", err)
		}
		if len(got) != 1 || !got[0].AttemptStarted || got[0].NetworkRetries != 0 {
			t.Errorf("QueuedItems = %+v, want the item with its attempt untouched", got)
		}
	})
}

func TestWriteExtraction(t *testing.T) {
	ctx := context.Background()

	t.Run("writes the extraction columns, advances the sequence and drops the attempt", func(t *testing.T) {
		st, clock := openWithClock(t)
		owner := newUser(t, st, "owner")
		in := link("https://example.com/a")
		in.UserNote = ptr("my note")
		in.IsPrivate = true
		item, _ := capture(t, st, owner, in, keepNote)
		if err := st.StartExtractionAttempt(ctx, owner, item.ID); err != nil {
			t.Fatalf("StartExtractionAttempt: %v", err)
		}
		clock.Advance(time.Minute)
		before := store.SequenceForTest(t, st)

		written, err := st.WriteExtraction(ctx, owner, item.ID, store.Extraction{
			Mode: "full", Title: ptr("Tuning SQLite"), SiteName: ptr("Example Blog"),
			ContentText: ptr("Article text"), ContentTruncated: true, ReadingMinutes: ptr[int64](4),
		})
		if err != nil || !written {
			t.Fatalf("WriteExtraction = %v, %v; want written", written, err)
		}
		got, err := st.Item(ctx, owner, item.ID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if got.Status != "extracted" || got.ExtractionMode == nil || *got.ExtractionMode != "full" {
			t.Errorf("status %q mode %v, want extracted and full", got.Status, got.ExtractionMode)
		}
		if got.Title != "Tuning SQLite" || got.SiteName == nil || *got.SiteName != "Example Blog" {
			t.Errorf("title %q site %v, want the extracted ones", got.Title, got.SiteName)
		}
		if !got.ContentTruncated || got.ReadingMinutes == nil || *got.ReadingMinutes != 4 {
			t.Errorf("truncated %v reading %v, want true and 4", got.ContentTruncated, got.ReadingMinutes)
		}
		if got.UserNote == nil || *got.UserNote != "my note" || !got.IsPrivate {
			t.Errorf("note %v private %v, want them untouched", got.UserNote, got.IsPrivate)
		}
		wantDate := clock.Now()
		if got.ExtractedAt == nil || !got.ExtractedAt.Equal(wantDate) || !got.UpdatedAt.Equal(wantDate) {
			t.Errorf("extracted_at %v updated_at %v, want %v", got.ExtractedAt, got.UpdatedAt, wantDate)
		}
		if got.ChangeSeq != before+1 || store.SequenceForTest(t, st) != before+1 {
			t.Errorf("change_seq %d, want %d", got.ChangeSeq, before+1)
		}
		content, err := st.ItemContent(ctx, owner, item.ID)
		if err != nil || content == nil || *content != "Article text" {
			t.Errorf("ItemContent = %v, %v; want the article text", content, err)
		}
		if queued, err := st.QueuedItems(ctx, owner, "", 10); err != nil || len(queued) != 0 {
			t.Errorf("QueuedItems = %+v, %v; want none", queued, err)
		}
		if got := store.AttemptCountForTest(t, st, owner); got != 0 {
			t.Errorf("attempt rows = %d, want the attempt deleted with the write", got)
		}
	})

	t.Run("a nil title keeps the item's title and null columns stay null", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		item, _ := capture(t, st, owner, note("buy milk"), nil)
		if _, err := st.WriteExtraction(ctx, owner, item.ID, store.Extraction{Mode: "none"}); err != nil {
			t.Fatalf("WriteExtraction: %v", err)
		}
		got, err := st.Item(ctx, owner, item.ID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if got.Title != "buy milk" || got.ReadingMinutes != nil || got.ContentTruncated {
			t.Errorf("item = %+v, want its title and no reading time", got)
		}
		nulls := store.NullColumnsForTest(t, st, owner, item.ID)
		if !nulls["content_text"] || !nulls["site_name"] || nulls["extraction_mode"] {
			t.Errorf("null columns = %v, want content_text and site_name null, extraction_mode set", nulls)
		}
	})

	t.Run("an item no longer queued or of another user is not written", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		item, _ := capture(t, st, owner, link("https://example.com/a"), keepNote)
		if written, err := st.WriteExtraction(ctx, other, item.ID, store.Extraction{Mode: "fallback"}); err != nil || written {
			t.Fatalf("WriteExtraction by another user = %v, %v; want not written", written, err)
		}
		if written, err := st.WriteExtraction(ctx, owner, item.ID, store.Extraction{Mode: "fallback"}); err != nil || !written {
			t.Fatalf("WriteExtraction = %v, %v; want written", written, err)
		}
		before := store.SequenceForTest(t, st)
		written, err := st.WriteExtraction(ctx, owner, item.ID, store.Extraction{Mode: "full", Title: ptr("Other")})
		if err != nil || written {
			t.Fatalf("second WriteExtraction = %v, %v; want ignored", written, err)
		}
		if after := store.SequenceForTest(t, st); after != before {
			t.Errorf("sequence moved from %d to %d on an ignored write", before, after)
		}
		got, err := st.Item(ctx, owner, item.ID)
		if err != nil || got.Title != "example.com" || *got.ExtractionMode != "fallback" {
			t.Errorf("item = %+v, %v; want the first write kept", got, err)
		}
	})

	t.Run("item content of another user is not found", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		item, _ := capture(t, st, owner, link("https://example.com/a"), keepNote)
		if _, err := st.ItemContent(ctx, other, item.ID); err == nil {
			t.Error("ItemContent of another user's item: want ErrNotFound")
		}
	})
}
