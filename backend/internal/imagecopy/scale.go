package imagecopy

import (
	"image"
	"image/color"
	"math"

	"golang.org/x/image/draw"
)

// Scaling, in two steps (design.md D10): an integer box reduction by k = floor(long / edge)
// when k ≥ 2, which averages k×k blocks and is the anti-aliasing filter, then
// draw.ApproxBiLinear to the exact size, where the factor left is below 2. Every draw.Kernel
// allocates a buffer of the destination width times the source height (x/image draw/scale.go
// makeTmpBuf), 303 MB for a 48 Mpx photo: the box step keeps the memory to the decoded source
// plus two small RGBA images. Transparency is flattened on white in the same pass.

// boxFactor is the integer reduction of the box step for an image whose longer side is long.
func boxFactor(long, edge int) int {
	return max(1, long/edge)
}

// outputSize is the size of the copy of a w×h image: the longer side at most edge, the other
// one rounded, never scaled up, never below one pixel.
func outputSize(w, h, edge int) (int, int) {
	long, short := max(w, h), min(w, h)
	if long <= edge {
		return w, h
	}
	scaled := int(max(1, (int64(short)*int64(edge)+int64(long)/2)/int64(long)))
	if w >= h {
		return edge, scaled
	}
	return scaled, edge
}

// scale returns the opaque copy of src at its output size; conv reads a *image.YCbCr.
func scale(src image.Image, conv converter, edge int) *image.RGBA {
	b := src.Bounds()
	w, h := outputSize(b.Dx(), b.Dy(), edge)
	mid := boxReduce(src, boxFactor(max(b.Dx(), b.Dy()), edge), conv)
	if mid.Bounds().Dx() == w && mid.Bounds().Dy() == h {
		return mid
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), mid, mid.Bounds(), draw.Src, nil)
	return dst
}

// boxReduce averages the k×k blocks of src (the blocks of the last row and column may be
// smaller) into an opaque RGBA image, transparency flattened on white.
func boxReduce(src image.Image, k int, conv converter) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := (w+k-1)/k, (h+k-1)/k
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	// sums holds four channels per destination pixel of the current block row: Y, Cb, Cr for
	// a *image.YCbCr or *image.Gray (planar, converted by conv once averaged), else red, green,
	// blue already flattened on white, in 16 bits.
	sums := make([]int64, 4*dw)
	planar := false
	switch src.(type) {
	case *image.YCbCr, *image.Gray:
		planar = true
	}
	for dy := range dh {
		clear(sums)
		y0, y1 := b.Min.Y+dy*k, b.Min.Y+min((dy+1)*k, h)
		for y := y0; y < y1; y++ {
			switch s := src.(type) {
			case *image.YCbCr:
				for x := range w {
					sx := b.Min.X + x
					ci := s.COffset(sx, y)
					p := sums[4*(x/k):]
					p[0] += int64(s.Y[s.YOffset(sx, y)])
					p[1] += int64(s.Cb[ci])
					p[2] += int64(s.Cr[ci])
				}
			case *image.RGBA:
				row := s.Pix[s.PixOffset(b.Min.X, y):]
				for x := range w {
					px := row[4*x : 4*x+4]
					white := 0xFF - int64(px[3]) // premultiplied: add the white the alpha lets through
					p := sums[4*(x/k):]
					p[0] += (int64(px[0]) + white) * 0x101
					p[1] += (int64(px[1]) + white) * 0x101
					p[2] += (int64(px[2]) + white) * 0x101
				}
			case *image.NRGBA:
				row := s.Pix[s.PixOffset(b.Min.X, y):]
				for x := range w {
					px := row[4*x : 4*x+4]
					a := int64(px[3])
					white := (0xFF - a) * 0xFF
					p := sums[4*(x/k):]
					// 16 bits: c·a/255 + 255·(255−a)/255, scaled by 0x101/0xFF = 257/255.
					p[0] += (int64(px[0])*a + white) * 0x101 / 0xFF
					p[1] += (int64(px[1])*a + white) * 0x101 / 0xFF
					p[2] += (int64(px[2])*a + white) * 0x101 / 0xFF
				}
			case *image.Gray:
				// Luma without chroma: a monochrome HEIC may be limited range, which conv knows.
				row := s.Pix[s.PixOffset(b.Min.X, y):]
				for x := range w {
					p := sums[4*(x/k):]
					p[0] += int64(row[x])
					p[1] += 128
					p[2] += 128
				}
			case *image.NYCbCrA:
				// Converted pixel by pixel: the alpha weighs each colour before the average.
				for x := range w {
					sx := b.Min.X + x
					ci := s.COffset(sx, y)
					c := conv.rgb(float64(s.Y[s.YOffset(sx, y)]), float64(s.Cb[ci]), float64(s.Cr[ci]))
					a := int64(s.A[s.AOffset(sx, y)])
					white := (0xFF - a) * 0xFF
					p := sums[4*(x/k):]
					p[0] += (int64(c.R)*a + white) * 0x101 / 0xFF
					p[1] += (int64(c.G)*a + white) * 0x101 / 0xFF
					p[2] += (int64(c.B)*a + white) * 0x101 / 0xFF
				}
			default:
				for x := range w {
					r, g, bl, a := src.At(b.Min.X+x, y).RGBA() // premultiplied, 16 bits
					white := int64(0xFFFF - a)
					p := sums[4*(x/k):]
					p[0] += int64(r) + white
					p[1] += int64(g) + white
					p[2] += int64(bl) + white
				}
			}
		}
		bh := int64(y1 - y0)
		for dx := range dw {
			bw := int64(min((dx+1)*k, w) - dx*k)
			n := bw * bh
			p := sums[4*dx:]
			var c color.RGBA
			if planar {
				c = conv.rgb(float64(p[0])/float64(n), float64(p[1])/float64(n), float64(p[2])/float64(n))
			} else {
				c = color.RGBA{R: to8(p[0] / n), G: to8(p[1] / n), B: to8(p[2] / n), A: 0xFF}
			}
			dst.SetRGBA(dx, dy, c)
		}
	}
	return dst
}

