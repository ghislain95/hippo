package extraction

import (
	"math"
	"unicode"
	"unicode/utf8"

	lpdf "github.com/ledongthuc/pdf"
)

// The layout layer turns the content streams of a PDF into text. github.com/ledongthuc/pdf
// parses the file, decodes its filters, its encryption and its fonts; Page.GetPlainText keeps
// the whole text of a page in memory and drops the spaces that only the positioning of the
// glyphs gives. This layer follows the operators itself (pdf.Interpret): the text matrix, the
// widths of the glyphs, the spacing operators. It writes a space where a glyph starts away from
// the end of the previous one, a line break where the baseline moves, and hands every glyph to
// a textSink, which keeps the text up to its limit and counts the words of everything
// (design.md, "Library and layout layer").
const (
	// spaceGap is the gap between two glyphs, in font sizes, beyond which they are two words.
	spaceGap = 0.15
	// lineShift is the move of the baseline, in font sizes, beyond which a glyph starts a line.
	lineShift = 0.5
	// maxFormDepth bounds the nesting of form XObjects followed.
	maxFormDepth = 4
	// maxCIDWidths bounds the entries read from the /W array of a CID font.
	maxCIDWidths = 65_536
	// defaultGlyphWidth is the advance of a glyph whose font gives none (the standard 14 fonts
	// without /Widths), in thousandths of the font size.
	defaultGlyphWidth = 500
)

// matrix is a PDF transformation matrix [a b c d e f].
type matrix [6]float64

var identityMatrix = matrix{1, 0, 0, 1, 0, 0}

