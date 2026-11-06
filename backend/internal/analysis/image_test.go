package analysis_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/analysis"
	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
	"github.com/ghislain95/hippo/backend/internal/store"
)

// imagesDir holds the HEIC and WebP fixtures (backend/testdata/images/README.md).
const imagesDir = "../../testdata/images"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(imagesDir, name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// onlyImage builds the batch of one image item stored as content and returns its request.
func onlyImage(t *testing.T, e *env, content []byte, mime string) (request, string, string) {
	t.Helper()
	userID := e.user(t, "owner")
	id, file := e.image(t, userID, content, mime, nil)
	_, parsed := e.build(t, userID)
	return parsed.of(t, id), id, file
}

// singleJPEG decodes the single image of r.
func singleJPEG(t *testing.T, r request) image.Image {
	t.Helper()
	images := r.images(t)
	if len(images) != 1 {
		t.Fatalf("%d image blocks, want 1", len(images))
	}
	img, err := jpeg.Decode(bytes.NewReader(images[0]))
	if err != nil {
		t.Fatalf("the image block is not a JPEG: %v", err)
	}
	return img
}

// near tells whether the colour at (x, y) of img is within tolerance of want on each channel.
func near(img image.Image, x, y int, want color.RGBA, tolerance int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	diff := func(got uint32, want uint8) bool {
		d := int(got>>8) - int(want)
		return d <= tolerance && d >= -tolerance
	}
	return diff(r, want.R) && diff(g, want.G) && diff(b, want.B)
}

// withoutImage checks that r has its <element> block and no image block.
func withoutImage(t *testing.T, r request) {
	t.Helper()
	content := r.content(t)
	if len(content) != 1 || content[0].Type != "text" || content[0].Text != "<element>\nforme: image\nextraction: aucune\n</element>" {
		t.Errorf("message = %+v, want the <element> block only", content)
	}
}

func TestImageCopy(t *testing.T) {
	t.Run("A large photo is scaled down", func(t *testing.T) {
		e := newEnv(t)
		r, _, _ := onlyImage(t, e, imagetest.QuadrantJPEG(4000, 3000), "image/jpeg")
		if b := singleJPEG(t, r).Bounds(); b.Dx() != 1568 || b.Dy() != 1176 {
			t.Errorf("copy of %d×%d, want 1568×1176", b.Dx(), b.Dy())
		}
	})

	t.Run("A small image is not scaled up", func(t *testing.T) {
		e := newEnv(t)
		r, _, _ := onlyImage(t, e, imagetest.PNG(800, 600), "image/png")
		if b := singleJPEG(t, r).Bounds(); b.Dx() != 800 || b.Dy() != 600 {
			t.Errorf("copy of %d×%d, want 800×600", b.Dx(), b.Dy())
		}
	})

	t.Run("A rotated JPEG is turned upright", func(t *testing.T) {
		e := newEnv(t)
		stored := imagetest.WithEXIF(imagetest.QuadrantJPEG(400, 200), imagetest.EXIF(binary.LittleEndian, 6))
		r, _, _ := onlyImage(t, e, stored, "image/jpeg")
		img := singleJPEG(t, r)
		if b := img.Bounds(); b.Dx() != 200 || b.Dy() != 400 {
			t.Fatalf("copy of %d×%d, want 200×400", b.Dx(), b.Dy())
		}
		if !near(img, 5, 5, imagetest.BottomLeft, 24) {
			t.Errorf("top-left corner is %v, want the stored bottom-left colour %v", img.At(5, 5), imagetest.BottomLeft)
		}
	})

	t.Run("The copy carries no metadata", func(t *testing.T) {
		e := newEnv(t)
		stored := imagetest.WithEXIF(imagetest.QuadrantJPEG(640, 480), imagetest.EXIF(binary.BigEndian, 1))
		r, _, file := onlyImage(t, e, stored, "image/jpeg")
		copied := r.images(t)
		if len(copied) != 1 {
			t.Fatalf("%d image blocks, want 1", len(copied))
		}
		for _, marker := range []byte{0xE1, 0xFE} { // APP1 (EXIF, XMP), COM
			if segmentPresent(copied[0], marker) {
				t.Errorf("the copy has a segment 0xFF%02X", marker)
			}
		}
		for _, value := range [][]byte{[]byte(imagetest.FictionalMake), []byte(imagetest.FictionalModel), []byte("Exif")} {
			if bytes.Contains(copied[0], value) {
				t.Errorf("the copy holds %q", value)
			}
		}
		after, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, stored) {
			t.Error("the stored file changed")
		}
	})

	t.Run("A HEIC image is converted", func(t *testing.T) {
		e := newEnv(t)
		r, _, _ := onlyImage(t, e, fixture(t, "rotated.heic"), "image/heic")
		img := singleJPEG(t, r)
		// rotated.heic: 640×480 stored, turned upright to 480×640 with blue at the top left.
		if b := img.Bounds(); b.Dx() != 480 || b.Dy() != 640 {
			t.Fatalf("copy of %d×%d, want 480×640", b.Dx(), b.Dy())
		}
		if !near(img, 10, 10, imagetest.BottomLeft, 48) {
			t.Errorf("top-left corner is %v, want blue: the photo is not upright", img.At(10, 10))
		}
	})

	t.Run("A WebP image is converted", func(t *testing.T) {
		e := newEnv(t)
		r, _, _ := onlyImage(t, e, fixture(t, "quadrants.webp"), "image/webp")
		if b := singleJPEG(t, r).Bounds(); b.Dx() != 640 || b.Dy() != 480 {
			t.Errorf("copy of %d×%d, want 640×480", b.Dx(), b.Dy())
		}
	})

	t.Run("Too many pixels", func(t *testing.T) {
		e := newEnv(t)
		r, id, _ := onlyImage(t, e, imagetest.JPEGDeclaring(10000, 6000), "image/jpeg")
		withoutImage(t, r)
		if !e.hasLog(t, map[string]string{"item_id": id, "reason": "too_many_pixels"}) {
			t.Errorf("no log line with the item id and too_many_pixels:\n%s", e.logs)
		}
	})

	t.Run("An undecodable image", func(t *testing.T) {
		e := newEnv(t)
		r, id, _ := onlyImage(t, e, imagetest.JPEGCut(64, 64), "image/jpeg")
		withoutImage(t, r)
		if !e.hasLog(t, map[string]string{"item_id": id, "reason": "undecodable"}) {
			t.Errorf("no log line with the item id and undecodable:\n%s", e.logs)
		}
	})

	t.Run("A copy that fails does not stop the batch", func(t *testing.T) {
		e := newEnv(t)
		e.limits.Memory = 16 << 20 // the large photo exceeds it; the small one does not
		userID := e.user(t, "owner")
		first, _ := e.image(t, userID, imagetest.QuadrantJPEG(4000, 3000), "image/jpeg", nil)
		second, _ := e.image(t, userID, imagetest.JPEG(64, 48), "image/jpeg", nil)
		_, parsed := e.build(t, userID)
		if len(parsed.Requests) != 2 {
			t.Fatalf("%d requests, want 2", len(parsed.Requests))
		}
		withoutImage(t, parsed.of(t, first))
		if b := singleJPEG(t, parsed.of(t, second)).Bounds(); b.Dx() != 64 || b.Dy() != 48 {
			t.Errorf("second copy of %d×%d, want 64×48", b.Dx(), b.Dy())
		}
		if !e.hasLog(t, map[string]string{"item_id": first, "reason": "too_large"}) {
			t.Errorf("no log line with the first item and too_large:\n%s", e.logs)
		}
	})

	t.Run("a stored file that is missing sends the item without its image", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		id, file := e.image(t, userID, imagetest.JPEG(64, 48), "image/jpeg", nil)
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		_, parsed := e.build(t, userID)
		withoutImage(t, parsed.of(t, id))
		if !e.hasLog(t, map[string]string{"item_id": id, "reason": "asset_missing"}) {
			t.Errorf("no log line with the item id and asset_missing:\n%s", e.logs)
		}
	})

	t.Run("a copier that cannot start stops the building", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		e.image(t, userID, imagetest.JPEG(64, 48), "image/jpeg", nil)
		queue, err := e.st.AnalysisQueue(context.Background(), userID, 1)
		if err != nil {
			t.Fatal(err)
		}
		reqs := e.requestsWith(t, userID, 0, []string{filepath.Join(t.TempDir(), "missing-binary"), "copy-image"})
		added, err := reqs.Add(context.Background(), queue[0])
		if added || !errors.Is(err, analysis.ErrCopierUnavailable) {
			t.Errorf("Add = %v, %v; want ErrCopierUnavailable", added, err)
		}
		if len(reqs.IDs()) != 0 {
			t.Error("a request was kept")
		}
	})
}

