package imagecopy

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"slices"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/capture/imagetest"
)

func TestJPEGOrientation(t *testing.T) {
	jpg := imagetest.JPEG(8, 8)
	orders := []imagetest.ByteOrder{binary.BigEndian, binary.LittleEndian}

	t.Run("reads the values 1 to 8 in both byte orders", func(t *testing.T) {
		for _, order := range orders {
			for want := 1; want <= 8; want++ {
				file := imagetest.WithEXIF(jpg, imagetest.EXIF(order, uint16(want)))
				if got := jpegOrientation(bytes.NewReader(file), int64(len(file))); got != want {
					t.Errorf("%v, orientation %d: got %d", order, want, got)
				}
			}
		}
	})

	t.Run("an EXIF block after another APP segment is read", func(t *testing.T) {
		file := imagetest.WithEXIF(jpg, imagetest.EXIF(binary.BigEndian, 6))
		file = imagetest.WithSegment(file, 0xE0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))
		if got := jpegOrientation(bytes.NewReader(file), int64(len(file))); got != 6 {
			t.Errorf("got %d, want 6", got)
		}
	})

	cases := []struct {
		name string
		file []byte
	}{
		{name: "no EXIF block", file: jpg},
		{name: "EXIF without orientation", file: imagetest.WithEXIF(jpg, imagetest.EXIF(binary.BigEndian, 0))},
		{name: "orientation out of range", file: imagetest.WithEXIF(jpg, imagetest.EXIF(binary.LittleEndian, 9))},
		{name: "orientation zero", file: imagetest.WithEXIF(jpg, patchOrientationEntry(0))},
		{name: "unknown byte order", file: imagetest.WithEXIF(jpg, append([]byte("XX"), imagetest.EXIF(binary.BigEndian, 6)[2:]...))},
		{name: "IFD0 offset beyond the block", file: imagetest.WithEXIF(jpg, patchIFDOffset(0xFFFFFF00))},
		{name: "IFD0 entry count beyond the block", file: imagetest.WithEXIF(jpg, patchEntryCount(0xFFFF))},
		{name: "orientation of another type", file: imagetest.WithEXIF(jpg, patchOrientationType(4))},
		{name: "APP1 segment longer than the file", file: cutAfterAPP1Header(imagetest.WithEXIF(jpg, imagetest.EXIF(binary.BigEndian, 6)))},
		{name: "APP1 segment shorter than its prefix", file: imagetest.WithSegment(jpg, 0xE1, []byte("Ex"))},
		{name: "not a JPEG", file: imagetest.PNG(4, 4)},
		{name: "empty file", file: nil},
	}
	for _, tc := range cases {
		t.Run("gives 1 for "+tc.name, func(t *testing.T) {
			if got := jpegOrientation(bytes.NewReader(tc.file), int64(len(tc.file))); got != 1 {
				t.Errorf("got %d, want 1", got)
			}
		})
	}
}

// exifBigEndian6 is the EXIF block of imagetest, big-endian, orientation 6: IFD0 at 8, its
// third entry the orientation.
func exifBigEndian6() []byte { return imagetest.EXIF(binary.BigEndian, 6) }

func patchIFDOffset(offset uint32) []byte {
	tiff := exifBigEndian6()
	binary.BigEndian.PutUint32(tiff[4:], offset)
	return tiff
}

func patchEntryCount(n uint16) []byte {
	tiff := exifBigEndian6()
	binary.BigEndian.PutUint16(tiff[8:], n)
	return tiff
}

func patchOrientationType(typ uint16) []byte {
	tiff := exifBigEndian6()
	binary.BigEndian.PutUint16(tiff[10+2*12+2:], typ)
	return tiff
}

func patchOrientationEntry(value uint16) []byte {
	tiff := exifBigEndian6()
	binary.BigEndian.PutUint16(tiff[10+2*12+8:], value)
	return tiff
}

// cutAfterAPP1Header keeps the SOI, the APP1 marker and its length, and a few bytes of its body:
// the segment declares more bytes than the file holds.
func cutAfterAPP1Header(file []byte) []byte {
	return file[:2+4+10]
}

