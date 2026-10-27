package capture_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/assetfs"
	"github.com/ghislain95/hippo/backend/internal/capture"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/store"
	"github.com/ghislain95/hippo/backend/internal/store/storetest"
)

func TestDetectImage(t *testing.T) {
	jpegFile, pngFile := imagetest.JPEG(32, 16), imagetest.PNG(640, 480)
	cases := []struct {
		name          string
		content       []byte
		mimeType      string // "" when refused
		width, height int64  // 0 when null
	}{
		{name: "PNG with its dimensions", content: pngFile, mimeType: "image/png", width: 640, height: 480},
		{name: "JPEG with its dimensions", content: jpegFile, mimeType: "image/jpeg", width: 32, height: 16},
		{name: "WebP lossy", content: imagetest.WebP("VP8 "), mimeType: "image/webp"},
		{name: "WebP lossless", content: imagetest.WebP("VP8L"), mimeType: "image/webp"},
		{name: "WebP extended", content: imagetest.WebP("VP8X"), mimeType: "image/webp"},
		{name: "HEIC brand heic", content: imagetest.HEIC("heic", "mif1", "heic"), mimeType: "image/heic"},
		{name: "HEIC brand heix", content: imagetest.HEIC("heix"), mimeType: "image/heic"},
		{name: "HEIC brand heim", content: imagetest.HEIC("heim"), mimeType: "image/heic"},
		{name: "HEIC brand heis", content: imagetest.HEIC("heis"), mimeType: "image/heic"},
		{name: "mif1 compatible with heic", content: imagetest.HEIC("mif1", "mif1", "heic"), mimeType: "image/heic"},
		{name: "msf1 compatible with heix", content: imagetest.HEIC("msf1", "heix"), mimeType: "image/heic"},
		{name: "HEIC with an ftyp box at the 4096-byte cap", content: imagetest.HEICWithFtypSize("heic", 4096), mimeType: "image/heic"},
		{name: "HEIC with an ftyp box of 4097 bytes, over the cap", content: imagetest.HEICWithFtypSize("heic", 4097)},

		{name: "AVIF", content: imagetest.AVIF()},
		{name: "mif1 compatible with avif only", content: imagetest.HEIC("mif1", "mif1", "avif")},
		{name: "mif1 without compatible brand", content: imagetest.HEIC("mif1")},
		{name: "MP4 video", content: imagetest.HEIC("isom", "isom", "mp41")},
		{name: "truncated ftyp box", content: imagetest.Ftyp("mif1", "mif1", "heic")[:20]},
		{name: "ftyp box shorter than its header", content: append([]byte{0, 0, 0, 8}, "ftypheic\x00\x00\x00\x00"...)},
		{name: "ftyp box larger than the file", content: append([]byte{0, 0, 1, 0}, "ftypheic\x00\x00\x00\x00heic"...)},
		{name: "WebP with another chunk", content: imagetest.WebP("ALPH")},
		{name: "RIFF that is not WebP", content: append([]byte("RIFF\x10\x00\x00\x00WAVEfmt "), make([]byte, 16)...)},
		{name: "truncated JPEG header", content: jpegFile[:20]},
		{name: "truncated PNG header", content: pngFile[:20]},
		{name: "PNG signature alone", content: pngFile[:8]},
		{name: "GIF", content: imagetest.GIF()},
		{name: "SVG", content: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)},
		{name: "text", content: []byte("just some text, not an image\n")},
		{name: "empty file", content: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mimeType, width, height, err := capture.DetectImage(bytes.NewReader(tc.content), int64(len(tc.content)))
			if tc.mimeType == "" {
				if !errors.Is(err, capture.ErrUnsupportedImage) {
					t.Errorf("err = %v (type %q), want ErrUnsupportedImage", err, mimeType)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want %s", err, tc.mimeType)
			}
			if mimeType != tc.mimeType {
				t.Errorf("type = %q, want %q", mimeType, tc.mimeType)
			}
			assertDimension(t, "width", width, tc.width)
			assertDimension(t, "height", height, tc.height)
		})
	}
}

