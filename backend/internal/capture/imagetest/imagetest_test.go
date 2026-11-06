package imagetest_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
)

func TestQuadrants(t *testing.T) {
	img := imagetest.Quadrants(4, 2)
	corners := map[image.Point]color.RGBA{
		{0, 0}: imagetest.TopLeft, {3, 0}: imagetest.TopRight,
		{0, 1}: imagetest.BottomLeft, {3, 1}: imagetest.BottomRight,
	}
	for p, want := range corners {
		if got := img.RGBAAt(p.X, p.Y); got != want {
			t.Errorf("pixel %v = %v, want %v", p, got, want)
		}
	}
	decoded, err := jpeg.Decode(bytes.NewReader(imagetest.QuadrantJPEG(400, 200)))
	if err != nil || decoded.Bounds() != image.Rect(0, 0, 400, 200) {
		t.Errorf("QuadrantJPEG: %v, %v; want a 400×200 JPEG", decoded, err)
	}
}

func TestEXIF(t *testing.T) {
	for _, order := range []imagetest.ByteOrder{binary.BigEndian, binary.LittleEndian} {
		t.Run(order.String(), func(t *testing.T) {
			tiff := imagetest.EXIF(order, 6)
			wantHeader := map[string]string{"BigEndian": "MM\x00*", "LittleEndian": "II*\x00"}[order.String()]
			if string(tiff[:4]) != wantHeader || order.Uint32(tiff[4:]) != 8 {
				t.Fatalf("header % x, want %q and IFD0 at 8", tiff[:8], wantHeader)
			}
			// IFD0: make, model, orientation, GPS pointer.
			if n := order.Uint16(tiff[8:]); n != 4 {
				t.Fatalf("IFD0 holds %d entries, want 4", n)
			}
			orientation := tiff[10+2*12:]
			if order.Uint16(orientation) != 0x0112 || order.Uint16(orientation[8:]) != 6 {
				t.Errorf("third entry % x, want the orientation 6", orientation[:12])
			}
			gps := tiff[10+3*12:]
			gpsIFD := order.Uint32(gps[8:])
			if order.Uint16(gps) != 0x8825 || int(gpsIFD) >= len(tiff) || order.Uint16(tiff[gpsIFD:]) != 2 {
				t.Errorf("GPS pointer % x, want a GPS IFD of two entries inside the block", gps[:12])
			}
			for _, want := range [][]byte{
				[]byte(imagetest.FictionalMake), []byte(imagetest.FictionalModel),
				order.AppendUint32(nil, imagetest.FictionalLatitude),
			} {
				if !bytes.Contains(tiff, want) {
					t.Errorf("block does not hold %q", want)
				}
			}
			if n := order.Uint16(imagetest.EXIF(order, 0)[8:]); n != 3 {
				t.Errorf("without orientation, IFD0 holds %d entries, want 3", n)
			}
		})
	}
}

func TestWithEXIF(t *testing.T) {
	tiff := imagetest.EXIF(binary.BigEndian, 6)
	jpg := imagetest.WithEXIF(imagetest.JPEG(8, 8), tiff)
	if !bytes.Equal(jpg[:4], []byte{0xFF, 0xD8, 0xFF, 0xE1}) {
		t.Fatalf("starts with % x, want SOI then APP1", jpg[:4])
	}
	if n := int(binary.BigEndian.Uint16(jpg[4:])); n != len(tiff)+8 || string(jpg[6:12]) != "Exif\x00\x00" {
		t.Errorf("APP1 of %d bytes starting %q, want %d bytes starting Exif", n, jpg[6:12], len(tiff)+8)
	}
	if _, err := jpeg.Decode(bytes.NewReader(jpg)); err != nil {
		t.Errorf("decode: %v", err)
	}
}

func TestJPEGDeclaring(t *testing.T) {
	jpg := imagetest.JPEGDeclaring(10_000, 6_000)
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpg))
	if err != nil || cfg.Width != 10_000 || cfg.Height != 6_000 {
		t.Errorf("config %d×%d, %v; want 10000×6000", cfg.Width, cfg.Height, err)
	}
	if len(jpg) > 4096 {
		t.Errorf("file of %d bytes, want a small one", len(jpg))
	}
}

func TestJPEGCut(t *testing.T) {
	jpg := imagetest.JPEGCut(64, 48)
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(jpg))
	if err != nil || cfg.Width != 64 || cfg.Height != 48 {
		t.Errorf("config %d×%d, %v; want 64×48", cfg.Width, cfg.Height, err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(jpg)); err == nil {
		t.Error("decode succeeded, want an error")
	}
}

func TestPNGWithAlpha(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(imagetest.PNGWithAlpha(4, 2)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Errorf("left pixel alpha %d, want transparent", a)
	}
	if r, g, b, a := img.At(3, 1).RGBA(); r != 0xffff || g != 0 || b != 0 || a != 0xffff {
		t.Errorf("right pixel %d %d %d %d, want opaque red", r, g, b, a)
	}
}
