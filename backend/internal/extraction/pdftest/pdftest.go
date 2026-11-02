// Package pdftest builds fictional PDF files for tests, in code: the repository is public and
// holds no PDF file (ADR-0027), no font program (so no font licence), and no hostile file that
// a scanner might flag. A Doc describes the document; WriteTo writes it with exact
// cross-reference offsets, streaming what is large (images, bodies over 50 MiB) instead of
// building it in memory. The generator imitates the producers the extraction meets: Word (a
// Tj per line with spaces), Chrome (a Type0 font, one TJ string per glyph and no space
// character), LaTeX (a move per word). Only tests import it.
package pdftest

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
)

// Font is the kind of font the text is written with.
type Font int

const (
	// Simple is a Type1 font with WinAnsiEncoding and /Widths, one byte per glyph.
	Simple Font = iota
	// TwoByte is a Type0 font, Identity-H, two bytes per glyph, /W widths and a ToUnicode
	// map, without embedded font program.
	TwoByte
)

// Placement is how the glyphs of a line are positioned.
type Placement int

const (
	// Spaces writes one Tj per line, its words separated by space characters.
	Spaces Placement = iota
	// Glyphs writes one TJ per line, each glyph its own string with a small kerning, and the
	// words separated by offsets of the text position only, without space character.
	Glyphs
	// WordMoves positions each word with its own Tm, without space character.
	WordMoves
)

// Encryption is the security handler of a document.
type Encryption int

const (
	// NoEncryption writes the document in clear.
	NoEncryption Encryption = iota
	// AES128 encrypts strings and streams with AES-128 (V 4, R 4); with an empty
	// UserPassword the document opens without password.
	AES128
	// AES256 writes an AES-256 encryption dictionary (V 5, R 6), which the reader refuses
	// before decrypting anything: the content is left in clear.
	AES256
)

// Page is one page of a Doc.
type Page struct {
	// Lines are the lines of text of the page, top to bottom.
	Lines []string
	// Image draws an image covering the page, as a scanned page does.
	Image bool
	// Broken makes the content stream undecodable: FlateDecode over bytes that are not zlib.
	Broken bool
	// Content, when set, writes the raw content stream instead of Lines (hostile pages); it is
	// compressed when the Doc is.
	Content func(w io.Writer) error
}

// Doc describes a PDF document.
type Doc struct {
	// Version is the version of the header, "1.7" when empty.
	Version string
	// Title is the raw string of /Info /Title; nil writes no title.
	Title []byte
	// BrokenInfo points the cross-reference entry of /Info at another object, so that the
	// metadata cannot be read while the pages can.
	BrokenInfo bool
	Font       Font
	Placement  Placement
	// Compress writes the content streams with FlateDecode.
	Compress   bool
	Encryption Encryption
	// UserPassword is the password needed to open an AES-128 document; "" opens it without.
	UserPassword string
	Pages        []Page
	// ImageBytes adds to every page an image of that many pseudo-random bytes, which the page
	// does not draw: the size of a large document, read by nothing.
	ImageBytes int
	// TreeCycle lists the root of the page tree among its own kids.
	TreeCycle bool
	// RepeatKids lists every page twice among the kids of the root: a tree that is not one.
	RepeatKids bool
	// ExtraKids is written as is at the end of the kids of the root ("null"…).
	ExtraKids string
	// PrevLoop points the /Prev of the cross-reference section at itself.
	PrevLoop bool
}

const (
	fontSize   = 11
	lineHeight = 14
	leftMargin = 72
	topLine    = 770
	spaceWidth = 278
	ownerPass  = "fictional-owner-password"
)

// fileID is the /ID of every generated document: its value matters only to the encryption.
var fileID = []byte("hippo-pdftest-id")

// Bytes returns the document; only for documents that fit in memory.
func (d Doc) Bytes() []byte {
	var b bytes.Buffer
	if _, err := d.WriteTo(&b); err != nil {
		panic(err) // a bytes.Buffer does not fail
	}
	return b.Bytes()
}

// WriteTo writes the document to w.
func (d Doc) WriteTo(w io.Writer) (int64, error) {
	pw := &writer{w: bufio.NewWriterSize(w, 64<<10), doc: d}
	err := pw.document()
	if err == nil {
		err = pw.w.Flush()
	}
	return pw.n, err
}