func (m matrix) mul(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func translation(x, y float64) matrix { return matrix{1, 0, 0, 1, x, y} }

// pdfFont is what the layout needs of a font: its decoding and the widths of its glyphs.
type pdfFont struct {
	encoding lpdf.TextEncoding
	codeLen  int // bytes per code: 1 for a simple font, 2 for a Type0 font
	first    int
	widths   []float64
	cid      map[int]float64 // widths of a CID font
	cidWidth float64         // /DW of a CID font
}

func loadFont(v lpdf.Value) *pdfFont {
	font := &lpdf.Font{V: v}
	f := &pdfFont{encoding: font.Encoder(), codeLen: 1}
	if v.Key("Subtype").Name() == "Type0" {
		f.codeLen = 2
		descendant := v.Key("DescendantFonts").Index(0)
		f.cidWidth = 1000
		if dw := descendant.Key("DW"); dw.Kind() == lpdf.Integer || dw.Kind() == lpdf.Real {
			f.cidWidth = dw.Float64()
		}
		f.cid = map[int]float64{}
		w := descendant.Key("W")
		for i := 0; i < w.Len() && len(f.cid) < maxCIDWidths; {
			start := int(w.Index(i).Int64())
			if i+1 < w.Len() && w.Index(i+1).Kind() == lpdf.Array {
				list := w.Index(i + 1)
				for j := 0; j < list.Len() && len(f.cid) < maxCIDWidths; j++ {
					f.cid[start+j] = list.Index(j).Float64()
				}
				i += 2
				continue
			}
			if i+2 >= w.Len() {
				break
			}
			end, width := int(w.Index(i+1).Int64()), w.Index(i+2).Float64()
			for c := start; c <= end && len(f.cid) < maxCIDWidths; c++ {
				f.cid[c] = width
			}
			i += 3
		}
		return f
	}
	f.first = int(v.Key("FirstChar").Int64())
	widths := v.Key("Widths")
	for i := 0; i < widths.Len() && i < 256; i++ {
		f.widths = append(f.widths, widths.Index(i).Float64())
	}
	return f
}

// width is the advance of code, in thousandths of the font size.
func (f *pdfFont) width(code int) float64 {
	if f.cid != nil {
		if w, ok := f.cid[code]; ok {
			return w
		}
		return f.cidWidth
	}
	if i := code - f.first; i >= 0 && i < len(f.widths) {
		return f.widths[i]
	}
	return defaultGlyphWidth
}

// textState is the graphics and text state the layout follows.
type textState struct {
	ctm                              matrix
	font                             *pdfFont
	size, charSpace, wordSpace, rise float64
	scale, leading                   float64
}

// layout lays out the pages of one document into its sink.
type layout struct {
	sink *textSink
	// The end of the previous glyph, in device space, and its size.
	haveLast            bool
	lastX, lastY, lastH float64
}

// page lays out one page: its content stream (or array of streams) with its resources.
func (l *layout) page(contents, resources lpdf.Value) {
	l.haveLast = false
	l.sink.separator(sepPage)
	l.run(contents, resources, textState{ctm: identityMatrix, scale: 1}, 0)
}

// glyph places one decoded glyph whose origin is (x, y) in device space, of size h, ending at
// endX.
func (l *layout) glyph(text string, x, y, h, endX float64) {
	h = math.Max(h, 1)
	if l.haveLast {
		size := math.Max(h, l.lastH)
		switch {
		case math.Abs(y-l.lastY) > lineShift*size:
			l.sink.separator(sepLine)
		case x-l.lastX > spaceGap*size || x-l.lastX < -size:
			l.sink.separator(sepSpace)
		}
	}
	l.sink.text(text)
	l.haveLast, l.lastX, l.lastY, l.lastH = true, endX, y, h
}

// run interprets a content stream; forms are followed maxFormDepth levels deep.
func (l *layout) run(stream, resources lpdf.Value, g textState, depth int) {
	var tm, tlm matrix
	var saved []textState
	fonts := map[string]*pdfFont{}
	show := func(raw string) {
		if g.font == nil {
			g.font = &pdfFont{codeLen: 1}
		}
		f := g.font
		for i := 0; i+f.codeLen <= len(raw); i += f.codeLen {
			code := 0
			for k := range f.codeLen {
				code = code<<8 | int(raw[i+k])
			}
			text := raw[i : i+f.codeLen]
			if f.encoding != nil {
				text = f.encoding.Decode(text)
			}
			trm := matrix{g.size * g.scale, 0, 0, g.size, 0, g.rise}.mul(tm).mul(g.ctm)
			advance := f.width(code)/1000*g.size + g.charSpace
			if f.codeLen == 1 && raw[i] == ' ' {
				advance += g.wordSpace
			}
			next := translation(advance*g.scale, 0).mul(tm)
			if text != "" {
				l.glyph(text, trm[4], trm[5], math.Hypot(trm[2], trm[3]), next.mul(g.ctm)[4])
			}
			tm = next
		}
	}
	lpdf.Interpret(stream, func(stack *lpdf.Stack, op string) {
		args := make([]lpdf.Value, stack.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = stack.Pop()
		}
		num := func(i int) float64 {
			if i < len(args) {
				return args[i].Float64()
			}
			return 0
		}
		six := func() matrix { return matrix{num(0), num(1), num(2), num(3), num(4), num(5)} }
		switch op {
		case "q":
			saved = append(saved, g)
		case "Q":
			if n := len(saved); n > 0 {
				g, saved = saved[n-1], saved[:n-1]
			}
		case "cm":
			if len(args) == 6 {
				g.ctm = six().mul(g.ctm)
			}
		case "BT":
			tm, tlm = identityMatrix, identityMatrix
		case "Tf":
			if len(args) == 2 {
				name := args[0].Name()
				f, ok := fonts[name]
				if !ok {
					f = loadFont(resources.Key("Font").Key(name))
					fonts[name] = f
				}
				g.font, g.size = f, num(1)
			}
		case "Tc":
			g.charSpace = num(0)
		case "Tw":
			g.wordSpace = num(0)
		case "Tz":
			g.scale = num(0) / 100
		case "TL":
			g.leading = num(0)
		case "Ts":
			g.rise = num(0)
		case "Td", "TD":
			if op == "TD" {
				g.leading = -num(1)
			}
			tlm = translation(num(0), num(1)).mul(tlm)
			tm = tlm
		case "Tm":
			if len(args) == 6 {
				tlm = six()
				tm = tlm
			}
		case "T*":
			tlm = translation(0, -g.leading).mul(tlm)
			tm = tlm
		case "'", "\"":
			if op == "\"" && len(args) == 3 {
				g.wordSpace, g.charSpace = num(0), num(1)
			}
			tlm = translation(0, -g.leading).mul(tlm)
			tm = tlm
			if len(args) > 0 {
				show(args[len(args)-1].RawString())
			}
		case "Tj":
			if len(args) == 1 {
				show(args[0].RawString())
			}
		case "TJ":
			if len(args) == 1 {
				array := args[0]
				for i := 0; i < array.Len(); i++ {
					item := array.Index(i)
					if item.Kind() == lpdf.String {
						show(item.RawString())
					} else {
						tm = translation(-item.Float64()/1000*g.size*g.scale, 0).mul(tm)
					}
				}
			}
		case "Do":
			if depth < maxFormDepth && len(args) == 1 {
				form := resources.Key("XObject").Key(args[0].Name())
				if form.Key("Subtype").Name() != "Form" {
					return // an image: its data is never read
				}
				formResources := form.Key("Resources")
				if formResources.IsNull() {
					formResources = resources
				}
				inner := g
				if m := form.Key("Matrix"); m.Len() == 6 {
					inner.ctm = matrix{
						m.Index(0).Float64(), m.Index(1).Float64(), m.Index(2).Float64(),
						m.Index(3).Float64(), m.Index(4).Float64(), m.Index(5).Float64(),
					}.mul(g.ctm)
				}
				l.run(form, formResources, inner, depth+1)
			}
		}
	})
}

// separator is what separates two glyphs, in increasing strength.
type separator int

const (
	sepNone separator = iota
	sepSpace
	sepLine
	sepPage
)

var separatorText = [...]string{sepNone: "", sepSpace: " ", sepLine: "\n", sepPage: "\n\n"}

// ligatures are expanded to their letters, so that a word reads and counts as typed.
var ligatures = map[rune]string{
	'ﬀ': "ff", 'ﬁ': "fi", 'ﬂ': "fl", 'ﬃ': "ffi", 'ﬄ': "ffl", 'ﬅ': "st", 'ﬆ': "st",
}

const softHyphen = '­'

// textSink receives the glyphs of a document in reading order. It keeps the text, cleaned
// without losing a letter, until it holds keepLimit code points, and counts the words of the
// whole document as strings.Fields would on the whole text. The kept text only grows: what was
// committed of it is never written again (pdf.go).
type textSink struct {
	keepLimit int
	// commit is called when the kept text reaches keepLimit, and every wordCommitInterval
	// words.
	commit func()

	kept       []byte
	keptRunes  int
	full       bool
	words      int64
	pending    separator
	started    bool // a visible character was written
	last       rune // the last visible character, and the one before it
	beforeLast rune
	hyphen     bool // a soft hyphen was met since the last visible character
}

// separator records a separator before the next visible character; the strongest wins.
func (s *textSink) separator(sep separator) {
	s.pending = max(s.pending, sep)
}

// text adds the decoded text of one glyph.
func (s *textSink) text(text string) {
	for _, r := range text {
		if expanded, ok := ligatures[r]; ok {
			for _, letter := range expanded {
				s.visible(letter)
			}
			continue
		}
		switch {
		case r == softHyphen:
			s.hyphen = true
		case r == '\n' || r == '\r':
			s.separator(sepLine)
		case unicode.IsSpace(r):
			s.separator(sepSpace)
		case unicode.IsControl(r) || r == utf8.RuneError:
			// Control characters and glyphs the font cannot decode carry no text.
		default:
			s.visible(r)
		}
	}
}

// visible writes a visible character, after the separator it waits for. A line that ends with
// a soft hyphen, or with a hyphen right after a letter, is joined to the next one: the word is
// whole, counted once, and no letter is dropped (the hyphen is kept, the soft one is not).
func (s *textSink) visible(r rune) {
	sep := s.pending
	if !s.started {
		sep = sepNone
	} else if sep == sepLine && (s.hyphen || s.last == '-' && unicode.IsLetter(s.beforeLast)) {
		sep = sepNone
	}
	if sep != sepNone || !s.started {
		s.words++
		if s.words%wordCommitInterval == 0 && s.commit != nil {
			defer s.commit()
		}
	}
	s.keep(separatorText[sep])
	var buf [utf8.UTFMax]byte
	s.keep(string(buf[:utf8.EncodeRune(buf[:], r)]))
	s.pending, s.hyphen, s.started = sepNone, false, true
	s.beforeLast, s.last = s.last, r
}

func (s *textSink) keep(text string) {
	if s.full || text == "" {
		return
	}
	s.kept = append(s.kept, text...)
	s.keptRunes += utf8.RuneCountInString(text)
	if s.keptRunes >= s.keepLimit {
		s.full = true
		if s.commit != nil {
			s.commit()
		}
	}
}