func TestHEIFTransform(t *testing.T) {
	t.Run("reads the irot of a HEIC made by sips", func(t *testing.T) {
		file, err := os.ReadFile("../../testdata/images/rotated.heic")
		if err != nil {
			t.Fatal(err)
		}
		if got := heifTransform(bytes.NewReader(file), int64(len(file))); got != (transform{rotate: 3, mirror: noMirror}) {
			t.Errorf("got %+v, want rotate 3, no mirror", got)
		}
	})

	cases := []struct {
		name string
		file []byte
		want transform
	}{
		{name: "no transform", file: heif(1, 0, nil), want: transform{mirror: noMirror}},
		{name: "irot of the primary item", file: heif(1, 0, []prop{{"irot", 1}}), want: transform{rotate: 1, mirror: noMirror}},
		{name: "imir axis 1", file: heif(1, 0, []prop{{"imir", 1}}), want: transform{mirror: 1}},
		{name: "irot then imir axis 0", file: heif(1, 0, []prop{{"irot", 2}, {"imir", 0}}), want: transform{rotate: 2, mirror: 0}},
		{name: "a property of another item is ignored", file: heif(2, 0, []prop{{"irot", 1}}), want: transform{mirror: noMirror}},
		{name: "version 1 and 16-bit indices", file: heif(1, 1, []prop{{"irot", 3}}), want: transform{rotate: 3, mirror: noMirror}},
		{name: "no meta box", file: imagetest.Ftyp("heic", "mif1"), want: transform{mirror: noMirror}},
		{name: "truncated meta box", file: heif(1, 0, []prop{{"irot", 1}})[:60], want: transform{mirror: noMirror}},
		{name: "empty file", file: nil, want: transform{mirror: noMirror}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := heifTransform(bytes.NewReader(tc.file), int64(len(tc.file))); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}

	t.Run("an index beyond the properties is ignored", func(t *testing.T) {
		file := heif(1, 0, []prop{{"irot", 1}})
		// The association of the primary item is the last byte: point it past ipco.
		file[len(file)-1] = 0x05
		if got := heifTransform(bytes.NewReader(file), int64(len(file))); got != (transform{mirror: noMirror}) {
			t.Errorf("got %+v, want none", got)
		}
	})
}

type prop struct {
	typ   string
	value byte
}

// heif is an ftyp box and a meta box whose primary item is 1, whose ipco holds one ispe then
// props, and whose ipma associates item (1 or another) with every property. ipmaFlags 1 writes
// 16-bit indices; the version of ipma is 1 then, with 32-bit item ids.
func heif(item uint32, ipmaFlags byte, props []prop) []byte {
	box := func(typ string, body []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
		return append(append(out, typ...), body...)
	}
	ispe := box("ispe", []byte{0, 0, 0, 0, 0, 0, 0, 64, 0, 0, 0, 48})
	ipco := ispe
	for _, p := range props {
		ipco = append(ipco, box(p.typ, []byte{p.value})...)
	}
	ipma := []byte{ipmaFlags, 0, 0, ipmaFlags} // version, flags (bit 0: 16-bit indices)
	ipma = binary.BigEndian.AppendUint32(ipma, 1)
	if ipmaFlags == 1 {
		ipma = binary.BigEndian.AppendUint32(ipma, item)
	} else {
		ipma = binary.BigEndian.AppendUint16(ipma, uint16(item))
	}
	ipma = append(ipma, byte(1+len(props)))
	for i := range 1 + len(props) {
		if ipmaFlags == 1 {
			ipma = binary.BigEndian.AppendUint16(ipma, uint16(0x8000|(i+1)))
		} else {
			ipma = append(ipma, byte(0x80|(i+1)))
		}
	}
	pitm := box("pitm", []byte{0, 0, 0, 0, 0, 1})
	meta := append([]byte{0, 0, 0, 0}, pitm...)
	meta = append(meta, box("iprp", append(box("ipco", ipco), box("ipma", ipma)...))...)
	return append(imagetest.Ftyp("heic", "mif1", "heic"), box("meta", meta)...)
}

func TestOrient(t *testing.T) {
	// A 2×1 image: a at the left, b at the right.
	a := color.RGBA{R: 255, A: 255}
	b := color.RGBA{B: 255, A: 255}
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.SetRGBA(0, 0, a)
	src.SetRGBA(1, 0, b)

	cases := []struct {
		orientation int
		want        [][]color.RGBA // rows
	}{
		{1, [][]color.RGBA{{a, b}}},
		{2, [][]color.RGBA{{b, a}}},   // mirrored left-right
		{3, [][]color.RGBA{{b, a}}},   // half turn
		{4, [][]color.RGBA{{a, b}}},   // mirrored top-bottom
		{5, [][]color.RGBA{{a}, {b}}}, // transposed
		{6, [][]color.RGBA{{a}, {b}}}, // quarter turn clockwise
		{7, [][]color.RGBA{{b}, {a}}}, // transversed
		{8, [][]color.RGBA{{b}, {a}}}, // quarter turn anticlockwise
		{0, [][]color.RGBA{{a, b}}},   // unknown: untouched
		{9, [][]color.RGBA{{a, b}}},   // unknown: untouched
	}
	for _, tc := range cases {
		got := orient(src, tc.orientation)
		var rows [][]color.RGBA
		for y := range got.Bounds().Dy() {
			var row []color.RGBA
			for x := range got.Bounds().Dx() {
				row = append(row, got.RGBAAt(x, y))
			}
			rows = append(rows, row)
		}
		if !slices.EqualFunc(rows, tc.want, slices.Equal) {
			t.Errorf("orientation %d: got %v, want %v", tc.orientation, rows, tc.want)
		}
	}

	t.Run("a 2×2 image tells 5 from 6 and 7 from 8", func(t *testing.T) {
		// tl tr / bl br
		q := imagetest.Quadrants(2, 2)
		tl, tr, bl, br := imagetest.TopLeft, imagetest.TopRight, imagetest.BottomLeft, imagetest.BottomRight
		want := map[int][4]color.RGBA{
			5: {tl, bl, tr, br},
			6: {bl, tl, br, tr},
			7: {br, tr, bl, tl},
			8: {tr, br, tl, bl},
		}
		for o, w := range want {
			got := orient(q, o)
			corners := [4]color.RGBA{got.RGBAAt(0, 0), got.RGBAAt(1, 0), got.RGBAAt(0, 1), got.RGBAAt(1, 1)}
			if corners != w {
				t.Errorf("orientation %d: got %v, want %v", o, corners, w)
			}
		}
	})

	t.Run("the HEIF transform maps to the same turns as h265's", func(t *testing.T) {
		cases := []struct {
			tr   transform
			want []int // orientations applied in order
		}{
			{transform{rotate: 0, mirror: noMirror}, nil},
			{transform{rotate: 1, mirror: noMirror}, []int{8}},
			{transform{rotate: 2, mirror: noMirror}, []int{3}},
			{transform{rotate: 3, mirror: noMirror}, []int{6}},
			{transform{rotate: 0, mirror: 0}, []int{4}},
			{transform{rotate: 1, mirror: 1}, []int{8, 2}},
		}
		for _, tc := range cases {
			if got := tc.tr.orientations(); !slices.Equal(got, tc.want) {
				t.Errorf("%+v: got %v, want %v", tc.tr, got, tc.want)
			}
		}
	})
}
