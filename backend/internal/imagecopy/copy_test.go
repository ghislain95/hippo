package imagecopy

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"github.com/gen2brain/h265/heic"

	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
)

// copyBytes runs copyImage over file with the limits of production, and decodes the copy.
func copyBytes(t *testing.T, file []byte) (answer, []byte, image.Image) {
	t.Helper()
	var out bytes.Buffer
	ans, err := copyImage(bytes.NewReader(file), int64(len(file)), &out, defaultRequest())
	if err != nil {
		t.Fatalf("copyImage: %v", err)
	}
	if ans.Reason != ReasonOK {
		return ans, out.Bytes(), nil
	}
	img, err := jpeg.Decode(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode the copy: %v", err)
	}
	if img.Bounds().Dx() != ans.Width || img.Bounds().Dy() != ans.Height {
		t.Errorf("copy of %v, answer says %d×%d", img.Bounds(), ans.Width, ans.Height)
	}
	return ans, out.Bytes(), img
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	file, err := os.ReadFile("../../testdata/images/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func rgbaAt(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// nearish is near with the tolerance of a lossy codec on a flat area.
func nearish(t *testing.T, got, want color.RGBA) {
	t.Helper()
	diff := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	if diff(got.R, want.R) > 32 || diff(got.G, want.G) > 32 || diff(got.B, want.B) > 32 {
		t.Errorf("colour %v, want about %v", got, want)
	}
}

func TestFixtures(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
	}{
		{"rotated.heic", 640, 480},
		{"grid.heic", 2048, 1536},
		{"quadrants.webp", 640, 480},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := fixture(t, tc.name)
			if len(file) >= 200<<10 {
				t.Errorf("%d bytes, want less than 200 KB", len(file))
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(file))
			if err != nil || cfg.Width != tc.width || cfg.Height != tc.height {
				t.Errorf("config %d×%d, %v; want %d×%d", cfg.Width, cfg.Height, err, tc.width, tc.height)
			}
		})
	}

	t.Run("grid.heic is a grid", func(t *testing.T) {
		file := fixture(t, "grid.heic")
		// The primary item of a grid is of type grid: its infe names it.
		if !bytes.Contains(file, []byte("grid")) || bytes.Count(file, []byte("infe")) < 2 {
			t.Error("grid.heic holds no grid item")
		}
	})
}