// writer writes the objects of a document and remembers their offsets.
type writer struct {
	w       *bufio.Writer
	doc     Doc
	n       int64
	offsets []int64 // by object number - 1
	err     error
	key     []byte // the file key of an AES-128 document
	cids    map[rune]int
	runes   []rune // by CID - 1
}

func (p *writer) write(b []byte) {
	if p.err != nil {
		return
	}
	n, err := p.w.Write(b)
	p.n += int64(n)
	p.err = err
}

func (p *writer) printf(format string, args ...any) { p.write(fmt.Appendf(nil, format, args...)) }

// Object numbers: fixed objects first, then three per page (page, content, image) and one more
// per page with ImageBytes.
const (
	objCatalog = 1 + iota
	objPages
	objFont
	objDescendant
	objToUnicode
	objInfo
	objEncrypt
	objImage // the small image drawn by scanned pages
	firstPageObj
)

func pageObj(i int) int    { return firstPageObj + 3*i }
func contentObj(i int) int { return firstPageObj + 3*i + 1 }
func fillerObj(i int) int  { return firstPageObj + 3*i + 2 }

func (p *writer) document() error {
	d := p.doc
	version := d.Version
	if version == "" {
		version = "1.7"
	}
	p.printf("%%PDF-%s\n%%\xe2\xe3\xcf\xd3\n", version)
	if d.Font == TwoByte {
		p.assignCIDs()
	}
	if d.Encryption == AES128 {
		p.key = fileKey(d.UserPassword)
	}
	total := firstPageObj + 3*len(d.Pages) - 1
	p.offsets = make([]int64, total)

	p.object(objCatalog, []byte(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", objPages)))
	var kids strings.Builder
	if d.TreeCycle {
		fmt.Fprintf(&kids, "%d 0 R ", objPages)
	}
	for i := range d.Pages {
		fmt.Fprintf(&kids, "%d 0 R ", pageObj(i))
		if d.RepeatKids {
			fmt.Fprintf(&kids, "%d 0 R ", pageObj(i))
		}
	}
	kids.WriteString(d.ExtraKids)
	p.object(objPages, []byte(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), len(d.Pages))))
	p.fonts()
	info := fmt.Appendf(nil, "<< /Producer %s", p.stringObj(objInfo, []byte("pdftest")))
	if d.Title != nil {
		info = fmt.Appendf(info, " /Title %s", p.stringObj(objInfo, d.Title))
	}
	p.object(objInfo, append(info, " >>"...))
	p.encryptDict()
	p.stream(objImage, "/Type /XObject /Subtype /Image /Width 8 /Height 8 /ColorSpace /DeviceGray /BitsPerComponent 8",
		func(w io.Writer) error { _, err := w.Write(bytes.Repeat([]byte{0x80}, 64)); return err }, false)
	for i, page := range d.Pages {
		p.page(i, page)
	}
	if d.BrokenInfo {
		p.offsets[objInfo-1] = p.offsets[objCatalog-1]
	}
	p.xref()
	return p.err
}

// object writes object num with its body.
func (p *writer) object(num int, body []byte) {
	p.offsets[num-1] = p.n
	p.printf("%d 0 obj\n", num)
	p.write(body)
	p.write([]byte("\nendobj\n"))
}

// stream writes object num as a stream of the given dictionary entries, its data written by
// data, compressed when compress, encrypted with the document.
func (p *writer) stream(num int, entries string, data func(io.Writer) error, compress bool) {
	if p.err != nil {
		return
	}
	var body bytes.Buffer
	var out io.Writer = &body
	var zw *zlib.Writer
	if compress {
		zw = zlib.NewWriter(&body)
		out = zw
		entries += " /Filter /FlateDecode"
	}
	if err := data(out); err != nil {
		p.err = err
		return
	}
	if zw != nil {
		if err := zw.Close(); err != nil {
			p.err = err
			return
		}
	}
	encoded := p.encrypt(num, body.Bytes())
	p.offsets[num-1] = p.n
	p.printf("%d 0 obj\n<< %s /Length %d >>\nstream\n", num, entries, len(encoded))
	p.write(encoded)
	p.write([]byte("\nendstream\nendobj\n"))
}