func TestPrivateItem(t *testing.T) {
	t.Run("A private item reaching the request building stops the batch", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		public := e.note(t, userID, "publique")
		queue, err := e.st.AnalysisQueue(context.Background(), userID, 1)
		if err != nil {
			t.Fatal(err)
		}
		reqs := e.requests(t, userID, 0)
		if added, err := reqs.Add(context.Background(), queue[0]); err != nil || !added {
			t.Fatalf("Add = %v, %v", added, err)
		}
		private := store.AnalysisQueueItem{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7D90", Kind: "note", Title: "Titre privé", UserNote: ptr("Note privée"),
			IsPrivate: true, Assets: []store.Asset{},
		}
		added, err := reqs.Add(context.Background(), private)
		if added || !errors.Is(err, analysis.ErrPrivateItem) {
			t.Fatalf("Add(private) = %v, %v; want ErrPrivateItem", added, err)
		}
		raw, parsed := assemble(t, reqs, reqs.IDs())
		if len(parsed.Requests) != 1 || parsed.Requests[0].CustomID != public {
			t.Errorf("body holds %d requests, want the public one only", len(parsed.Requests))
		}
		for _, value := range []string{private.ID, "Titre privé", "Note privée"} {
			if bytes.Contains(raw, []byte(value)) {
				t.Errorf("the body holds %q of the private item", value)
			}
		}
		if bytes.Contains([]byte(err.Error()), []byte("privée")) {
			t.Error("the error quotes the item")
		}
	})
}

