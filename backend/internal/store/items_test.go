package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

const (
	testClientID  = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E"
	otherClientID = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8F"
)

func ptr[T any](v T) *T { return &v }

// testClock is a settable clock shared by the store and the test.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *testClock {
	return &testClock{now: time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func openWithClock(t *testing.T) (*store.Store, *testClock) {
	t.Helper()
	clock := newClock()
	st, _ := storetest.OpenWithClock(t, clock.Now)
	return st, clock
}

func newUser(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	id, err := st.CreateUser(context.Background(), name, "hash-"+name)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return id
}

// link is a link capture whose normalised URL is its URL: normalisation belongs to capture.
func link(url string) store.NewItem {
	return store.NewItem{Kind: "link", Source: "share", Status: "queued", Title: "example.com", URL: ptr(url), NormalizedURL: ptr(url)}
}

// appendNote is a merge that appends note after a "|", unless the note already equals it. It
// records the clock it received.
func appendNote(note string, received *time.Time) store.NoteMerge {
	return func(existing *string, now time.Time) (*string, bool) {
		if received != nil {
			*received = now
		}
		if existing == nil {
			return &note, true
		}
		if *existing == note {
			return existing, false
		}
		merged := *existing + "|" + note
		return &merged, true
	}
}

// keepNote is the merge of a capture without note.
func keepNote(existing *string, _ time.Time) (*string, bool) { return existing, false }

func capture(t *testing.T, st *store.Store, userID string, in store.NewItem, merge store.NoteMerge) (store.Item, bool) {
	t.Helper()
	item, created, err := st.CaptureItem(context.Background(), userID, in, merge)
	if err != nil {
		t.Fatalf("CaptureItem: %v", err)
	}
	return item, created
}

func withClientID(in store.NewItem, clientID string) store.NewItem {
	in.ClientID = ptr(clientID)
	return in
}

func TestCreateUser(t *testing.T) {
	t.Run("creates distinct users", func(t *testing.T) {
		st := storetest.Open(t)
		first, second := newUser(t, st, "a"), newUser(t, st, "b")
		if first == second || len(first) != 26 || len(second) != 26 {
			t.Errorf("user ids %q and %q, want two different ULIDs", first, second)
		}
	})
}

func TestCaptureItem(t *testing.T) {
	ctx := context.Background()

	t.Run("a new item is created with a change_seq above the previous write", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		before := store.SequenceForTest(t, st)
		first, created := capture(t, st, user, link("https://example.com/a"), keepNote)
		if !created {
			t.Fatal("first capture: created = false, want true")
		}
		if first.ChangeSeq <= before {
			t.Errorf("change_seq = %d, want above %d", first.ChangeSeq, before)
		}
		second, _ := capture(t, st, user, link("https://example.com/b"), keepNote)
		if second.ChangeSeq <= first.ChangeSeq {
			t.Errorf("second change_seq = %d, want above %d", second.ChangeSeq, first.ChangeSeq)
		}
		if got := store.SequenceForTest(t, st); got != second.ChangeSeq {
			t.Errorf("sequence = %d, want the last write's %d", got, second.ChangeSeq)
		}
	})

	t.Run("the stored item is read back with its defaults", func(t *testing.T) {
		st, clock := openWithClock(t)
		user := newUser(t, st, "owner")
		in := link("https://example.com/a")
		in.UserNote, in.IsPrivate, in.Source = ptr("for the trip"), true, "manual"
		item, _ := capture(t, st, user, in, keepNote)
		if item.UserID != user || item.Kind != "link" || item.Source != "manual" || item.Status != "queued" ||
			item.Title != "example.com" || item.ReadStatus != "to_read" || !item.IsPrivate {
			t.Errorf("item = %+v, want the captured values", item)
		}
		if item.URL == nil || *item.URL != "https://example.com/a" || item.UserNote == nil || *item.UserNote != "for the trip" {
			t.Errorf("url, user_note = %v, %v; want the captured values", item.URL, item.UserNote)
		}
		if item.ReadProgress != 0 || item.Attempts != 0 || item.IsPinned || item.ContentTruncated ||
			item.UserEdited == nil || len(item.UserEdited) != 0 {
			t.Errorf("item = %+v, want the defaults of a new item", item)
		}
		if !item.CreatedAt.Equal(clock.Now()) || !item.UpdatedAt.Equal(item.CreatedAt) || item.CreatedAt.Location().String() != "UTC" {
			t.Errorf("created_at, updated_at = %v, %v; want the clock's UTC date %v", item.CreatedAt, item.UpdatedAt, clock.Now())
		}
		read, err := st.Item(ctx, user, item.ID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if read.ID != item.ID || read.ChangeSeq != item.ChangeSeq || !read.CreatedAt.Equal(item.CreatedAt) {
			t.Errorf("read back %+v, want %+v", read, item)
		}
	})

	t.Run("a known client_id of the same user returns the stored item and leaves the sequence", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, withClientID(link("https://example.com/a"), testClientID), keepNote)
		sequence := store.SequenceForTest(t, st)

		other := withClientID(link("https://example.com/b"), testClientID)
		other.UserNote = ptr("other")
		replay, created := capture(t, st, user, other, appendNote("other", nil))
		if created {
			t.Error("replay: created = true, want false")
		}
		if replay.ID != first.ID || *replay.URL != "https://example.com/a" || replay.UserNote != nil {
			t.Errorf("replay = %+v, want the first item unchanged", replay)
		}
		if got := store.SequenceForTest(t, st); got != sequence {
			t.Errorf("sequence after replay = %d, want %d", got, sequence)
		}
		if got := store.ItemCountForTest(t, st, user); got != 1 {
			t.Errorf("items = %d, want 1", got)
		}
	})

	t.Run("a client_id merged into an existing item is replayed without merging again", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, link("https://example.com/a"), keepNote)

		duplicate := withClientID(link("https://example.com/a"), testClientID)
		duplicate.IsPrivate = true
		merged, created := capture(t, st, user, duplicate, appendNote("to test", nil))
		if created || merged.ID != first.ID || merged.UserNote == nil || *merged.UserNote != "to test" || !merged.IsPrivate {
			t.Fatalf("duplicate = %+v (created %v), want the first item with the note, private", merged, created)
		}
		if _, err := st.SetPrivate(ctx, user, first.ID, false); err != nil {
			t.Fatalf("SetPrivate: %v", err)
		}
		sequence := store.SequenceForTest(t, st)

		replay, created := capture(t, st, user, duplicate, appendNote("again", nil))
		if created || replay.ID != first.ID {
			t.Errorf("replay = %s (created %v), want the item %s", replay.ID, created, first.ID)
		}
		if replay.UserNote == nil || *replay.UserNote != "to test" || replay.IsPrivate {
			t.Errorf("replay note, private = %v, %v; want the note once and the item public", replay.UserNote, replay.IsPrivate)
		}
		if got := store.SequenceForTest(t, st); got != sequence {
			t.Errorf("sequence after replay = %d, want %d", got, sequence)
		}
	})

	t.Run("a duplicate without change writes nothing to items", func(t *testing.T) {
		st, clock := openWithClock(t)
		user := newUser(t, st, "owner")
		in := link("https://example.com/a")
		in.UserNote = ptr("for the trip")
		first, _ := capture(t, st, user, in, keepNote)
		sequence := store.SequenceForTest(t, st)
		clock.Advance(time.Hour)

		again := link("https://example.com/a")
		again.Source, again.Title, again.URL = "manual", "other.example.com", ptr("https://example.com/a/")
		duplicate, created := capture(t, st, user, again, appendNote("for the trip", nil))
		if created || duplicate.ID != first.ID {
			t.Errorf("duplicate = %s (created %v), want the item %s", duplicate.ID, created, first.ID)
		}
		if duplicate.Source != "share" || duplicate.Title != "example.com" || *duplicate.URL != "https://example.com/a" ||
			duplicate.Status != "queued" || *duplicate.UserNote != "for the trip" {
			t.Errorf("duplicate = %+v, want the item unchanged", duplicate)
		}
		if !duplicate.UpdatedAt.Equal(first.UpdatedAt) || duplicate.ChangeSeq != first.ChangeSeq {
			t.Errorf("updated_at, change_seq = %v, %d; want %v, %d", duplicate.UpdatedAt, duplicate.ChangeSeq, first.UpdatedAt, first.ChangeSeq)
		}
		if got := store.SequenceForTest(t, st); got != sequence {
			t.Errorf("sequence = %d, want %d", got, sequence)
		}
		if got := store.ItemCountForTest(t, st, user); got != 1 {
			t.Errorf("items = %d, want 1", got)
		}
	})

	t.Run("a duplicate that changes the note advances updated_at and change_seq once", func(t *testing.T) {
		st, clock := openWithClock(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, link("https://example.com/a"), keepNote)
		sequence := store.SequenceForTest(t, st)
		clock.Advance(time.Hour)

		var received time.Time
		duplicate, created := capture(t, st, user, link("https://example.com/a"), appendNote("to test", &received))
		if created || duplicate.ID != first.ID || duplicate.UserNote == nil || *duplicate.UserNote != "to test" {
			t.Fatalf("duplicate = %+v (created %v), want the item with the note", duplicate, created)
		}
		if !received.Equal(clock.Now()) {
			t.Errorf("merge received %v, want the store's clock %v", received, clock.Now())
		}
		if !duplicate.UpdatedAt.Equal(clock.Now()) || !duplicate.CreatedAt.Equal(first.CreatedAt) {
			t.Errorf("updated_at, created_at = %v, %v; want %v, %v", duplicate.UpdatedAt, duplicate.CreatedAt, clock.Now(), first.CreatedAt)
		}
		if got := store.SequenceForTest(t, st); got != sequence+1 || duplicate.ChangeSeq != sequence+1 {
			t.Errorf("sequence, change_seq = %d, %d; want %d", got, duplicate.ChangeSeq, sequence+1)
		}
	})

	t.Run("a duplicate records its client_id", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, link("https://example.com/a"), keepNote)
		capture(t, st, user, withClientID(link("https://example.com/a"), testClientID), keepNote)

		replay, created := capture(t, st, user, withClientID(link("https://example.com/b"), testClientID), keepNote)
		if created || replay.ID != first.ID {
			t.Errorf("replay of a duplicate's client_id = %s (created %v), want the item %s", replay.ID, created, first.ID)
		}
		if got := store.ItemCountForTest(t, st, user); got != 1 {
			t.Errorf("items = %d, want 1", got)
		}
	})

	t.Run("a duplicate only makes an item private", func(t *testing.T) {
		st, clock := openWithClock(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, link("https://example.com/a"), keepNote)
		clock.Advance(time.Hour)

		private := link("https://example.com/a")
		private.IsPrivate = true
		made, _ := capture(t, st, user, private, keepNote)
		if !made.IsPrivate || made.ChangeSeq <= first.ChangeSeq || !made.UpdatedAt.Equal(clock.Now()) {
			t.Errorf("private duplicate = %+v, want the item private, with a new change_seq and updated_at", made)
		}
		if made.Status != first.Status {
			t.Errorf("status = %s, want %s unchanged", made.Status, first.Status)
		}
		sequence := store.SequenceForTest(t, st)
		clock.Advance(time.Hour)

		kept, _ := capture(t, st, user, link("https://example.com/a"), keepNote)
		if !kept.IsPrivate || kept.ChangeSeq != made.ChangeSeq || !kept.UpdatedAt.Equal(made.UpdatedAt) {
			t.Errorf("unmarked duplicate = %+v, want the item still private and untouched", kept)
		}
		if got := store.SequenceForTest(t, st); got != sequence {
			t.Errorf("sequence = %d, want %d", got, sequence)
		}
	})

	t.Run("a duplicate that changes note and privacy advances the sequence once", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		capture(t, st, user, link("https://example.com/a"), keepNote)
		sequence := store.SequenceForTest(t, st)
		private := link("https://example.com/a")
		private.IsPrivate = true
		item, _ := capture(t, st, user, private, appendNote("to test", nil))
		if !item.IsPrivate || item.UserNote == nil || *item.UserNote != "to test" {
			t.Errorf("duplicate = %+v, want the note and the item private", item)
		}
		if got := store.SequenceForTest(t, st); got != sequence+1 || item.ChangeSeq != sequence+1 {
			t.Errorf("sequence, change_seq = %d, %d; want %d", got, item.ChangeSeq, sequence+1)
		}
	})

	t.Run("two users are isolated", func(t *testing.T) {
		st := storetest.Open(t)
		a, b := newUser(t, st, "a"), newUser(t, st, "b")
		in := withClientID(link("https://example.com/a"), testClientID)
		first, _ := capture(t, st, a, in, keepNote)
		second, created := capture(t, st, b, in, appendNote("mine", nil))
		if !created || second.ID == first.ID || second.UserID != b {
			t.Errorf("other user's capture = %+v (created %v), want a new item of user b", second, created)
		}
		duplicateOfB, created := capture(t, st, b, link("https://example.com/a"), keepNote)
		if created || duplicateOfB.ID != second.ID {
			t.Errorf("duplicate of user b = %s (created %v), want b's item %s", duplicateOfB.ID, created, second.ID)
		}
		read, err := st.Item(ctx, a, first.ID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if read.UserNote != nil || read.ChangeSeq != first.ChangeSeq {
			t.Errorf("user a's item = %+v, want it untouched", read)
		}
		if got := store.ItemCountForTest(t, st, a); got != 1 {
			t.Errorf("items of a = %d, want 1", got)
		}
	})

	t.Run("an item without normalised URL is never a duplicate", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		in := link("https://example.com/a")
		in.NormalizedURL = nil
		first, _ := capture(t, st, user, in, keepNote)
		second, created := capture(t, st, user, in, keepNote)
		if !created || second.ID == first.ID {
			t.Errorf("second capture = %s (created %v), want a new item", second.ID, created)
		}
	})

	t.Run("absent values are stored as NULL", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		in := link("https://example.com/a")
		in.NormalizedURL = nil
		item, _ := capture(t, st, user, in, keepNote)
		nulls := store.NullColumnsForTest(t, st, user, item.ID)
		for _, column := range []string{"normalized_url", "user_note", "site_name", "extraction_mode", "content_text", "fiche_json", "deleted_at"} {
			if !nulls[column] {
				t.Errorf("%s is not NULL", column)
			}
		}
		if nulls["url"] {
			t.Error("url is NULL, want the captured URL")
		}
		stored, _ := capture(t, st, user, link("https://example.com/b"), keepNote)
		if store.NullColumnsForTest(t, st, user, stored.ID)["normalized_url"] {
			t.Error("normalized_url is NULL, want the normalised URL")
		}
	})

	t.Run("the safety net on the capture_ids insert answers a replay", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, withClientID(link("https://example.com/a"), testClientID), keepNote)
		sequence := store.SequenceForTest(t, st)
		replay, created, err := store.CaptureSkippingLookupsForTest(t, st, user, withClientID(link("https://example.com/b"), testClientID), keepNote)
		if err != nil {
			t.Fatalf("capture of a taken client_id: %v, want a replay", err)
		}
		if created || replay.ID != first.ID {
			t.Errorf("capture of a taken client_id = %s (created %v), want the replay of %s", replay.ID, created, first.ID)
		}
		if got := store.SequenceForTest(t, st); got != sequence {
			t.Errorf("sequence = %d, want %d: the rolled back insert must not advance it", got, sequence)
		}
		if got := store.ItemCountForTest(t, st, user); got != 1 {
			t.Errorf("items = %d, want 1: the item insert must be rolled back", got)
		}
	})

	t.Run("the safety net on the items insert merges the duplicate", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, link("https://example.com/a"), keepNote)
		sequence := store.SequenceForTest(t, st)
		in := withClientID(link("https://example.com/a"), testClientID)
		in.IsPrivate = true
		merged, created, err := store.CaptureSkippingLookupsForTest(t, st, user, in, appendNote("to test", nil))
		if err != nil {
			t.Fatalf("capture of a taken normalised URL: %v, want a merge", err)
		}
		if created || merged.ID != first.ID || merged.UserNote == nil || *merged.UserNote != "to test" || !merged.IsPrivate {
			t.Errorf("capture of a taken normalised URL = %+v (created %v), want the item %s with the note, private", merged, created, first.ID)
		}
		if got := store.SequenceForTest(t, st); got != sequence+1 {
			t.Errorf("sequence = %d, want %d: one write, the failed insert rolled back", got, sequence+1)
		}
		replay, created := capture(t, st, user, withClientID(link("https://example.com/b"), testClientID), keepNote)
		if created || replay.ID != first.ID {
			t.Errorf("replay = %s (created %v), want the item %s: the merge records its client_id", replay.ID, created, first.ID)
		}
	})

	t.Run("an item of another user is not found", func(t *testing.T) {
		st := storetest.Open(t)
		a, b := newUser(t, st, "a"), newUser(t, st, "b")
		item, _ := capture(t, st, a, link("https://example.com/a"), keepNote)
		if _, err := st.Item(ctx, b, item.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Item of another user: %v, want ErrNotFound", err)
		}
		if _, err := st.SetPrivate(ctx, b, item.ID, true); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SetPrivate of another user: %v, want ErrNotFound", err)
		}
	})
}