// filler writes object num as an image of size pseudo-random bytes, streamed.
func (p *writer) filler(num, size, seed int) {
	if p.err != nil {
		return
	}
	p.offsets[num-1] = p.n
	if p.key != nil {
		size += 16 + (16 - size%16) // IV and padding of the AES encryption
	}
	p.printf("%d 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length %d >>\nstream\n",
		num, size, size)
	rng := rand.New(rand.NewPCG(uint64(seed), 7))
	chunk := make([]byte, 32<<10)
	for left := size; left > 0 && p.err == nil; left -= len(chunk) {
		for i := 0; i+8 <= len(chunk); i += 8 {
			binary.LittleEndian.PutUint64(chunk[i:], rng.Uint64())
		}
		p.write(chunk[:min(left, len(chunk))])
	}
	p.write([]byte("\nendstream\nendobj\n"))
}

func (p *writer) page(i int, page Page) {
	d := p.doc
	resources := fmt.Sprintf("/Font << /F1 %d 0 R >> /XObject << /Im1 %d 0 R", objFont, objImage)
	if d.ImageBytes > 0 {
		resources += fmt.Sprintf(" /Fill %d 0 R", fillerObj(i))
	}
	resources += " >>"
	p.object(pageObj(i), []byte(fmt.Sprintf(
		"<< /Type /Page /Parent %d 0 R /MediaBox [0 0 595 842] /Resources << %s >> /Contents %d 0 R >>",
		objPages, resources, contentObj(i))))
	switch {
	case page.Broken:
		p.stream(contentObj(i), "/Filter /FlateDecode", func(w io.Writer) error {
			_, err := w.Write([]byte("these bytes are not a zlib stream, the page cannot be decoded"))
			return err
		}, false)
	case page.Content != nil:
		p.stream(contentObj(i), "", page.Content, d.Compress)
	default:
		p.stream(contentObj(i), "", func(w io.Writer) error { return p.content(w, page) }, d.Compress)
	}
	if d.ImageBytes > 0 {
		p.filler(fillerObj(i), d.ImageBytes, i)
	} else {
		p.object(fillerObj(i), []byte("null"))
	}
}

// content writes the content stream of a page of lines.
func (p *writer) content(w io.Writer, page Page) error {
	var b bytes.Buffer
	if page.Image {
		b.WriteString("q 595 0 0 842 0 0 cm /Im1 Do Q\n")
	}
	for n, line := range page.Lines {
		y := topLine - n*lineHeight
		fmt.Fprintf(&b, "BT /F1 %d Tf\n", fontSize)
		switch p.doc.Placement {
		case Spaces:
			fmt.Fprintf(&b, "1 0 0 1 %d %d Tm %s Tj\n", leftMargin, y, p.encode(line))
		case Glyphs:
			fmt.Fprintf(&b, "1 0 0 1 %d %d Tm [", leftMargin, y)
			for k, word := range strings.Fields(line) {
				if k > 0 {
					fmt.Fprintf(&b, " -%d ", spaceWidth)
				}
				for g, r := range []rune(word) {
					if g > 0 {
						fmt.Fprintf(&b, " %d ", []int{8, -8}[g%2]) // a kerning of a hundredth of an em
					}
					b.WriteString(p.encode(string(r)))
				}
			}
			b.WriteString("] TJ\n")
		case WordMoves:
			x := float64(leftMargin)
			for _, word := range strings.Fields(line) {
				fmt.Fprintf(&b, "1 0 0 1 %.2f %d Tm %s Tj\n", x, y, p.encode(word))
				for _, r := range word {
					x += float64(glyphWidth(r)) * fontSize / 1000
				}
				x += spaceWidth * fontSize / 1000.0
			}
		}
		b.WriteString("ET\n")
		if _, err := w.Write(b.Bytes()); err != nil {
			return err
		}
		b.Reset()
	}
	_, err := w.Write(b.Bytes())
	return err
}

// glyphWidth is the advance of a glyph, in thousandths of the font size: varied, so that the
// reader must use the widths of the font to place the glyphs.
func glyphWidth(r rune) int {
	if r == ' ' {
		return spaceWidth
	}
	return 420 + int(r)*37%240
}

