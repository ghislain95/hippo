// Package imagetest builds image files for tests, in code: PNG and JPEG through the standard
// encoders, HEIC, WebP, GIF and AVIF through their minimal headers, and the JPEG and PNG the
// image copy is tested on (EXIF orientation, fictitious GPS and device name, a header declaring
// more pixels than the file holds, a JPEG cut after its header, transparency). No image of the
// repository holds personal content (ADR-0027). Only tests import it: its panics are
// programming errors of a test.
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

// The colours of the quadrants of Quadrants, the same as backend/testdata/images/synthetic.go.
var (
	TopLeft     = color.RGBA{R: 255, A: 255} // red
	TopRight    = color.RGBA{G: 255, A: 255} // green
	BottomLeft  = color.RGBA{B: 255, A: 255} // blue
	BottomRight = color.RGBA{R: 255, G: 255, A: 255}
)

// Quadrants is a width×height image of four flat quadrants (TopLeft, TopRight, BottomLeft,
// BottomRight): after a rotation or a mirror, the colour of a corner tells where it came from.
func Quadrants(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			c := BottomRight
			switch {
			case x < width/2 && y < height/2:
				c = TopLeft
			case y < height/2:
				c = TopRight
			case x < width/2:
				c = BottomLeft
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// QuadrantJPEG is Quadrants encoded as a JPEG of quality 90.
func QuadrantJPEG(width, height int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, Quadrants(width, height), &jpeg.Options{Quality: 90}); err != nil {
		panic("imagetest: encode jpeg: " + err.Error())
	}
	return buf.Bytes()
}

// Fictitious values of the EXIF block of EXIF: distinctive bytes that a test searches for in a
// copy. No real device, no real place.
const (
	FictionalMake  = "Fictional Camera Maker"
	FictionalModel = "Fictional Phone Model 7"
	// FictionalLatitude is the numerator of the degrees of the GPS latitude, a rational whose
	// eight bytes appear as is in the block.
	FictionalLatitude = 0x0BADC0DE
)

// EXIF tags written by EXIF.
const (
	tagMake        = 0x010F
	tagModel       = 0x0110
	tagOrientation = 0x0112
	tagGPSIFD      = 0x8825
	tagGPSLatRef   = 0x0001
	tagGPSLat      = 0x0002
	typeASCII      = 2
	typeShort      = 3
	typeLong       = 4
	typeRational   = 5
)

// ByteOrder is binary.BigEndian or binary.LittleEndian: the two byte orders of a TIFF header.
type ByteOrder interface {
	binary.ByteOrder
	binary.AppendByteOrder
}

// EXIF is the TIFF structure of an EXIF block (without its "Exif\0\0" prefix), in order: IFD0
// holding the device's make and model (FictionalMake, FictionalModel), the orientation when it
// is not zero, and a pointer to a GPS IFD holding a fictitious latitude (FictionalLatitude).
func EXIF(order ByteOrder, orientation uint16) []byte {
	type entry struct {
		tag, typ uint16
		count    uint32
		value    []byte // inline when 4 bytes or less, else written after the IFD
	}
	ascii := func(s string) []byte { return append([]byte(s), 0) }
	ifd0 := []entry{
		{tag: tagMake, typ: typeASCII, count: uint32(len(FictionalMake) + 1), value: ascii(FictionalMake)},
		{tag: tagModel, typ: typeASCII, count: uint32(len(FictionalModel) + 1), value: ascii(FictionalModel)},
	}
	if orientation != 0 {
		ifd0 = append(ifd0, entry{tag: tagOrientation, typ: typeShort, count: 1, value: order.AppendUint16(nil, orientation)})
	}
	ifd0 = append(ifd0, entry{tag: tagGPSIFD, typ: typeLong, count: 1}) // value set below
	lat := order.AppendUint32(nil, FictionalLatitude)
	lat = order.AppendUint32(lat, 1)
	lat = order.AppendUint32(lat, 30)
	lat = order.AppendUint32(lat, 1)
	lat = order.AppendUint32(lat, 0)
	lat = order.AppendUint32(lat, 1)
	gps := []entry{
		{tag: tagGPSLatRef, typ: typeASCII, count: 2, value: ascii("N")},
		{tag: tagGPSLat, typ: typeRational, count: 3, value: lat},
	}

	// writeIFD writes the entries at offset start, their long values right after them, and
	// returns the bytes and the offset where they end.
	writeIFD := func(entries []entry, start uint32) []byte {
		size := u32(2 + 12*len(entries) + 4)
		extra := start + size
		var head, tail []byte
		head = order.AppendUint16(head, u16(len(entries)))
		for _, e := range entries {
			head = order.AppendUint16(head, e.tag)
			head = order.AppendUint16(head, e.typ)
			head = order.AppendUint32(head, e.count)
			if len(e.value) <= 4 {
				head = append(head, e.value...)
				head = append(head, make([]byte, 4-len(e.value))...)
				continue
			}
			head = order.AppendUint32(head, extra+u32(len(tail)))
			tail = append(tail, e.value...)
		}
		head = order.AppendUint32(head, 0) // no next IFD
		return append(head, tail...)
	}

	header := []byte("II*\x00")
	if order.Uint16([]byte{0, 1}) == 1 { // big-endian
		header = []byte("MM\x00*")
	}
	header = order.AppendUint32(header, 8)
	// IFD0 size does not depend on the GPS pointer's value: write it once to learn where the
	// GPS IFD starts, then again with the pointer.
	first := writeIFD(ifd0, 8)
	gpsStart := 8 + u32(len(first))
	ifd0[len(ifd0)-1].value = order.AppendUint32(nil, gpsStart)
	out := append(header, writeIFD(ifd0, 8)...)
	return append(out, writeIFD(gps, gpsStart)...)
}

// u16 and u32 convert the small lengths of a structure a test builds.
func u16(n int) uint16 {
	if n < 0 || n > math.MaxUint16 {
		panic("imagetest: length out of range")
	}
	return uint16(n)
}

func u32(n int) uint32 {
	if n < 0 || n > math.MaxUint32 {
		panic("imagetest: length out of range")
	}
	return uint32(n)
}

// WithSegment inserts, right after the SOI marker of the JPEG jpg, a segment of this marker
// (0xE1 for APP1…) and body.
func WithSegment(jpg []byte, marker byte, body []byte) []byte {
	out := append([]byte{}, jpg[:2]...)
	out = append(out, 0xFF, marker)
	out = binary.BigEndian.AppendUint16(out, u16(len(body)+2))
	out = append(out, body...)
	return append(out, jpg[2:]...)
}

// WithEXIF inserts an APP1 segment holding the EXIF block tiff (EXIF) into the JPEG jpg.
func WithEXIF(jpg, tiff []byte) []byte {
	return WithSegment(jpg, 0xE1, append([]byte("Exif\x00\x00"), tiff...))
}

// JPEGDeclaring is a small JPEG whose frame header declares width×height: its decoder reads
// those dimensions from the header, and the file never holds them. Nothing of that size is
// built.
func JPEGDeclaring(width, height int) []byte {
	if width > math.MaxUint16 || height > math.MaxUint16 || width < 1 || height < 1 {
		panic("imagetest: dimensions out of range for a jpeg")
	}
	out := JPEG(16, 16)
	sof := segmentOffset(out, 0xC0)
	// SOF0: marker (2), length (2), precision (1), height (2), width (2).
	binary.BigEndian.PutUint16(out[sof+5:], uint16(height))
	binary.BigEndian.PutUint16(out[sof+7:], uint16(width))
	return out
}

// JPEGCut is a width×height JPEG cut right after the header of its scan: its dimensions are
// readable, its pixels are not.
func JPEGCut(width, height int) []byte {
	full := JPEG(width, height)
	sos := segmentOffset(full, 0xDA)
	length := int(binary.BigEndian.Uint16(full[sos+2:]))
	return append([]byte{}, full[:sos+2+length]...)
}

// segmentOffset is the offset of the first segment of marker in the JPEG jpg, which must have
// one before its scan data.
func segmentOffset(jpg []byte, marker byte) int {
	for i := 2; i+4 <= len(jpg); {
		if jpg[i] != 0xFF {
			break
		}
		if jpg[i+1] == marker {
			return i
		}
		i += 2 + int(binary.BigEndian.Uint16(jpg[i+2:]))
	}
	panic("imagetest: jpeg segment not found")
}

// PNGWithAlpha is a width×height PNG whose left half is fully transparent and right half
// opaque red.
func PNGWithAlpha(width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := width / 2; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("imagetest: encode png: " + err.Error())
	}
	return buf.Bytes()
}
