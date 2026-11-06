package imagecopy

import (
	"encoding/binary"
	"image"
	"io"
)

// Orientation of a JPEG: the EXIF tag 0x0112 of the APP1 segment, which the standard decoder
// ignores. Values 1 to 8 as in EXIF 2.32 (1: upright; 6: turn a quarter clockwise to display).

const (
	// maxSegments bounds the segments read before the scan of a JPEG.
	maxSegments = 64
	exifPrefix  = "Exif\x00\x00"
	// markerSOS, markerEOI, markerAPP1: the JPEG markers the orientation reader stops at or reads.
	markerSOS  = 0xDA
	markerEOI  = 0xD9
	markerAPP1 = 0xE1
	tagOrient  = 0x0112
	typeShort  = 3
)

// jpegOrientation reads the EXIF orientation of the JPEG of r (size bytes): 1 when it has none,
// or any malformed structure. It reads the segment headers before the scan and the body of an
// EXIF APP1 segment only (64 KiB at most, the bound of a segment's length).
func jpegOrientation(r io.ReaderAt, size int64) int {
	var head [4]byte
	if size < 4 || !readAt(r, head[:2], 0) || head[0] != 0xFF || head[1] != 0xD8 {
		return 1
	}
	off := int64(2)
	for range maxSegments {
		if off+4 > size || !readAt(r, head[:], off) || head[0] != 0xFF {
			return 1
		}
		marker := head[1]
		switch {
		case marker == 0xFF: // fill byte before a marker
			off++
			continue
		case marker == markerSOS || marker == markerEOI:
			return 1
		case marker == 0x01 || marker >= 0xD0 && marker <= 0xD7: // no length
			off += 2
			continue
		}
		length := int64(binary.BigEndian.Uint16(head[2:]))
		if length < 2 || off+2+length > size {
			return 1
		}
		if marker == markerAPP1 && length-2 >= int64(len(exifPrefix)) {
			body := make([]byte, length-2)
			if !readAt(r, body, off+4) {
				return 1
			}
			if string(body[:len(exifPrefix)]) == exifPrefix {
				return tiffOrientation(body[len(exifPrefix):])
			}
		}
		off += 2 + length
	}
	return 1
}

