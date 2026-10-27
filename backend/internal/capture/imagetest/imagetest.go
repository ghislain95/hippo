// Package imagetest builds image files for tests, in code: PNG and JPEG through the standard
// encoders, HEIC, WebP, GIF and AVIF through their minimal headers. No image of the repository
// holds personal content (ADR-0027). Only tests import it: its panics are programming errors
// of a test.
package imagetest

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
)

// PNG is a valid width×height PNG.
func PNG(width, height int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, gradient(width, height)); err != nil {
		panic("imagetest: encode png: " + err.Error())
	}
	return buf.Bytes()
}

// PNGOfSize is a valid width×height PNG of exactly size bytes: a private ancillary chunk, which
// decoders skip, pads it before its end. size must leave room for that chunk (12 bytes).
func PNGOfSize(width, height, size int) []byte {
	raw := PNG(width, height)
	const chunkOverhead = 12 // length, type, CRC
	padding := size - len(raw) - chunkOverhead
	if padding < 0 || padding > math.MaxUint32 {
		panic("imagetest: size out of range for the png")
	}
	iend := raw[len(raw)-12:] // the IEND chunk always ends the stream, 12 bytes
	chunk := make([]byte, 0, chunkOverhead+padding)
	chunk = binary.BigEndian.AppendUint32(chunk, uint32(padding))
	chunk = append(chunk, "prVt"...)
	chunk = append(chunk, make([]byte, padding)...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	out := make([]byte, 0, size)
	out = append(out, raw[:len(raw)-12]...)
	out = append(out, chunk...)
	return append(out, iend...)
}

// JPEG is a valid width×height JPEG.
func JPEG(width, height int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, gradient(width, height), &jpeg.Options{Quality: 50}); err != nil {
		panic("imagetest: encode jpeg: " + err.Error())
	}
	return buf.Bytes()
}

// HEIC is a file starting with an ftyp box of this major brand and these compatible brands,
// followed by a few bytes: enough for format detection, not a decodable image.
func HEIC(major string, compatible ...string) []byte {
	box := Ftyp(major, compatible...)
	return append(box, make([]byte, 64)...)
}

// Ftyp is an ISO BMFF ftyp box: size, "ftyp", major brand, minor version, compatible brands.
func Ftyp(major string, compatible ...string) []byte {
	size := 16 + 4*len(compatible)
	if size > math.MaxUint32 {
		panic("imagetest: too many brands")
	}
	box := binary.BigEndian.AppendUint32(nil, uint32(size))
	box = append(box, "ftyp"...)
	box = append(box, major...)
	box = append(box, 0, 0, 0, 0)
	for _, brand := range compatible {
		box = append(box, brand...)
	}
	return box
}

// HEICWithFtypSize is a HEIC file (major brand major, compatible with heic) whose ftyp box is
// exactly size bytes long, its last bytes being padding, followed by a few bytes.
func HEICWithFtypSize(major string, size int) []byte {
	if size < 20 || size > math.MaxUint32 {
		panic("imagetest: ftyp size out of range")
	}
	compatible := make([]string, (size-16)/4)
	for i := range compatible {
		compatible[i] = "mif1"
	}
	compatible[0] = "heic"
	box := Ftyp(major, compatible...)
	box = append(box, make([]byte, size-len(box))...)
	binary.BigEndian.PutUint32(box[0:4], uint32(size))
	return append(box, make([]byte, 64)...)
}

// WebP is a RIFF WEBP file whose first chunk is chunk ("VP8 ", "VP8L", "VP8X"…), followed by a
// few bytes: enough for format detection, not a decodable image.
func WebP(chunk string) []byte {
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, 4+8+16)
	out = append(out, "WEBP"...)
	out = append(out, chunk...)
	out = binary.LittleEndian.AppendUint32(out, 16)
	return append(out, make([]byte, 16)...)
}

// GIF is the header of a 1×1 GIF.
func GIF() []byte {
	return append([]byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"), make([]byte, 16)...)
}

// AVIF is the header of an AVIF file: an ftyp box of brand avif, compatible with mif1.
func AVIF() []byte {
	return HEIC("avif", "avif", "mif1", "miaf")
}

// gradient is a width×height image whose rows change colour, so that the encoders have
// something to encode.
func gradient(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var shade uint8
	for y := range height {
		shade += 3
		for x := range width {
			img.Set(x, y, color.RGBA{R: shade, G: 64, B: 128, A: 255})
		}
	}
	return img
}
