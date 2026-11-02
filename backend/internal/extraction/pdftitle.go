package extraction

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// junkPrefixes start the metadata titles that Office writes from a file name (RG-28).
var junkPrefixes = []string{"Microsoft Word - ", "Microsoft PowerPoint - ", "Microsoft Excel - "}

// junkTitles are the metadata titles a producer writes when the author gave none.
var junkTitles = []string{"Untitled", "Sans titre", "Document1", "PowerPoint Presentation"}

// documentExtensions end the file names that producers write as a title.
var documentExtensions = map[string]bool{
	"doc": true, "docx": true, "odt": true, "rtf": true, "txt": true, "pdf": true, "ps": true, "tex": true,
	"dvi": true, "htm": true, "html": true, "xhtml": true, "md": true, "ppt": true, "pptx": true, "odp": true,
	"key": true, "xls": true, "xlsx": true, "ods": true, "numbers": true, "pages": true, "indd": true,
	"tif": true, "tiff": true, "jpg": true, "jpeg": true, "png": true,
}

// pdfTitle is the title of a PDF link (RG-28): its metadata title meta unless junk, else the
// file name of final, the URL the PDF was served from, else the provisional title.
func pdfTitle(meta string, final *url.URL, provisional string) string {
	if title := cleanPDFText(meta); !isJunkTitle(title) {
		return cutTitle(title)
	}
	if name := urlFileName(final); name != "" {
		return cutTitle(name)
	}
	return provisional
}

// cleanPDFText removes the control characters of a text of a PDF (the padding that AES leaves
// at the end of a string included) and its invisible format characters (zero-width space, byte
// order mark, soft hyphen…), makes it valid UTF-8 and collapses its whitespace: a title made of
// invisible characters only is empty.
func cleanPDFText(text string) string {
	text = strings.Map(func(r rune) rune {
		if (unicode.IsControl(r) && !unicode.IsSpace(r)) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(text, "�"))
	return clean(text)
}

// isJunkTitle tells whether a cleaned metadata title is no title: empty, written by Office
// from a file name, a default title, or a file name.
func isJunkTitle(title string) bool {
	if title == "" {
		return true
	}
	for _, prefix := range junkPrefixes {
		if len(title) >= len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
			return true
		}
	}
	for _, junk := range junkTitles {
		if strings.EqualFold(title, junk) {
			return true
		}
	}
	return isFileName(title)
}

// isFileName tells whether the last component of title, after / or \, ends with a document
// extension right after a character that is not a space: "page.html", not "Node.js en
// production" nor "Rapport .pdf".
func isFileName(title string) bool {
	last := title[strings.LastIndexAny(title, `/\`)+1:]
	dot := strings.LastIndexByte(last, '.')
	if dot < 1 || last[dot-1] == ' ' {
		return false
	}
	return documentExtensions[strings.ToLower(last[dot+1:])]
}

// urlFileName is the last non-empty path segment of u when it ends with .pdf ignoring case,
// percent-decoded (raw when it cannot be, or not into UTF-8), without its suffix and cleaned;
// "" otherwise.
func urlFileName(u *url.URL) string {
	if u == nil {
		return ""
	}
	segments := strings.Split(u.EscapedPath(), "/")
	var last string
	for i := len(segments) - 1; i >= 0 && last == ""; i-- {
		last = segments[i]
	}
	if len(last) < len(".pdf") || !strings.EqualFold(last[len(last)-len(".pdf"):], ".pdf") {
		return ""
	}
	name := last[:len(last)-len(".pdf")]
	// A name that does not decode, or decodes to bytes that are not UTF-8 (a Latin-1 escape such
	// as %E9), is kept as written in the URL.
	if decoded, err := url.PathUnescape(name); err == nil && utf8.ValidString(decoded) {
		name = decoded
	}
	return cleanPDFText(name)
}
