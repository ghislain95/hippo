package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

const testClientID = "01JB8Z6Q4M1X2Y3Z4A5B6C7D8E"

func ptr[T any](v T) *T { return &v }

func newUser(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	id, err := st.CreateUser(context.Background(), name, "hash-"+name)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return id
}

func link(url string) store.NewItem {
	return store.NewItem{Kind: "link", Source: "share", Status: "queued", Title: "example.com", URL: ptr(url)}
}

func capture(t *testing.T, st *store.Store, userID string, in store.NewItem) (store.Item, bool) {
	t.Helper()
	item, created, err := st.CaptureItem(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("CaptureItem: %v", err)
	}
	return item, created
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
		first, created := capture(t, st, user, link("https://example.com/a"))
		if !created {
			t.Fatal("first capture: created = false, want true")
		}
		if first.ChangeSeq <= before {
			t.Errorf("change_seq = %d, want above %d", first.ChangeSeq, before)
		}
		second, _ := capture(t, st, user, link("https://example.com/b"))
		if second.ChangeSeq <= first.ChangeSeq {
			t.Errorf("second change_seq = %d, want above %d", second.ChangeSeq, first.ChangeSeq)
		}
		if got := store.SequenceForTest(t, st); got != second.ChangeSeq {
			t.Errorf("sequence = %d, want the last write's %d", got, second.ChangeSeq)
		}
	})

	t.Run("the stored item is read back with its defaults", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		in := link("https://example.com/a")
		in.UserNote, in.IsPrivate, in.Source = ptr("for the trip"), true, "manual"
		item, _ := capture(t, st, user, in)
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
		if item.CreatedAt.IsZero() || !item.UpdatedAt.Equal(item.CreatedAt) || item.CreatedAt.Location().String() != "UTC" {
			t.Errorf("created_at, updated_at = %v, %v; want the same UTC date", item.CreatedAt, item.UpdatedAt)
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
		in := link("https://example.com/a")
		in.ClientID = ptr(testClientID)
		first, _ := capture(t, st, user, in)
		sequence := store.SequenceForTest(t, st)

		other := link("https://example.com/b")
		other.ClientID, other.UserNote = ptr(testClientID), ptr("other")
		replay, created := capture(t, st, user, other)
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

	t.Run("the same client_id for another user creates a second item", func(t *testing.T) {
		st := storetest.Open(t)
		a, b := newUser(t, st, "a"), newUser(t, st, "b")
		in := link("https://example.com/a")
		in.ClientID = ptr(testClientID)
		first, _ := capture(t, st, a, in)
		second, created := capture(t, st, b, in)
		if !created || second.ID == first.ID || second.UserID != b {
			t.Errorf("other user's capture = %+v (created %v), want a new item of user b", second, created)
		}
	})

	t.Run("two captures without client_id create two items", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		first, _ := capture(t, st, user, link("https://example.com/a"))
		second, created := capture(t, st, user, link("https://example.com/a"))
		if !created || second.ID == first.ID {
			t.Errorf("second capture = %s (created %v), want a new item", second.ID, created)
		}
	})

	t.Run("absent values are stored as NULL", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		item, _ := capture(t, st, user, link("https://example.com/a"))
		nulls := store.NullColumnsForTest(t, st, user, item.ID)
		for _, column := range []string{"client_id", "normalized_url", "user_note", "site_name", "extraction_mode", "content_text", "fiche_json", "deleted_at"} {
			if !nulls[column] {
				t.Errorf("%s is not NULL", column)
			}
		}
		if nulls["url"] {
			t.Error("url is NULL, want the captured URL")
		}
	})

	t.Run("a unique violation on client_id is answered as a replay", func(t *testing.T) {
		st := storetest.Open(t)
		user := newUser(t, st, "owner")
		in := link("https://example.com/a")
		in.ClientID = ptr(testClientID)
		first, _ := capture(t, st, user, in)
		sequence := store.SequenceForTest(t, st)
		replay, created, err := store.CaptureSkippingLookupForTest(t, st, user, in)
		if err != nil {
			t.Fatalf("capture of a taken client_id: %v, want a replay", err)
		}
		if created || replay.ID != first.ID {
			t.Errorf("capture of a taken client_id = %s (created %v), want the replay of %s", replay.ID, created, first.ID)
		}
		if got := store.SequenceForTest(t, st); got != sequence {
			t.Errorf("sequence = %d, want %d: the failed insert must not advance it", got, sequence)
		}
	})

	t.Run("an item of another user is not found", func(t *testing.T) {
		st := storetest.Open(t)
		a, b := newUser(t, st, "a"), newUser(t, st, "b")
		item, _ := capture(t, st, a, link("https://example.com/a"))
		if _, err := st.Item(ctx, b, item.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Item of another user: %v, want ErrNotFound", err)
		}
	})
}