func TestSetPrivate(t *testing.T) {
	ctx := context.Background()

	t.Run("sets the flag, keeps the status and advances the sequence only on change", func(t *testing.T) {
		st, clock := openWithClock(t)
		user := newUser(t, st, "owner")
		item, _ := capture(t, st, user, link("https://example.com/a"), keepNote)
		clock.Advance(time.Minute)
		private, err := st.SetPrivate(ctx, user, item.ID, true)
		if err != nil {
			t.Fatalf("SetPrivate: %v", err)
		}
		if !private.IsPrivate || private.Status != item.Status || private.ChangeSeq <= item.ChangeSeq || !private.UpdatedAt.Equal(clock.Now()) {
			t.Errorf("after SetPrivate(true) = %+v, want private, same status, new change_seq and updated_at", private)
		}
		sequence := store.SequenceForTest(t, st)
		again, err := st.SetPrivate(ctx, user, item.ID, true)
		if err != nil {
			t.Fatalf("SetPrivate again: %v", err)
		}
		if again.ChangeSeq != private.ChangeSeq || store.SequenceForTest(t, st) != sequence {
			t.Errorf("SetPrivate without change wrote: change_seq %d, want %d", again.ChangeSeq, private.ChangeSeq)
		}
		public, err := st.SetPrivate(ctx, user, item.ID, false)
		if err != nil || public.IsPrivate {
			t.Errorf("SetPrivate(false) = %+v, %v; want the item public", public, err)
		}
	})
}

func TestMigrationCaptureIDs(t *testing.T) {
	t.Run("a client_id captured before the migration is still a replay", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "hippo.db")
		userID, itemID := store.SeedVersion2ForTest(t, path, testClientID)
		st, err := store.Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open (migrate to the latest version): %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })

		replay, created := capture(t, st, userID, withClientID(link("https://example.com/b"), testClientID), appendNote("other", nil))
		if created || replay.ID != itemID {
			t.Errorf("replay after migration = %s (created %v), want the item %s", replay.ID, created, itemID)
		}
		if replay.UserNote != nil || *replay.URL != "https://example.com/a" {
			t.Errorf("replay = %+v, want the item unchanged", replay)
		}
		if got := store.ItemCountForTest(t, st, userID); got != 1 {
			t.Errorf("items = %d, want 1", got)
		}
		if store.NullColumnsForTest(t, st, userID, itemID)["normalized_url"] != true {
			t.Error("normalized_url of an item captured before the migration is not NULL")
		}
	})
}