// tiffOrientation reads the orientation tag of IFD0 of the TIFF structure of an EXIF block; 1
// when absent, out of range or malformed. Every offset is checked against the block.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:4]) {
	case "II*\x00":
		order = binary.LittleEndian
	case "MM\x00*":
		order = binary.BigEndian
	default:
		return 1
	}
	ifd := int64(order.Uint32(t[4:]))
	if ifd+2 > int64(len(t)) {
		return 1
	}
	count := int64(order.Uint16(t[ifd:]))
	if ifd+2+12*count > int64(len(t)) {
		return 1
	}
	for i := range count {
		e := ifd + 2 + 12*i
		if order.Uint16(t[e:]) != tagOrient {
			continue
		}
		if order.Uint16(t[e+2:]) != typeShort || order.Uint32(t[e+4:]) != 1 {
			return 1
		}
		if v := int(order.Uint16(t[e+8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// readAt fills p from r at off; false on any error, a short read included.
func readAt(r io.ReaderAt, p []byte, off int64) bool {
	n, err := r.ReadAt(p, off)
	return n == len(p) && (err == nil || err == io.EOF)
}

// Orientation of a HEIC: the irot and imir properties of its primary item (ISO/IEC 23008-12),
// applied in that order. The h265 decoder applies them only to a full-size NRGBA image (its
// AutoRotate option), which a 48 Mpx photo cannot afford in the child's memory: they are read
// here, from the meta box, and applied to the copy, the way h265 applies them. Only 32-bit box
// sizes are read: a file whose meta box sits after a 64-bit or open-ended box gets no
// transform (an image of at most 20 MB has none before its meta box).

const (
	// maxMetaBox bounds the meta box read; a larger one gives no transform.
	maxMetaBox = 1 << 20
	// maxTopBoxes bounds the top-level boxes walked to find meta.
	maxTopBoxes = 64
	// noMirror is the mirror of a transform without imir.
	noMirror = -1
)

// transform is the irot and imir of a HEIC's primary item.
type transform struct {
	rotate int // quarter turns anticlockwise, 0 to 3 (irot)
	mirror int // the axis of imir as h265 reads it (0: top and bottom swapped, 1: left and right), or noMirror
}

// orientations are the EXIF orientations that apply t, in order: a quarter anticlockwise is 8,
// a half turn 3, three quarters anticlockwise (one clockwise) 6; then the mirror.
func (t transform) orientations() []int {
	var out []int
	if t.rotate > 0 {
		out = append(out, [4]int{1, 8, 3, 6}[t.rotate&3])
	}
	if t.mirror >= 0 {
		out = append(out, [2]int{4, 2}[t.mirror&1])
	}
	return out
}

// heifTransform reads the irot and imir of the primary item of the HEIF file of r (size bytes);
// none when absent or on any malformed structure.
func heifTransform(r io.ReaderAt, size int64) transform {
	t := transform{mirror: noMirror}
	meta := topBox(r, size, "meta")
	if len(meta) < 4 {
		return t
	}
	primary, hasPrimary := uint32(0), false
	var props [][]byte // each property box of ipco, header included, in order
	var ipma []byte
	eachBox(meta[4:], func(typ string, body []byte) {
		switch {
		case typ == "pitm" && len(body) >= 6 && body[0] == 0:
			primary, hasPrimary = uint32(binary.BigEndian.Uint16(body[4:])), true
		case typ == "pitm" && len(body) >= 8:
			primary, hasPrimary = binary.BigEndian.Uint32(body[4:]), true
		case typ == "iprp":
			eachBox(body, func(typ string, body []byte) {
				switch typ {
				case "ipco":
					eachBox(body, func(typ string, prop []byte) { props = append(props, append([]byte(typ), prop...)) })
				case "ipma":
					ipma = body
				}
			})
		}
	})
	if !hasPrimary {
		return t
	}
	for _, index := range associations(ipma, primary) {
		if index < 1 || index > len(props) || len(props[index-1]) < 5 {
			continue
		}
		switch prop := props[index-1]; string(prop[:4]) {
		case "irot":
			t.rotate = int(prop[4] & 3)
		case "imir":
			t.mirror = int(prop[4] & 1)
		}
	}
	return t
}

// associations are the property indices (1-based) that the body of the ipma box associates
// with item: entries of an item id (16 bits in version 0, else 32), a count, and indices of 7
// bits (15 when flag 1 is set), after an essential bit.
func associations(ipma []byte, item uint32) []int {
	if len(ipma) < 8 {
		return nil
	}
	idLen, indexLen := 2, 1
	if ipma[0] >= 1 {
		idLen = 4
	}
	if ipma[3]&1 == 1 {
		indexLen = 2
	}
	b := ipma[8:]
	for range binary.BigEndian.Uint32(ipma[4:]) {
		if len(b) < idLen+1 {
			return nil
		}
		id := binary.BigEndian.Uint32(append(make([]byte, 4-idLen), b[:idLen]...))
		n := int(b[idLen])
		b = b[idLen+1:]
		if len(b) < n*indexLen {
			return nil
		}
		if id == item {
			indices := make([]int, n)
			for i := range n {
				if indexLen == 2 {
					indices[i] = int(binary.BigEndian.Uint16(b[2*i:]) & 0x7FFF)
				} else {
					indices[i] = int(b[i] & 0x7F)
				}
			}
			return indices
		}
		b = b[n*indexLen:]
	}
	return nil
}

// topBox returns the body of the first top-level box of typ in the file of r (size bytes), at
// most maxMetaBox long; nil when absent, larger or malformed.
func topBox(r io.ReaderAt, size int64, typ string) []byte {
	var head [8]byte
	for off, i := int64(0), 0; i < maxTopBoxes && off+8 <= size && readAt(r, head[:], off); i++ {
		length := int64(binary.BigEndian.Uint32(head[:]))
		if length < 8 || off+length > size {
			return nil
		}
		if string(head[4:]) == typ {
			if length-8 > maxMetaBox {
				return nil
			}
			body := make([]byte, length-8)
			if !readAt(r, body, off+8) {
				return nil
			}
			return body
		}
		off += length
	}
	return nil
}

// eachBox calls fn with the type and body of each box of b, in order, up to the first malformed
// one.
func eachBox(b []byte, fn func(typ string, body []byte)) {
	for len(b) >= 8 {
		length := int64(binary.BigEndian.Uint32(b))
		if length < 8 || length > int64(len(b)) {
			return
		}
		fn(string(b[4:8]), b[8:length])
		b = b[length:]
	}
}

// orient turns img upright for the EXIF orientation o; any other value leaves it as is.
func orient(img *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return img
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si := img.PixOffset(b.Min.X+sx, b.Min.Y+sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return dst
}