func assertDimension(t *testing.T, name string, got *int64, want int64) {
	t.Helper()
	switch {
	case want == 0 && got != nil:
		t.Errorf("%s = %d, want null", name, *got)
	case want != 0 && (got == nil || *got != want):
		t.Errorf("%s = %v, want %d", name, got, want)
	}
}

func TestImageTitle(t *testing.T) {
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatalf("LoadParis: %v", err)
	}
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		note *string
		now  time.Time
		want string
	}{
		{name: "first line of the note", note: ptr("Tarte aux pommes\nde mamie"), now: at, want: "Tarte aux pommes"},
		{name: "first non-empty line", note: ptr("  \n\n  Idea: a shelf  \nmore"), now: at, want: "Idea: a shelf"},
		{name: "long line cut", note: ptr(strings.Repeat("é", 100)), now: at, want: strings.Repeat("é", 80) + "…"},
		{name: "dated without note", now: at, want: "Image du 27 octobre 2026"},
		{name: "day in Paris", now: time.Date(2026, 10, 27, 23, 30, 0, 0, time.UTC), want: "Image du 28 octobre 2026"},
		{name: "first day of the month", now: time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC), want: "Image du 1er novembre 2026"},
		{name: "accented month", now: time.Date(2026, 2, 14, 12, 0, 0, 0, time.UTC), want: "Image du 14 février 2026"},
		{name: "august", now: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), want: "Image du 2 août 2026"},
		{name: "december", now: time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC), want: "Image du 1er décembre 2026"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := capture.ImageTitle(tc.note, tc.now, paris); got != tc.want {
				t.Errorf("title = %q, want %q", got, tc.want)
			}
		})
	}
}

// imageService is a capture service on a real database and a real assets directory.
type imageService struct {
	service *capture.Service
	store   *store.Store
	assets  *assetfs.Dir
	dir     string
	dbPath  string
	userID  string
	logs    *bytes.Buffer
}

func newImageService(t *testing.T) imageService {
	t.Helper()
	st, dbPath := storetest.OpenWithClock(t, func() time.Time { return time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC) })
	userID, err := st.CreateUser(context.Background(), "owner", "hash")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	paris, err := capture.LoadParis()
	if err != nil {
		t.Fatalf("LoadParis: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "assets")
	assets, err := assetfs.Open(dir)
	if err != nil {
		t.Fatalf("assetfs.Open: %v", err)
	}
	t.Cleanup(func() { _ = assets.Close() })
	logs := &bytes.Buffer{}
	service := capture.NewService(st, paris, slog.New(slog.NewJSONHandler(logs, nil)))
	return imageService{service: service, store: st, assets: assets, dir: dir, dbPath: dbPath, userID: userID, logs: logs}
}

func (s imageService) upload(t *testing.T, content []byte) *assetfs.Upload {
	t.Helper()
	up, err := s.assets.CreateTemp()
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := io.Copy(up, bytes.NewReader(content)); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	return up
}

func (s imageService) files(t *testing.T) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(s.dir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			rel, relErr := filepath.Rel(s.dir, path)
			found = append(found, filepath.ToSlash(rel))
			return relErr
		}
		return err
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return found
}

