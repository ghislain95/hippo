package imagecopy

import (
	"image"
	"image/color"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
)

func TestBoxFactor(t *testing.T) {
	cases := []struct{ long, want int }{
		{800, 1}, {1568, 1}, {1569, 1}, {3135, 1}, {3136, 2}, {4000, 2}, {4032, 2}, {8064, 5}, {50_000_000, 31887},
	}
	for _, tc := range cases {
		if got := boxFactor(tc.long, 1568); got != tc.want {
			t.Errorf("boxFactor(%d) = %d, want %d", tc.long, got, tc.want)
		}
	}
}

func TestOutputSize(t *testing.T) {
	cases := []struct{ w, h, wantW, wantH int }{
		{4000, 3000, 1568, 1176},
		{3000, 4000, 1176, 1568},
		{800, 600, 800, 600},     // never scaled up
		{1568, 1000, 1568, 1000}, // already at the edge
		{1569, 1569, 1568, 1568},
		{8064, 6048, 1568, 1176},
		{1290, 2796, 723, 1568},  // a phone screenshot
		{50_000_000, 1, 1568, 1}, // never below one pixel
	}
	for _, tc := range cases {
		if w, h := outputSize(tc.w, tc.h, 1568); w != tc.wantW || h != tc.wantH {
			t.Errorf("outputSize(%d, %d) = %d×%d, want %d×%d", tc.w, tc.h, w, h, tc.wantW, tc.wantH)
		}
	}
}

func TestScale(t *testing.T) {
	t.Run("scales to the output size through the box step", func(t *testing.T) {
		src := imagetest.Quadrants(4000, 3000)
		got := scale(src, jfif, 1568)
		if got.Bounds() != image.Rect(0, 0, 1568, 1176) {
			t.Fatalf("bounds %v, want 1568×1176", got.Bounds())
		}
		near(t, got.RGBAAt(10, 10), imagetest.TopLeft)
		near(t, got.RGBAAt(1557, 1165), imagetest.BottomRight)
	})

	t.Run("an image within the edge keeps its size", func(t *testing.T) {
		got := scale(imagetest.Quadrants(800, 600), jfif, 1568)
		if got.Bounds() != image.Rect(0, 0, 800, 600) {
			t.Errorf("bounds %v, want 800×600", got.Bounds())
		}
	})

	t.Run("each pixel type gives the same colours", func(t *testing.T) {
		rgba := imagetest.Quadrants(64, 32)
		nrgba := image.NewNRGBA(rgba.Bounds())
		ycc := image.NewYCbCr(rgba.Bounds(), image.YCbCrSubsampleRatio420)
		gray := image.NewGray(rgba.Bounds())
		cmyk := image.NewCMYK(rgba.Bounds())
		for y := range 32 {
			for x := range 64 {
				c := rgba.RGBAAt(x, y)
				nrgba.SetNRGBA(x, y, color.NRGBA{R: c.R, G: c.G, B: c.B, A: 255})
				yy, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
				ycc.Y[ycc.YOffset(x, y)] = yy
				ycc.Cb[ycc.COffset(x, y)] = cb
				ycc.Cr[ycc.COffset(x, y)] = cr
				gray.SetGray(x, y, color.Gray{Y: 128})
				cmyk.Set(x, y, c)
			}
		}
		for name, src := range map[string]image.Image{"RGBA": rgba, "NRGBA": nrgba, "YCbCr": ycc, "CMYK": cmyk} {
			got := scale(src, jfif, 16) // factor 4
			if got.Bounds() != image.Rect(0, 0, 16, 8) {
				t.Errorf("%s: bounds %v, want 16×8", name, got.Bounds())
				continue
			}
			near(t, got.RGBAAt(1, 1), imagetest.TopLeft)
			near(t, got.RGBAAt(14, 1), imagetest.TopRight)
			near(t, got.RGBAAt(1, 6), imagetest.BottomLeft)
			near(t, got.RGBAAt(14, 6), imagetest.BottomRight)
		}
		near(t, scale(gray, jfif, 16).RGBAAt(3, 3), color.RGBA{R: 128, G: 128, B: 128, A: 255})
	})

	t.Run("transparency is flattened on white", func(t *testing.T) {
		src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := range 8 {
			for x := 4; x < 8; x++ {
				src.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			}
			src.SetNRGBA(0, y, color.NRGBA{B: 255, A: 0}) // transparent, whatever its colour
		}
		half := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		for i := range 4 {
			half.SetNRGBA(i%2, i/2, color.NRGBA{A: 128}) // black at half opacity
		}
		for name, img := range map[string]*image.RGBA{"same size": scale(src, jfif, 1568), "reduced": scale(src, jfif, 2)} {
			w := img.Bounds().Dx()
			near(t, img.RGBAAt(0, 0), color.RGBA{R: 255, G: 255, B: 255, A: 255})
			near(t, img.RGBAAt(w-1, 0), color.RGBA{R: 255, A: 255})
			if img.Opaque() == false {
				t.Errorf("%s: not opaque", name)
			}
		}
		near(t, scale(half, jfif, 1568).RGBAAt(1, 1), color.RGBA{R: 127, G: 127, B: 127, A: 255})
	})
}

