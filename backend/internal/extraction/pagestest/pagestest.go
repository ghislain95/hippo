// Package pagestest builds fictional HTML pages for tests, in code, when their size matters more
// than their text: an article of a given length, a page of several megabytes. The hand-written
// pages are files in backend/testdata/pages. No page of the repository is a real page nor holds
// personal content (ADR-0027). Only tests import it.
package pagestest

import (
	"bytes"
	"fmt"
	"html"
	"strings"
)

// vocabulary is prefix-free, so that a word cut in its middle is never another word.
var vocabulary = strings.Fields(`alpha bravo charlie delta echo foxtrot golf hotel india juliett
	kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray yankee zulu`)

// IsWord tells whether w, without a final period, is a whole word of the generated articles.
func IsWord(w string) bool {
	w = strings.TrimSuffix(w, ".")
	for _, v := range vocabulary {
		if v == w {
			return true
		}
	}
	return false
}

// Article is an HTML page titled title whose article is the paragraph text.
func Article(title, text string) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>%s</title></head><body><article>",
		html.EscapeString(title))
	fmt.Fprintf(&b, "<p>%s</p>", html.EscapeString(text))
	b.WriteString("</article></body></html>")
	return b.Bytes()
}

// LongArticle is an article page whose main content is about length characters of words of
// the vocabulary, in paragraphs of 100 words; it also returns the number of words.
func LongArticle(length int) ([]byte, int) {
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Long article</title></head><body><article>`)
	written, words := 0, 0
	for written < length {
		var p strings.Builder
		for i := 0; i < 100; i++ {
			if i > 0 {
				p.WriteByte(' ')
			}
			p.WriteString(vocabulary[(words+i*7)%len(vocabulary)])
		}
		p.WriteByte('.')
		words += 100
		written += p.Len() + 2 // the blank line between paragraphs
		b.WriteString("<p>")
		b.WriteString(p.String())
		b.WriteString("</p>\n")
	}
	b.WriteString("</article></body></html>")
	return b.Bytes(), words
}

// Large is an article page of about size bytes made of nested blocks, a heavy but realistic
// document for Readability: each block is a div holding a paragraph of about 500 bytes, so that
// a 5 MB page stays under 20 000 elements.
func Large(size int) []byte {
	var b bytes.Buffer
	b.Grow(size + 1024)
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Large page</title>` +
		`<meta property="og:site_name" content="Example Blog"></head><body><main><article>`)
	for i := 0; b.Len() < size; i++ {
		b.WriteString(`<div class="block"><p>`)
		for j := 0; j < 80; j++ {
			b.WriteString(vocabulary[(i+j)%len(vocabulary)])
			b.WriteByte(' ')
		}
		b.WriteString("</p></div>\n")
	}
	b.WriteString("</article></main></body></html>")
	return b.Bytes()
}

// Tiny is a page of about size bytes made of tiny elements only, the worst case for the
// memory of the HTML tree: Readability refuses it, but only after it is parsed.
func Tiny(size int) []byte {
	var b bytes.Buffer
	b.Grow(size + 1024)
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Tiny elements</title></head><body>`)
	for b.Len() < size {
		b.WriteString("<i>x</i>")
	}
	b.WriteString("</body></html>")
	return b.Bytes()
}

// Reopened is the page of the review that defeats a plain count of tags: formatting elements
// left open (forty <b>, each with its own id) are rebuilt by the HTML parser before each text and
// each element that follows, so that blocks of a few tags each become thousands of elements.
// blocks is the number of </div><div>x that follow.
func Reopened(blocks int) []byte {
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Reopened</title></head><body><div>`)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, `<b id="%d">`, i)
	}
	for i := 0; i < blocks; i++ {
		b.WriteString("</div><div>x")
	}
	b.WriteString("</div></body></html>")
	return b.Bytes()
}

// reopenedHead opens a block and forty formatting elements, each with its own id, that stay
// active: the parser rebuilds them before each text and element that follows.
func reopenedHead(b *bytes.Buffer) {
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Reopened</title></head><body><div>`)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(b, `<b id="%d">`, i)
	}
}

func reopenedBlocks(b *bytes.Buffer, blocks int) {
	for i := 0; i < blocks; i++ {
		b.WriteString("</div><div>x")
	}
	b.WriteString("</div></body></html>")
}

// StrayEnds is Reopened with forty </i> after the <b>: end tags of another formatting element,
// ignored by the parser, which a count of openings minus closings takes for closings.
func StrayEnds(blocks int) []byte {
	var b bytes.Buffer
	reopenedHead(&b)
	for i := 0; i < 40; i++ {
		b.WriteString("</i>")
	}
	reopenedBlocks(&b, blocks)
	return b.Bytes()
}

// ScopedEnds is Reopened with forty </b> inside an <object>: a scope boundary, beyond which
// the parser ignores them, while the <b> opened before it stay active.
func ScopedEnds(blocks int) []byte {
	var b bytes.Buffer
	reopenedHead(&b)
	b.WriteString("<object>")
	for i := 0; i < 40; i++ {
		b.WriteString("</b>")
	}
	b.WriteString("</object>")
	reopenedBlocks(&b, blocks)
	return b.Bytes()
}

// FormattedArticle is a fictional article whose paragraphs hold closed links, bold and
// emphasis, as a real article does.
func FormattedArticle(paragraphs int) []byte {
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>A formatted article about SQLite</title></head>` +
		`<body><nav><ul><li><a href="/">Home</a></li><li><a href="/archives">Archives</a></li></ul></nav><main><article>`)
	for i := 0; i < paragraphs; i++ {
		fmt.Fprintf(&b, `<p>%s <a href="/post/%d">a <strong>linked</strong> word</a>, some <b>bold</b> and <em>emphasis</em>, `+
			`<code>PRAGMA</code> settings and <i>italic</i> text of paragraph %d.</p>`+"\n", vocabulary[i%len(vocabulary)], i, i)
		if i%10 == 9 {
			fmt.Fprintf(&b, "<h2>Section %d</h2>\n<table><tr><td><b>key</b></td><td>value %d</td></tr></table>\n", i, i)
		}
	}
	b.WriteString(`</article></main><footer><p>Footer of a <a href="/">fictional blog</a>.</p></footer></body></html>`)
	return b.Bytes()
}