func TestCaptureImage(t *testing.T) {
	ctx := context.Background()

	t.Run("creates an image item with its asset and keeps the file", func(t *testing.T) {
		s := newImageService(t)
		content := imagetest.PNG(640, 480)
		result, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{
			Image: s.upload(t, content), UserNote: ptr("  recipe for Sunday \n"), IsPrivate: ptr(true), Source: ptr("manual"),
		})
		if err != nil || !result.Created {
			t.Fatalf("CaptureImage = %v, %v; want a new item", result.Created, err)
		}
		item := result.Item
		if item.Kind != "image" || item.Status != "queued" || item.URL != nil || item.Source != "manual" || !item.IsPrivate {
			t.Errorf("item = %+v, want a private queued image from manual", item)
		}
		if item.UserNote == nil || *item.UserNote != "recipe for Sunday" || item.Title != "recipe for Sunday" {
			t.Errorf("note %v title %q, want the trimmed note as both", item.UserNote, item.Title)
		}
		if len(item.Assets) != 1 {
			t.Fatalf("assets = %+v, want one", item.Assets)
		}
		asset := item.Assets[0]
		wantPath := s.userID + "/" + asset.ID + ".png"
		if asset.Path != wantPath || asset.MimeType != "image/png" || asset.SizeBytes != int64(len(content)) {
			t.Errorf("asset = %+v, want %s, image/png, %d bytes", asset, wantPath, len(content))
		}
		if got := s.files(t); len(got) != 1 || got[0] != wantPath {
			t.Errorf("files = %v, want only %s", got, wantPath)
		}
	})

	t.Run("dated title from the store's clock without note", func(t *testing.T) {
		s := newImageService(t)
		result, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{Image: s.upload(t, imagetest.JPEG(8, 8)), UserNote: ptr("  \n ")})
		if err != nil {
			t.Fatalf("CaptureImage: %v", err)
		}
		if result.Item.Title != "Image du 27 octobre 2026" || result.Item.UserNote != nil {
			t.Errorf("title %q note %v, want the dated title and no note", result.Item.Title, result.Item.UserNote)
		}
		if ext := filepath.Ext(result.Item.Assets[0].Path); ext != ".jpg" {
			t.Errorf("extension = %q, want .jpg", ext)
		}
	})

	refusals := []struct {
		name    string
		req     capture.ImageRequest
		content []byte
		wantErr error
		field   string
	}{
		{name: "malformed client_id", req: capture.ImageRequest{ClientID: ptr("not-a-ulid")}, content: imagetest.PNG(4, 4), field: "client_id"},
		{name: "unknown source", req: capture.ImageRequest{Source: ptr("rss")}, content: imagetest.PNG(4, 4), field: "source"},
		{name: "client_id before the format", req: capture.ImageRequest{ClientID: ptr("bad")}, content: imagetest.GIF(), field: "client_id"},
		{name: "unsupported format", content: imagetest.GIF(), wantErr: capture.ErrUnsupportedImage},
		{name: "truncated PNG", content: imagetest.PNG(4, 4)[:30], wantErr: capture.ErrUnsupportedImage},
	}
	for _, tc := range refusals {
		t.Run("refuses and removes the file: "+tc.name, func(t *testing.T) {
			s := newImageService(t)
			req := tc.req
			req.Image = s.upload(t, tc.content)
			_, err := s.service.CaptureImage(ctx, s.userID, req)
			var validation *capture.ValidationError
			switch {
			case tc.field != "":
				if !errors.As(err, &validation) || validation.Field != tc.field {
					t.Errorf("err = %v, want a ValidationError on %s", err, tc.field)
				}
			case !errors.Is(err, tc.wantErr):
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if got := s.files(t); len(got) != 0 {
				t.Errorf("files = %v, want none", got)
			}
			if got := storetest.ItemCount(t, s.dbPath, s.userID); got != 0 {
				t.Errorf("items = %d, want 0", got)
			}
		})
	}

	t.Run("refuses and removes an image over 20 MB", func(t *testing.T) {
		s := newImageService(t)
		_, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{
			Image: s.upload(t, imagetest.PNGOfSize(4, 4, capture.MaxImageSize+1)),
		})
		if !errors.Is(err, capture.ErrImageTooLarge) {
			t.Errorf("err = %v, want ErrImageTooLarge", err)
		}
		if got := s.files(t); len(got) != 0 {
			t.Errorf("files = %v, want none", got)
		}
	})

	t.Run("a replay keeps the first file only", func(t *testing.T) {
		s := newImageService(t)
		first, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{Image: s.upload(t, imagetest.PNG(4, 4)), ClientID: ptr(testClientID)})
		if err != nil {
			t.Fatalf("CaptureImage: %v", err)
		}
		replay, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{Image: s.upload(t, imagetest.PNG(4, 4)), ClientID: ptr(testClientID)})
		if err != nil || replay.Created || replay.Item.ID != first.Item.ID {
			t.Fatalf("replay = %s, %v, %v; want the first item %s", replay.Item.ID, replay.Created, err, first.Item.ID)
		}
		if got := s.files(t); len(got) != 1 || got[0] != first.Item.Assets[0].Path {
			t.Errorf("files = %v, want only %s", got, first.Item.Assets[0].Path)
		}
	})

	t.Run("a replay whose file cannot be removed still answers with the existing item", func(t *testing.T) {
		s := newImageService(t)
		first, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{Image: s.upload(t, imagetest.PNG(4, 4)), ClientID: ptr(testClientID)})
		if err != nil {
			t.Fatalf("CaptureImage: %v", err)
		}
		stuck := &unremovable{Upload: s.upload(t, imagetest.PNG(4, 4))}
		replay, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{Image: stuck, ClientID: ptr(testClientID)})
		if err != nil || replay.Created || replay.Item.ID != first.Item.ID {
			t.Fatalf("replay = %s, %v, %v; want the first item %s and no error", replay.Item.ID, replay.Created, err, first.Item.ID)
		}
		if stuck.committedAs == "" {
			t.Fatal("the replayed file was never committed")
		}
		logs := s.logs.String()
		assetID := strings.TrimSuffix(filepath.Base(stuck.committedAs), ".png")
		if !strings.Contains(logs, `"asset_id":"`+assetID+`"`) || !strings.Contains(logs, `"level":"WARN"`) {
			t.Errorf("logs do not report the file of asset %s:\n%s", assetID, logs)
		}
		if strings.Contains(logs, stuck.committedAs) || strings.Contains(logs, ".png") || strings.Contains(logs, s.dir) {
			t.Errorf("logs name a path:\n%s", logs)
		}
	})

	t.Run("a refusal whose file cannot be removed reports both", func(t *testing.T) {
		s := newImageService(t)
		_, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{Image: &unremovable{Upload: s.upload(t, imagetest.GIF())}})
		if !errors.Is(err, capture.ErrUnsupportedImage) || !errors.Is(err, errDiskFailure) {
			t.Errorf("err = %v, want ErrUnsupportedImage joined with the removal failure", err)
		}
		logs := s.logs.String()
		if !strings.Contains(logs, "remove the file of an image capture not stored") || !strings.Contains(logs, `"level":"WARN"`) {
			t.Errorf("logs do not report the removal failure:\n%s", logs)
		}
		if strings.Contains(logs, ".tmp-") || strings.Contains(logs, s.dir) {
			t.Errorf("logs name a path:\n%s", logs)
		}
	})

	t.Run("removes the file when the store fails", func(t *testing.T) {
		s := newImageService(t)
		if err := s.store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if _, err := s.service.CaptureImage(ctx, s.userID, capture.ImageRequest{Image: s.upload(t, imagetest.PNG(4, 4))}); err == nil {
			t.Fatal("CaptureImage on a closed store succeeded")
		}
		if got := s.files(t); len(got) != 0 {
			t.Errorf("files = %v, want none", got)
		}
	})
}

// errDiskFailure stands for a file system that refuses a removal.
var errDiskFailure = errors.New("disk failure")

// unremovable is a real upload whose removal fails, as on a failing disk: the boundary with the
// file system, not a fake of the store. It records where Commit put the file.
type unremovable struct {
	*assetfs.Upload
	committedAs string
}

func (u *unremovable) Commit(rel string) error {
	u.committedAs = rel
	return u.Upload.Commit(rel)
}

func (u *unremovable) Remove() error { return errDiskFailure }
