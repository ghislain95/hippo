package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/store"
)

// image is an image capture with one asset, whose id comes from the store as in capture.
func image(t *testing.T, st *store.Store, userID string, clientID *string) store.NewItem {
	t.Helper()
	id, err := st.NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	return store.NewItem{
		Kind: "image", Source: "share", Status: "queued", ClientID: clientID,
		TitleAt: func(now time.Time) string { return "Image du " + now.Format(time.DateOnly) },
		Asset: &store.NewAsset{
			ID: id, Path: userID + "/" + id + ".png", MimeType: "image/png",
			Width: ptr[int64](640), Height: ptr[int64](480), SizeBytes: 1234,
		},
	}
}

func TestMigrationAssets(t *testing.T) {
	t.Run("refuses an asset whose user is not the user of its item", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		item, _, err := st.CaptureItem(context.Background(), owner, link("https://example.com/a"), nil)
		if err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		if err := store.InsertAssetRowForTest(t, st, "01JB8Z6Q4M1X2Y3Z4A5B6C7D90", item.ID, other); err == nil {
			t.Fatal("an asset of another user than its item's was stored")
		}
		if err := store.InsertAssetRowForTest(t, st, "01JB8Z6Q4M1X2Y3Z4A5B6C7D91", item.ID, owner); err != nil {
			t.Fatalf("an asset of the item's user was refused: %v", err)
		}
	})

	t.Run("refuses an unknown type", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		in := image(t, st, owner, nil)
		in.Asset.MimeType = "image/gif"
		if _, _, err := st.CaptureItem(context.Background(), owner, in, nil); err == nil {
			t.Fatal("an image/gif asset was stored")
		}
		if got := store.ItemCountForTest(t, st, owner); got != 0 {
			t.Errorf("items = %d, want 0: the refused asset must roll the item back", got)
		}
	})
}

