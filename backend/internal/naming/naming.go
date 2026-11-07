// Package naming holds the two normalisations of names the analysis relies on: the form of a
// tag (RG-58) and the normalised name of a category, its key for the match of a proposal
// (RG-54). Both are pure functions. It is a leaf package, and the only importer of
// golang.org/x/text (depguard rule text-normalisation-only-in-naming).
package naming

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxTagLength is the length of a tag at most, in runes (RG-58, tags.name CHECK).
const MaxTagLength = 40

// CategoryKey returns the normalised name of a category (RG-54): lower case, accents removed,
// each run of white space collapsed into one space, edges trimmed. « Ménage » and « menage »
// have the same key; « Java & Spring » and « Java et Spring » do not.
func CategoryKey(name string) string {
	var b strings.Builder
	space := false
	for _, r := range unaccented(name) {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

// Tag returns the normalised form of a tag (RG-58): lower case, accents removed, each run of
// white space, `_` and `-` replaced by one `-`, no `-` at either end, cut to MaxTagLength
// runes. Any other character is kept, so that the schema's pattern refuses it (`c++` makes the
// response invalid rather than becoming `c`). The result may be empty; the caller drops it.
func Tag(s string) string {
	runes := make([]rune, 0, len(s))
	dash := false
	for _, r := range unaccented(s) {
		if unicode.IsSpace(r) || r == '_' || r == '-' {
			dash = true
			continue
		}
		if dash && len(runes) > 0 {
			runes = append(runes, '-')
		}
		dash = false
		runes = append(runes, r)
	}
	if len(runes) > MaxTagLength {
		runes = runes[:MaxTagLength]
	}
	return strings.TrimRight(string(runes), "-")
}

// unaccented returns s lower-cased, without its combining marks: the canonical decomposition
// (NFD) splits « é » into « e » and its accent, which is dropped. A letter without a canonical
// decomposition (« œ », « ł ») is kept as it is.
func unaccented(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