// encode writes text as a string of the font: a literal string of WinAnsi bytes for Simple, a
// hexadecimal string of CIDs for TwoByte.
func (p *writer) encode(text string) string {
	var b strings.Builder
	if p.doc.Font == TwoByte {
		b.WriteByte('<')
		for _, r := range text {
			fmt.Fprintf(&b, "%04X", p.cids[r])
		}
		b.WriteByte('>')
		return b.String()
	}
	b.WriteByte('(')
	for _, c := range winAnsi(text) {
		switch c {
		case '(', ')', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			if c < 0x20 || c >= 0x7f {
				fmt.Fprintf(&b, "\\%03o", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte(')')
	return b.String()
}

// winAnsiHigh maps the characters of WinAnsiEncoding between 0x80 and 0x9F that the tests use.
var winAnsiHigh = map[rune]byte{'€': 0x80, '…': 0x85, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94, '–': 0x96, '—': 0x97, 'œ': 0x9c, 'Œ': 0x8c}

func winAnsi(text string) []byte {
	out := make([]byte, 0, len(text))
	for _, r := range text {
		switch {
		case r < 0x80 || (r >= 0xa0 && r <= 0xff):
			out = append(out, byte(r))
		case winAnsiHigh[r] != 0:
			out = append(out, winAnsiHigh[r])
		default:
			out = append(out, '?')
		}
	}
	return out
}

// assignCIDs gives a CID to every character of the document, in order of first use.
func (p *writer) assignCIDs() {
	p.cids = map[rune]int{}
	for _, page := range p.doc.Pages {
		for _, line := range page.Lines {
			for _, r := range line {
				if _, ok := p.cids[r]; !ok {
					p.runes = append(p.runes, r)
					p.cids[r] = len(p.runes)
				}
			}
		}
	}
}

func (p *writer) fonts() {
	if p.doc.Font == Simple {
		widths := make([]string, 0, 224)
		for c := 32; c <= 255; c++ {
			widths = append(widths, fmt.Sprint(glyphWidth(rune(c))))
		}
		// The widths of the glyphs above 0x7F follow their code, not their character: the
		// generator only uses glyphWidth through characters below 0x80 and Latin-1 ones.
		p.object(objFont, []byte(fmt.Sprintf(
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 255 /Widths [%s] >>",
			strings.Join(widths, " "))))
		p.object(objDescendant, []byte("null"))
		p.object(objToUnicode, []byte("null"))
		return
	}
	p.object(objFont, []byte(fmt.Sprintf(
		"<< /Type /Font /Subtype /Type0 /BaseFont /FictiveSans /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>",
		objDescendant, objToUnicode)))
	widths := make([]string, len(p.runes))
	for i, r := range p.runes {
		widths[i] = fmt.Sprint(glyphWidth(r))
	}
	p.object(objDescendant, []byte(fmt.Sprintf(
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /FictiveSans /CIDSystemInfo << /Registry %s /Ordering %s /Supplement 0 >> /DW 500 /W [1 [%s]] >>",
		p.stringObj(objDescendant, []byte("Adobe")), p.stringObj(objDescendant, []byte("Identity")), strings.Join(widths, " "))))
	p.stream(objToUnicode, "", func(w io.Writer) error {
		var b bytes.Buffer
		b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n" +
			"/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n" +
			"/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n" +
			"1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
		for start := 0; start < len(p.runes); start += 100 {
			chunk := p.runes[start:min(start+100, len(p.runes))]
			fmt.Fprintf(&b, "%d beginbfchar\n", len(chunk))
			for i, r := range chunk {
				fmt.Fprintf(&b, "<%04X> <", start+i+1)
				for _, u := range utf16Units(r) {
					fmt.Fprintf(&b, "%04X", u)
				}
				b.WriteString(">\n")
			}
			b.WriteString("endbfchar\n")
		}
		b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
		_, err := w.Write(b.Bytes())
		return err
	}, p.doc.Compress)
}

func utf16Units(r rune) []uint16 {
	if r < 0x10000 {
		return []uint16{uint16(r)}
	}
	r -= 0x10000
	return []uint16{uint16(0xd800 + (r >> 10)), uint16(0xdc00 + (r & 0x3ff))}
}

// stringObj writes a string of object num, encrypted with the document, as a hexadecimal
// string.
func (p *writer) stringObj(num int, s []byte) []byte {
	return fmt.Appendf(nil, "<%X>", p.encrypt(num, s))
}

func (p *writer) encryptDict() {
	switch p.doc.Encryption {
	case NoEncryption:
		p.object(objEncrypt, []byte("null"))
	case AES128:
		o := ownerKey(p.doc.UserPassword)
		u := userKey(p.key)
		p.object(objEncrypt, []byte(fmt.Sprintf(
			"<< /Filter /Standard /V 4 /R 4 /Length 128 /CF << /StdCF << /CFM /AESV2 /AuthEvent /DocOpen /Length 16 >> >> "+
				"/StmF /StdCF /StrF /StdCF /O <%X> /U <%X> /P %d >>", o, u, permissions)))
	case AES256:
		// The values of an AES-256 dictionary have the sizes of the standard; the reader
		// refuses the 256-bit key before it uses any of them.
		p.object(objEncrypt, []byte(fmt.Sprintf(
			"<< /Filter /Standard /V 5 /R 6 /Length 256 /CF << /StdCF << /CFM /AESV3 /AuthEvent /DocOpen /Length 32 >> >> "+
				"/StmF /StdCF /StrF /StdCF /O <%X> /U <%X> /OE <%X> /UE <%X> /Perms <%X> /P %d >>",
			bytes.Repeat([]byte{1}, 48), bytes.Repeat([]byte{2}, 48), bytes.Repeat([]byte{3}, 32),
			bytes.Repeat([]byte{4}, 32), bytes.Repeat([]byte{5}, 16), permissions)))
	}
}

func (p *writer) xref() {
	start := p.n
	p.printf("xref\n0 %d\n0000000000 65535 f \n", len(p.offsets)+1)
	for _, off := range p.offsets {
		p.printf("%010d 00000 n \n", off)
	}
	trailer := fmt.Sprintf("/Size %d /Root %d 0 R /Info %d 0 R /ID [<%X> <%X>]", len(p.offsets)+1, objCatalog, objInfo, fileID, fileID)
	if p.doc.Encryption != NoEncryption {
		trailer += fmt.Sprintf(" /Encrypt %d 0 R", objEncrypt)
	}
	if p.doc.PrevLoop {
		trailer += fmt.Sprintf(" /Prev %d", start)
	}
	p.printf("trailer\n<< %s >>\nstartxref\n%d\n%%%%EOF\n", trailer, start)
}

// Standard security handler, PDF 32000-1:2008 §7.6.3 (revision 4, 128-bit key).

const permissions = -1044

var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func padded(password string) []byte {
	return append([]byte(password), passwordPad...)[:32]
}

// ownerKey is the /O entry (algorithm 3): the user password encrypted under the owner
// password of every generated document.
func ownerKey(userPassword string) []byte {
	sum := md5.Sum(padded(ownerPass))
	key := sum[:]
	for range 50 {
		next := md5.Sum(key)
		key = next[:]
	}
	o := padded(userPassword)
	xorRounds(key, o)
	return o
}

// fileKey is the encryption key of the document (algorithm 2).
func fileKey(userPassword string) []byte {
	h := md5.New()
	h.Write(padded(userPassword))
	h.Write(ownerKey(userPassword))
	var p [4]byte
	perms := int32(permissions)
	binary.LittleEndian.PutUint32(p[:], uint32(perms))
	h.Write(p[:])
	h.Write(fileID)
	key := h.Sum(nil)
	for range 50 {
		next := md5.Sum(key[:16])
		key = next[:]
	}
	return key[:16]
}

// userKey is the /U entry (algorithm 5) for the file key.
func userKey(key []byte) []byte {
	h := md5.New()
	h.Write(passwordPad)
	h.Write(fileID)
	u := h.Sum(nil)
	xorRounds(key, u)
	return append(u, bytes.Repeat([]byte{0}, 16)...)
}

// xorRounds encrypts data with RC4 under key, then 19 times under key XOR i.
func xorRounds(key, data []byte) {
	for i := range 20 {
		k := slices.Clone(key)
		for j := range k {
			k[j] ^= byte(i)
		}
		c, err := rc4.NewCipher(k)
		if err != nil {
			panic(err)
		}
		c.XORKeyStream(data, data)
	}
}

// encrypt encrypts data of object num with AES-128 for an AES128 document: the object key,
// a fixed IV of the object, PKCS#7 padding. Other documents are written in clear.
func (p *writer) encrypt(num int, data []byte) []byte {
	if p.key == nil {
		return data
	}
	h := md5.New()
	h.Write(p.key)
	h.Write([]byte{byte(num), byte(num >> 8), byte(num >> 16), 0, 0})
	h.Write([]byte("sAlT"))
	block, err := aes.NewCipher(h.Sum(nil)[:16])
	if err != nil {
		panic(err)
	}
	pad := 16 - len(data)%16
	plain := append(slices.Clone(data), bytes.Repeat([]byte{byte(pad)}, pad)...)
	iv := md5.Sum([]byte(fmt.Sprint("iv", num)))
	out := append(slices.Clone(iv[:]), make([]byte, len(plain))...)
	cipher.NewCBCEncrypter(block, iv[:]).CryptBlocks(out[16:], plain)
	return out
}