func TestPrivateLinkIsRefusedBeforeAnyRead(t *testing.T) {
	t.Run("a private link is refused before its content is read", func(t *testing.T) {
		e := newEnv(t)
		userID := e.user(t, "owner")
		cfg := e.config(t, userID)
		cfg.Contents = failingContents{t: t}
		reqs := e.open(t, cfg)
		private := store.AnalysisQueueItem{
			ID: "01JB8Z6Q4M1X2Y3Z4A5B6C7D90", Kind: "link", URL: ptr("https://example.com/private"),
			Title: "Titre privé", IsPrivate: true, Assets: []store.Asset{},
		}
		added, err := reqs.Add(context.Background(), private)
		if added || !errors.Is(err, analysis.ErrPrivateItem) {
			t.Fatalf("Add(private link) = %v, %v; want ErrPrivateItem", added, err)
		}
		if size, err := analysis.SegmentFileSize(reqs); err != nil || size != 0 {
			t.Errorf("segment file of %d bytes (%v), want empty", size, err)
		}
	})
}

// failingContents fails the test when the content of an item is read.
type failingContents struct{ t *testing.T }

func (f failingContents) ItemContent(context.Context, string, string) (*string, error) {
	f.t.Error("the content of the item was read")
	return nil, errors.New("read refused by the test")
}

// segmentPresent tells whether the JPEG jpg has a segment of marker before its scan.
func segmentPresent(jpg []byte, marker byte) bool {
	for i := 2; i+4 <= len(jpg) && jpg[i] == 0xFF; {
		if jpg[i+1] == marker {
			return true
		}
		if jpg[i+1] == 0xDA { // start of scan: no more marker segments of interest
			return false
		}
		i += 2 + int(binary.BigEndian.Uint16(jpg[i+2:]))
	}
	return false
}