func TestCopyImage(t *testing.T) {
	t.Run("a large JPEG is scaled down", func(t *testing.T) {
		ans, _, img := copyBytes(t, imagetest.QuadrantJPEG(4000, 3000))
		if ans.Reason != ReasonOK || img.Bounds() != image.Rect(0, 0, 1568, 1176) {
			t.Fatalf("answer %+v, want ok 1568×1176", ans)
		}
		nearish(t, rgbaAt(img, 5, 5), imagetest.TopLeft)
	})

	t.Run("a small PNG keeps its size", func(t *testing.T) {
		ans, _, _ := copyBytes(t, imagetest.PNG(800, 600))
		if ans.Reason != ReasonOK || ans.Width != 800 || ans.Height != 600 {
			t.Errorf("answer %+v, want ok 800×600", ans)
		}
	})

	t.Run("a JPEG is turned upright from its EXIF orientation", func(t *testing.T) {
		file := imagetest.WithEXIF(imagetest.QuadrantJPEG(400, 200), imagetest.EXIF(binary.LittleEndian, 6))
		ans, _, img := copyBytes(t, file)
		if ans.Width != 200 || ans.Height != 400 {
			t.Fatalf("answer %+v, want 200×400", ans)
		}
		nearish(t, rgbaAt(img, 2, 2), imagetest.BottomLeft)
	})

	t.Run("a HEIC is turned upright from its irot", func(t *testing.T) {
		ans, _, img := copyBytes(t, fixture(t, "rotated.heic"))
		if ans.Reason != ReasonOK || ans.Width != 480 || ans.Height != 640 {
			t.Fatalf("answer %+v, want ok 480×640", ans)
		}
		nearish(t, rgbaAt(img, 2, 2), imagetest.BottomLeft)
		nearish(t, rgbaAt(img, 477, 637), imagetest.TopRight)
	})

	t.Run("a HEIC grid is decoded and scaled down", func(t *testing.T) {
		ans, _, img := copyBytes(t, fixture(t, "grid.heic"))
		if ans.Reason != ReasonOK || ans.Width != 1568 || ans.Height != 1176 {
			t.Fatalf("answer %+v, want ok 1568×1176", ans)
		}
		nearish(t, rgbaAt(img, 2, 2), imagetest.TopLeft)
		nearish(t, rgbaAt(img, 1565, 1173), imagetest.BottomRight)
	})

	t.Run("a WebP is converted", func(t *testing.T) {
		ans, _, img := copyBytes(t, fixture(t, "quadrants.webp"))
		if ans.Reason != ReasonOK || ans.Width != 640 || ans.Height != 480 {
			t.Fatalf("answer %+v, want ok 640×480", ans)
		}
		nearish(t, rgbaAt(img, 160, 120), imagetest.TopLeft) // through the loss of the JPEG copy
	})

	t.Run("a lossy WebP is read as limited-range BT.601", func(t *testing.T) {
		// The colours dwebp 1.6.0 decodes from the fixture (dwebp -ppm), within 3: a full-range
		// reading gives a red of (238, 17, 15).
		file := fixture(t, "quadrants.webp")
		src, conv, _, ok := decode(bytes.NewReader(file), int64(len(file)), formatWebP, MaxPixels)
		if !ok {
			t.Fatal("not decoded")
		}
		got := boxReduce(src, 1, conv)
		want := map[image.Point]color.RGBA{
			{2, 2}: {255, 0, 2, 255}, {637, 2}: {16, 255, 15, 255}, {2, 477}: {0, 0, 255, 255},
			{637, 477}: {255, 255, 16, 255}, {160, 120}: {255, 4, 5, 255}, {480, 120}: {11, 255, 12, 255},
			{160, 360}: {4, 4, 254, 255}, {480, 360}: {254, 255, 11, 255},
		}
		for p, c := range want {
			near(t, got.RGBAAt(p.X, p.Y), c)
		}
	})

	t.Run("the copy is a JPEG without APP or comment segments", func(t *testing.T) {
		file := imagetest.WithEXIF(imagetest.QuadrantJPEG(400, 200), imagetest.EXIF(binary.BigEndian, 1))
		file = imagetest.WithSegment(file, 0xFE, []byte("a comment of the device"))
		_, out, _ := copyBytes(t, file)
		var markers []byte
		reachedScan := false
		for i := 2; i+4 <= len(out) && out[i] == 0xFF; {
			markers = append(markers, out[i+1])
			if out[i+1] == 0xDA {
				reachedScan = true
				break
			}
			i += 2 + int(binary.BigEndian.Uint16(out[i+2:]))
		}
		if !reachedScan {
			t.Fatalf("the walk of the copy's segments stopped before its scan (markers % X)", markers)
		}
		for _, m := range markers {
			if m != 0xDB && m != 0xC0 && m != 0xC4 && m != 0xDA {
				t.Errorf("segment 0x%X in the copy; want only DQT, SOF0, DHT, SOS (markers % X)", m, markers)
			}
		}
		forbidden := [][]byte{
			[]byte(imagetest.FictionalMake), []byte(imagetest.FictionalModel), []byte("Exif"), []byte("a comment"),
			binary.BigEndian.AppendUint32(nil, imagetest.FictionalLatitude), // the GPS latitude
		}
		if !bytes.Contains(file, forbidden[len(forbidden)-1]) {
			t.Fatal("the stored file holds no GPS latitude: the check below proves nothing")
		}
		for _, s := range forbidden {
			if bytes.Contains(out, s) {
				t.Errorf("the copy holds %q", s)
			}
		}
	})

	t.Run("a transparent PNG is flattened on white", func(t *testing.T) {
		_, _, img := copyBytes(t, imagetest.PNGWithAlpha(64, 64))
		nearish(t, rgbaAt(img, 2, 2), color.RGBA{R: 255, G: 255, B: 255, A: 255})
		nearish(t, rgbaAt(img, 61, 2), color.RGBA{R: 255, A: 255})
	})

	t.Run("a header declaring too many pixels is refused without decoding", func(t *testing.T) {
		ans, out, _ := copyBytes(t, imagetest.JPEGDeclaring(10_000, 6_000))
		if ans.Reason != ReasonTooManyPixels || len(out) != 0 {
			t.Errorf("answer %+v, %d bytes written; want too_many_pixels, nothing", ans, len(out))
		}
	})

	t.Run("a HEIC whose frame is larger than its header declares is refused before decoding", func(t *testing.T) {
		// The header gives the size of the image (ispe), which may be cropped from a larger
		// coded frame: patch it to 64×48 over a frame of 640×480, and lower the ceiling between
		// the two. Without a bound on the frame, the decoder would allocate it all.
		file := bytes.Clone(fixture(t, "rotated.heic"))
		at := bytes.Index(file, []byte("ispe"))
		binary.BigEndian.PutUint32(file[at+8:], 64)
		binary.BigEndian.PutUint32(file[at+12:], 48)
		req := defaultRequest()
		req.MaxPixels = 10_000
		var out bytes.Buffer
		ans, err := copyImage(bytes.NewReader(file), int64(len(file)), &out, req)
		if err != nil || ans.Reason == ReasonOK || out.Len() != 0 {
			t.Errorf("answer %+v, %v, %d bytes; want refused, nothing written", ans, err, out.Len())
		}
		// Under the default ceiling the same file is copied, cropped to its header.
		if ans, _, _ := copyBytes(t, file); ans.Reason != ReasonOK {
			t.Errorf("answer %+v under the default ceiling, want ok", ans)
		}
	})

	t.Run("a JPEG cut after its header is undecodable", func(t *testing.T) {
		ans, out, _ := copyBytes(t, imagetest.JPEGCut(64, 48))
		if ans.Reason != ReasonUndecodable || len(out) != 0 {
			t.Errorf("answer %+v, %d bytes written; want undecodable, nothing", ans, len(out))
		}
	})

	t.Run("an unknown format is undecodable", func(t *testing.T) {
		for _, file := range [][]byte{imagetest.GIF(), imagetest.AVIF(), imagetest.HEIC("heic", "mif1"), []byte("not an image")} {
			if ans, _, _ := copyBytes(t, file); ans.Reason != ReasonUndecodable {
				t.Errorf("answer %+v for % x, want undecodable", ans, file[:8])
			}
		}
	})

	t.Run("a copy above its output bound is too large", func(t *testing.T) {
		req := defaultRequest()
		req.MaxOutput = 512
		var out bytes.Buffer
		file := imagetest.QuadrantJPEG(400, 200)
		ans, err := copyImage(bytes.NewReader(file), int64(len(file)), &out, req)
		if err != nil || ans.Reason != ReasonTooLarge || out.Len() > 512 {
			t.Errorf("answer %+v, %v, %d bytes; want too_large, at most 512 bytes", ans, err, out.Len())
		}
	})
}