// to8 rounds a 16-bit channel to 8 bits.
func to8(v int64) uint8 {
	v = (v + 0x80) / 0x101
	if v <= 0 {
		return 0
	}
	if v >= 0xFF {
		return 0xFF
	}
	return uint8(v)
}

// converter turns averaged Y′CbCr samples of 8 bits into RGB, for a matrix and a range.
type converter struct {
	kr, kb float64
	full   bool
}

// jfif is the conversion of JPEG (JFIF) and of PNG greys: BT.601, full range, as
// image/color.YCbCrToRGB.
var jfif = converter{kr: 0.299, kb: 0.114, full: true}

// vp8 is the conversion of a lossy WebP: BT.601, limited range (RFC 6386, as libwebp's
// VP8YUVToRGB), which golang.org/x/image/webp returns as an *image.YCbCr without saying so.
var vp8 = converter{kr: 0.299, kb: 0.114, full: false}

// converterFor is the conversion of a HEIC declaring the matrix coefficients code matrix
// (ISO/IEC 23091-2) and its range; ok is false for a matrix that is not a weighted sum of R, G
// and B (0 identity/GBR, 8 YCgCo, 10 and 12 to 14 constant luminance or ICtCp) or is reserved:
// such an image is not copied rather than copied with wrong colours. Unspecified (2) reads as
// BT.601, as h265 does.
func converterFor(matrix uint16, fullRange bool) (c converter, ok bool) {
	c = converter{full: fullRange}
	switch matrix {
	case 1: // BT.709
		c.kr, c.kb = 0.2126, 0.0722
	case 2, 5, 6: // unspecified, BT.470 B/G, BT.601
		c.kr, c.kb = 0.299, 0.114
	case 4: // FCC
		c.kr, c.kb = 0.30, 0.11
	case 7: // SMPTE 240M
		c.kr, c.kb = 0.212, 0.087
	case 9: // BT.2020 non-constant luminance
		c.kr, c.kb = 0.2627, 0.0593
	default:
		return converter{}, false
	}
	return c, true
}

// rgb converts one sample.
func (c converter) rgb(y, cb, cr float64) color.RGBA {
	if c.full {
		cb, cr = cb-128, cr-128
	} else {
		y = (y - 16) * 255 / 219
		cb = (cb - 128) * 255 / 224
		cr = (cr - 128) * 255 / 224
	}
	kg := 1 - c.kr - c.kb
	r := y + 2*(1-c.kr)*cr
	bl := y + 2*(1-c.kb)*cb
	g := (y - c.kr*r - c.kb*bl) / kg
	return color.RGBA{R: clamp(r), G: clamp(g), B: clamp(bl), A: 0xFF}
}

func clamp(v float64) uint8 {
	return uint8(min(255, max(0, math.Round(v))))
}
