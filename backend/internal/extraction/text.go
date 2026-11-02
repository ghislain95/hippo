package extraction

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxContentLength is the longest content kept, in Unicode code points (RG-24).
	MaxContentLength = 120_000
	// MaxTitleLength is a technical bound of a title or a site name kept from a page, in code
	// points: cut on a word boundary, without ellipsis nor mark. It only keeps a hostile page
	// from writing a megabyte of title.
	MaxTitleLength = 1_000
	// wordBoundaryWindow is how far before the limit a cut looks for a whitespace.
	wordBoundaryWindow = 1_000
	// wordsPerMinute is the reading speed of RG-25.
	wordsPerMinute = 230
	// minFullLength is the length of article text, in code points, under which a page falls
	// back: a page that needs JavaScript has a shell, not an article.
	minFullLength = 200
)

// truncate cuts a content beyond MaxContentLength code points (RG-24), the main text of a page
// or the description a fallback keeps, and tells whether it cut.
func truncate(text string) (string, bool) {
	return cutAtWord(text, MaxContentLength)
}

// cutTitle bounds a title or a site name kept from a page to MaxTitleLength code points.
func cutTitle(text string) string {
	cut, _ := cutAtWord(text, MaxTitleLength)
	return cut
}

// cutAtWord cuts text beyond limit code points, at the last whitespace within the last
// wordBoundaryWindow code points before the limit, else at the limit itself, and tells whether
// it cut. Counting code points never cuts inside a UTF-8 sequence.
func cutAtWord(text string, limit int) (string, bool) {
	if utf8.RuneCountInString(text) <= limit {
		return text, false
	}
	runes := []rune(text)
	cut := limit
	// runes[limit] is the first code point beyond the limit: a whitespace there ends the last
	// word kept.
	for i := limit; i >= max(1, limit-wordBoundaryWindow); i-- {
		if unicode.IsSpace(runes[i]) {
			cut = i
			break
		}
	}
	return strings.TrimRightFunc(string(runes[:cut]), unicode.IsSpace), true
}

// readingMinutes is the reading time of text at wordsPerMinute, rounded up, at least 1 (RG-25).
func readingMinutes(text string) int64 {
	words := int64(len(strings.Fields(text)))
	return max(1, (words+wordsPerMinute-1)/wordsPerMinute)
}

// clean makes text valid UTF-8 and collapses its whitespace: the form of a title, a site name
// or a description.
func clean(text string) string {
	return strings.Join(strings.Fields(strings.ToValidUTF8(text, "�")), " ")
}