func TestYCbCrConversion(t *testing.T) {
	t.Run("full-range BT.601 is the JFIF conversion", func(t *testing.T) {
		for _, c := range []color.RGBA{imagetest.TopLeft, imagetest.TopRight, imagetest.BottomLeft, {R: 12, G: 200, B: 99, A: 255}} {
			y, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
			near(t, jfif.rgb(float64(y), float64(cb), float64(cr)), c)
		}
	})

	t.Run("limited range and BT.709 are read as declared", func(t *testing.T) {
		// Limited range: black is 16, white 235, neutral chroma 128.
		limited, ok := converterFor(1, false)
		if !ok {
			t.Fatal("BT.709 refused")
		}
		near(t, limited.rgb(16, 128, 128), color.RGBA{A: 255})
		near(t, limited.rgb(235, 128, 128), color.RGBA{R: 255, G: 255, B: 255, A: 255})
		// Pure red in limited BT.709: Y 63, Cb 102, Cr 240.
		near(t, limited.rgb(63, 102, 240), color.RGBA{R: 255, A: 255})
		// Unspecified matrix reads as BT.601, like h265.
		if c, ok := converterFor(2, true); !ok || c != jfif {
			t.Errorf("unspecified full range = %+v, %v; want the JFIF conversion", c, ok)
		}
		// A lossy WebP is limited-range BT.601: pure red is Y 81, Cb 90, Cr 240.
		near(t, vp8.rgb(81, 90, 240), color.RGBA{R: 255, A: 255})
	})

	t.Run("a matrix that is not a weighted sum of RGB is refused", func(t *testing.T) {
		for _, matrix := range []uint16{0, 3, 8, 10, 11, 12, 13, 14, 15, 255} {
			if _, ok := converterFor(matrix, true); ok {
				t.Errorf("matrix %d accepted, want refused", matrix)
			}
		}
		for _, matrix := range []uint16{1, 2, 4, 5, 6, 7, 9} {
			if _, ok := converterFor(matrix, false); !ok {
				t.Errorf("matrix %d refused, want accepted", matrix)
			}
		}
	})

	t.Run("a limited-range grey is expanded", func(t *testing.T) {
		gray := image.NewGray(image.Rect(0, 0, 4, 1))
		copy(gray.Pix, []uint8{16, 16, 235, 235})
		limited, _ := converterFor(6, false)
		got := boxReduce(gray, 1, limited)
		near(t, got.RGBAAt(0, 0), color.RGBA{A: 255})
		near(t, got.RGBAAt(3, 0), color.RGBA{R: 255, G: 255, B: 255, A: 255})
		// Full range leaves it as is.
		near(t, boxReduce(gray, 1, jfif).RGBAAt(0, 0), color.RGBA{R: 16, G: 16, B: 16, A: 255})
	})

	t.Run("Y′CbCr with alpha is converted by the converter and flattened on white", func(t *testing.T) {
		img := image.NewNYCbCrA(image.Rect(0, 0, 4, 4), image.YCbCrSubsampleRatio420)
		for i := range img.Y {
			img.Y[i] = 81 // limited-range red
		}
		for i := range img.Cb {
			img.Cb[i], img.Cr[i] = 90, 240
		}
		for y := range 4 {
			for x := range 4 {
				img.A[img.AOffset(x, y)] = map[bool]uint8{true: 255, false: 0}[x >= 2]
			}
		}
		got := boxReduce(img, 1, vp8)
		near(t, got.RGBAAt(3, 0), color.RGBA{R: 255, A: 255})
		near(t, got.RGBAAt(0, 0), color.RGBA{R: 255, G: 255, B: 255, A: 255})
		// Averaged over the block: half red, half white.
		near(t, boxReduce(img, 4, vp8).RGBAAt(0, 0), color.RGBA{R: 255, G: 128, B: 128, A: 255})
	})
}

// near fails when a colour channel differs by more than 3 from want.
func near(t *testing.T, got, want color.RGBA) {
	t.Helper()
	diff := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	if diff(got.R, want.R) > 3 || diff(got.G, want.G) > 3 || diff(got.B, want.B) > 3 || got.A != want.A {
		t.Errorf("colour %v, want %v", got, want)
	}
}