func TestHEICColours(t *testing.T) {
	// The copy reads the Y′CbCr planes with the matrix and range the file declares: its
	// colours are those of h265's own conversion to NRGBA.
	for _, name := range []string{"grid.heic", "rotated.heic"} {
		file := fixture(t, name)
		want, err := heic.Decode(bytes.NewReader(file))
		if err != nil {
			t.Fatal(err)
		}
		src, info, err := heic.DecodeColor(bytes.NewReader(file), heic.Options{ToYCbCr: true})
		if err != nil {
			t.Fatal(err)
		}
		conv, ok := converterFor(info.Matrix, info.FullRange)
		if !ok {
			t.Fatalf("%s: matrix %d refused", name, info.Matrix)
		}
		got := boxReduce(src, 1, conv)
		b := want.Bounds()
		for _, p := range []image.Point{{3, 3}, {b.Dx() - 4, 3}, {3, b.Dy() - 4}, {b.Dx() - 4, b.Dy() - 4}, {b.Dx() / 3, b.Dy() / 3}} {
			near(t, got.RGBAAt(p.X, p.Y), rgbaAt(want, p.X, p.Y))
		}
	}
}

func TestRequestValid(t *testing.T) {
	if !defaultRequest().valid() {
		t.Fatal("the default request is refused")
	}
	cases := []struct {
		name  string
		patch func(*request)
	}{
		{"pixels above the ceiling", func(r *request) { r.MaxPixels = MaxPixels + 1 }},
		{"no pixels", func(r *request) { r.MaxPixels = 0 }},
		{"long edge above the constant", func(r *request) { r.LongEdge = LongEdge + 1 }},
		{"no long edge", func(r *request) { r.LongEdge = 0 }},
		{"quality above 100", func(r *request) { r.Quality = 101 }},
		{"no quality", func(r *request) { r.Quality = 0 }},
		{"output above the bound", func(r *request) { r.MaxOutput = MaxOutput + 1 }},
		{"no output", func(r *request) { r.MaxOutput = 0 }},
		{"memory above the limit", func(r *request) { r.MemoryLimit = ChildMemoryLimit + 1 }},
		{"no memory", func(r *request) { r.MemoryLimit = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := defaultRequest()
			tc.patch(&r)
			if r.valid() {
				t.Errorf("%+v accepted, want refused", r)
			}
		})
	}
}

func TestNewCopier(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		limits := DefaultLimits()
		limits.Timeout = timeout
		if got := NewCopier([]string{"hippo"}, limits).limits.Timeout; got != Timeout {
			t.Errorf("timeout %v becomes %v, want %v", timeout, got, Timeout)
		}
	}
	limits := DefaultLimits()
	limits.Timeout = time.Second
	if got := NewCopier([]string{"hippo"}, limits).limits.Timeout; got != time.Second {
		t.Errorf("timeout 1s becomes %v", got)
	}
}
