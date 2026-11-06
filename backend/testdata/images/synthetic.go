//go:build ignore

// Command synthetic writes the synthetic PNG the image fixtures are made from (README.md): four
// flat coloured quadrants, red top-left, green top-right, blue bottom-left, yellow bottom-right,
// with a soft gradient so that the encoders have something to code. No photo, no personal
// content (ADR-0027). A .jpg output may carry an EXIF block holding only an orientation (1 to
// 8), the way a camera says how to turn its picture upright; nothing else.
//
//	go run synthetic.go <width> <height> <out.png|out.jpg> [orientation]
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "synthetic:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 && len(args) != 4 {
		return fmt.Errorf("usage: synthetic <width> <height> <out.png|out.jpg> [orientation]")
	}
	width, err := strconv.Atoi(args[0])
	if err != nil {
		return err
	}
	height, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			c := quadrant(x < width/2, y < height/2)
			shade := uint8(16 * x / width) // a gradient of 16 levels at most
			c.R = max(c.R, shade)
			c.G = max(c.G, shade)
			c.B = max(c.B, shade)
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	switch filepath.Ext(args[2]) {
	case ".png":
		if len(args) == 4 {
			return fmt.Errorf("an orientation needs a .jpg output")
		}
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
	case ".jpg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return err
		}
		if len(args) == 4 {
			orientation, err := strconv.Atoi(args[3])
			if err != nil || orientation < 1 || orientation > 8 {
				return fmt.Errorf("orientation %q: want 1 to 8", args[3])
			}
			raw := buf.Bytes()
			out := append([]byte{}, raw[:2]...) // SOI
			out = append(out, exifOrientation(uint16(orientation))...)
			buf = *bytes.NewBuffer(append(out, raw[2:]...))
		}
	default:
		return fmt.Errorf("output %q: want .png or .jpg", args[2])
	}
	return os.WriteFile(args[2], buf.Bytes(), 0o644)
}

// exifOrientation is an APP1 segment whose EXIF block holds the orientation tag alone: a
// big-endian TIFF header, one IFD of one entry (0x0112, SHORT, 1 value), no next IFD.
func exifOrientation(orientation uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08")
	tiff = binary.BigEndian.AppendUint16(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, 0x0112)
	tiff = binary.BigEndian.AppendUint16(tiff, 3)
	tiff = binary.BigEndian.AppendUint32(tiff, 1)
	tiff = binary.BigEndian.AppendUint16(tiff, orientation)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0) // value padding, next IFD offset
	body := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xff, 0xe1}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(body)+2))
	return append(segment, body...)
}

func quadrant(left, top bool) color.RGBA {
	switch {
	case left && top:
		return color.RGBA{R: 255, A: 255}
	case top:
		return color.RGBA{G: 255, A: 255}
	case left:
		return color.RGBA{B: 255, A: 255}
	}
	return color.RGBA{R: 255, G: 255, A: 255}
}