func TestAssets(t *testing.T) {
	ctx := context.Background()

	t.Run("inserts the asset with its item and reads it back", func(t *testing.T) {
		st, clock := openWithClock(t)
		owner := newUser(t, st, "owner")
		in := image(t, st, owner, nil)
		before := store.SequenceForTest(t, st)
		item, created, err := st.CaptureItem(ctx, owner, in, nil)
		if err != nil || !created {
			t.Fatalf("CaptureItem = %v, %v; want a new item", created, err)
		}
		if item.Kind != "image" || item.Title != "Image du 2026-10-27" {
			t.Errorf("item kind %q title %q, want image and the title of the store's clock", item.Kind, item.Title)
		}
		if got := store.SequenceForTest(t, st); got != before+1 {
			t.Errorf("sequence advanced by %d, want 1 for the item and its asset", got-before)
		}
		if len(item.Assets) != 1 {
			t.Fatalf("assets = %+v, want one", item.Assets)
		}
		asset := item.Assets[0]
		want := store.Asset{
			ID: in.Asset.ID, ItemID: item.ID, UserID: owner, Path: in.Asset.Path, MimeType: "image/png",
			Width: ptr[int64](640), Height: ptr[int64](480), SizeBytes: 1234, CreatedAt: clock.Now(),
		}
		assertAsset(t, asset, want)

		byID, err := st.Asset(ctx, owner, asset.ID)
		if err != nil {
			t.Fatalf("Asset: %v", err)
		}
		assertAsset(t, byID, want)
		reread, err := st.Item(ctx, owner, item.ID)
		if err != nil {
			t.Fatalf("Item: %v", err)
		}
		if len(reread.Assets) != 1 || reread.Assets[0].ID != asset.ID {
			t.Errorf("item read back has assets %+v, want %s", reread.Assets, asset.ID)
		}
	})

	t.Run("an asset without dimensions stores null", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		in := image(t, st, owner, nil)
		in.Asset.MimeType, in.Asset.Width, in.Asset.Height = "image/heic", nil, nil
		item, _, err := st.CaptureItem(ctx, owner, in, nil)
		if err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		if asset := item.Assets[0]; asset.Width != nil || asset.Height != nil {
			t.Errorf("width %v height %v, want nil", asset.Width, asset.Height)
		}
	})

	t.Run("another user's asset is not found", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner, other := newUser(t, st, "owner"), newUser(t, st, "other")
		item, _, err := st.CaptureItem(ctx, owner, image(t, st, owner, nil), nil)
		if err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		if _, err := st.Asset(ctx, other, item.Assets[0].ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("Asset of another user: err = %v, want ErrNotFound", err)
		}
		if _, err := st.Asset(ctx, owner, "01JB8Z6Q4M1X2Y3Z4A5B6C7D99"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("unknown Asset: err = %v, want ErrNotFound", err)
		}
		assets, err := store.ItemAssetsForTest(t, st, other, item.ID)
		if err != nil || len(assets) != 0 {
			t.Errorf("ItemAssets of another user = %+v, %v; want none", assets, err)
		}
	})

	t.Run("lists the assets of an item, of its user only", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		item, _, err := st.CaptureItem(ctx, owner, image(t, st, owner, nil), nil)
		if err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		if _, _, err := st.CaptureItem(ctx, owner, image(t, st, owner, nil), nil); err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		assets, err := store.ItemAssetsForTest(t, st, owner, item.ID)
		if err != nil {
			t.Fatalf("ItemAssets: %v", err)
		}
		if len(assets) != 1 || assets[0].ID != item.Assets[0].ID {
			t.Errorf("ItemAssets = %+v, want only the asset of this item", assets)
		}
	})

	t.Run("a link has no asset", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		item, _, err := st.CaptureItem(ctx, owner, link("https://example.com/a"), nil)
		if err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		if item.Assets == nil || len(item.Assets) != 0 {
			t.Errorf("assets = %#v, want an empty slice", item.Assets)
		}
	})

	t.Run("a replay stores no new asset", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		first, _, err := st.CaptureItem(ctx, owner, image(t, st, owner, ptr(testClientID)), nil)
		if err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		replay, created, err := st.CaptureItem(ctx, owner, image(t, st, owner, ptr(testClientID)), nil)
		if err != nil || created {
			t.Fatalf("replay = %v, %v; want the existing item", created, err)
		}
		if len(replay.Assets) != 1 || replay.Assets[0].ID != first.Assets[0].ID {
			t.Errorf("replay assets = %+v, want the first asset %s", replay.Assets, first.Assets[0].ID)
		}
		if got := store.AssetCountForTest(t, st, owner); got != 1 {
			t.Errorf("assets = %d, want 1", got)
		}
	})

	t.Run("the safety net of a replay stores no asset", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		first, _, err := st.CaptureItem(ctx, owner, image(t, st, owner, ptr(testClientID)), nil)
		if err != nil {
			t.Fatalf("CaptureItem: %v", err)
		}
		replay, created, err := store.CaptureSkippingLookupsForTest(t, st, owner, image(t, st, owner, ptr(testClientID)), nil)
		if err != nil || created || replay.ID != first.ID {
			t.Fatalf("safety net = %s, %v, %v; want the existing item %s", replay.ID, created, err, first.ID)
		}
		if got := store.AssetCountForTest(t, st, owner); got != 1 {
			t.Errorf("assets = %d, want 1", got)
		}
	})

	t.Run("concurrent replays store one asset", func(t *testing.T) {
		st, _ := openWithClock(t)
		owner := newUser(t, st, "owner")
		const replays = 8
		inputs := make([]store.NewItem, replays)
		for i := range inputs {
			inputs[i] = image(t, st, owner, ptr(testClientID))
		}
		created, errs := make([]bool, replays), make([]error, replays)
		var wg sync.WaitGroup
		for i := range replays {
			wg.Go(func() { _, created[i], errs[i] = st.CaptureItem(ctx, owner, inputs[i], nil) })
		}
		wg.Wait()
		count := 0
		for i := range replays {
			if errs[i] != nil {
				t.Fatalf("capture %d: %v", i, errs[i])
			}
			if created[i] {
				count++
			}
		}
		if count != 1 {
			t.Errorf("%d captures created an item, want 1", count)
		}
		if got := store.AssetCountForTest(t, st, owner); got != 1 {
			t.Errorf("assets = %d, want 1", got)
		}
	})

	t.Run("new ids are distinct ULIDs", func(t *testing.T) {
		st, _ := openWithClock(t)
		first, err := st.NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		second, err := st.NewID()
		if err != nil {
			t.Fatalf("NewID: %v", err)
		}
		if len(first) != 26 || first == second {
			t.Errorf("ids %q and %q, want two different ULIDs", first, second)
		}
	})
}

func assertAsset(t *testing.T, got, want store.Asset) {
	t.Helper()
	if got.ID != want.ID || got.ItemID != want.ItemID || got.UserID != want.UserID || got.Path != want.Path ||
		got.MimeType != want.MimeType || got.SizeBytes != want.SizeBytes || !got.CreatedAt.Equal(want.CreatedAt) ||
		!equalPtr(got.Width, want.Width) || !equalPtr(got.Height, want.Height) {
		t.Errorf("asset = %+v, want %+v", got, want)
	}
}

func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