// reopenedPage builds the pages of the third review: forty <b> opened, then middle, which
// should close them but does not for the parser, then blocks.
func reopenedPage(middle string, blocks int) []byte {
	var b bytes.Buffer
	reopenedHead(&b)
	b.WriteString(middle)
	reopenedBlocks(&b, blocks)
	return b.Bytes()
}

// SelectEnds is Reopened with forty </b> inside a <select>, where the parser ignores them.
func SelectEnds(blocks int) []byte {
	return reopenedPage("<select>"+strings.Repeat("</b>", 40)+"</select>", blocks)
}

// ForeignEnds is Reopened with forty </b> inside an SVG <foreignObject> and a MathML <mi>,
// scope boundaries of foreign content.
func ForeignEnds(blocks int) []byte {
	return reopenedPage("<svg><foreignObject>"+strings.Repeat("</b>", 40)+"</foreignObject></svg>"+
		"<math><mi>"+strings.Repeat("</b>", 40)+"</mi></math>", blocks)
}

// StyleInSVG is a page of size bytes whose <style>, inside an SVG the parser still has open
// after an ignored </svg>, holds hundreds of thousands of elements instead of text.
func StyleInSVG(size int) []byte {
	var b bytes.Buffer
	b.WriteString(`<!DOCTYPE html><html><head><title>Style</title></head><body><div>` +
		`<svg><foreignObject><div></svg></div></foreignObject><style>`)
	for b.Len() < size-200 {
		b.WriteString("<a/>")
	}
	b.WriteString("</style></svg></div></body></html>")
	return b.Bytes()
}

// pageHead is the head and the navigation of the normal pages below.
func pageHead(b *bytes.Buffer, title string) {
	fmt.Fprintf(b, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title></head><body><header><nav><ul>`, title)
	for i := 0; i < 30; i++ {
		fmt.Fprintf(b, `<li><a href="/c%d">Section %d</a></li>`, i, i)
	}
	b.WriteString(`</ul></nav></header><main><article>`)
}

// Cards is a normal page of n cards, each a link holding a block, as on a home page.
func Cards(n int) []byte {
	var b bytes.Buffer
	pageHead(&b, "A page of fictional cards")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `<a class="card" href="/p%d"><div class="c"><img src="/i%d.jpg"><h3>Card %d about %s</h3>`+
			`<p>A teaser of the fictional card number %d, about %s settings.</p></div></a>`,
			i, i, i, vocabulary[i%len(vocabulary)], i, vocabulary[(i+3)%len(vocabulary)])
	}
	b.WriteString(`</article></main><footer><p>&copy; site</p></footer></body></html>`)
	return b.Bytes()
}

// SloppyFont is an old-style page of n paragraphs inside <font> elements never closed.
func SloppyFont(n int) []byte {
	var b bytes.Buffer
	pageHead(&b, "An old fictional page with fonts")
	b.WriteString(`<font face="Arial"><font size="2">`)
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, `<p>Paragraph %d of an old page about %s, with <b>bold</b> words and a <a href="/x%d">link</a><br>`,
			i, vocabulary[i%len(vocabulary)], i)
	}
	b.WriteString(`</article></main><footer><p>&copy; site</p></footer></body></html>`)
	return b.Bytes()
}

// Words is a text of about length characters of words of the vocabulary, with its number of
// words.
func Words(length int) (string, int) {
	var b strings.Builder
	words := 0
	for b.Len() < length {
		if words > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(vocabulary[(words*7)%len(vocabulary)])
		words++
	}
	return b.String(), words
}

// MetaOnly is a page answered 200 whose only text is its og:title and og:description.
func MetaOnly(title, description string) []byte {
	return []byte(`<!DOCTYPE html><html><head><meta charset="utf-8">` +
		`<meta property="og:title" content="` + html.EscapeString(title) + `">` +
		`<meta property="og:description" content="` + html.EscapeString(description) + `">` +
		`</head><body></body></html>`)
}
