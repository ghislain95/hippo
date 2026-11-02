package extraction_test

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ghislain95/hippo/backend/internal/extraction"
)

func TestPDFTitle(t *testing.T) {
	const provisional = "example.com"
	const named = "https://example.com/docs/rapport-annuel-2025.pdf"
	cases := []struct {
		name, meta, url, want string
	}{
		{"a metadata title is kept", "Tuning SQLite", named, "Tuning SQLite"},
		{"control characters and AES padding are removed", "Copie fictive\x03\x03\x03", named, "Copie fictive"},
		{"tabs of AES padding are removed", "Copie fictive\t\t\t\t\t\t\t\t\t", named, "Copie fictive"},
		{"spaces are collapsed", "  Un   titre\n sur deux lignes ", named, "Un titre sur deux lignes"},
		{"a Word prefix is junk", "Microsoft Word - rapport.docx", named, "rapport-annuel-2025"},
		{"a PowerPoint prefix is junk", "Microsoft PowerPoint - Présentation", named, "rapport-annuel-2025"},
		{"an Excel prefix is junk", "Microsoft Excel - budget", named, "rapport-annuel-2025"},
		{"an HTML file name is junk", "page.html", named, "rapport-annuel-2025"},
		{"a file name with a path is junk", `C:\Users\fictif\Notes.DOCX`, named, "rapport-annuel-2025"},
		{"a Unix path is junk", "/tmp/export/scan.tif", named, "rapport-annuel-2025"},
		{"Untitled is junk", "Untitled", named, "rapport-annuel-2025"},
		{"Sans titre is junk whatever its case", "sans TITRE", named, "rapport-annuel-2025"},
		{"Document1 is junk", "Document1", named, "rapport-annuel-2025"},
		{"PowerPoint Presentation is junk", "PowerPoint Presentation", named, "rapport-annuel-2025"},
		{"only tabs is junk", "\t\t\t", named, "rapport-annuel-2025"},
		{"empty is junk", "", named, "rapport-annuel-2025"},
		{"a title that looks like a file name is kept", "Node.js en production", named, "Node.js en production"},
		{"an extension after a space is a title", "Rapport .pdf", named, "Rapport .pdf"},
		{"the suffix is matched ignoring case", "", "https://example.com/a/GUIDE.Pdf", "GUIDE"},
		{"the file name is percent-decoded", "", "https://example.com/files/Rapport%20d%27activit%C3%A9.PDF", "Rapport d'activité"},
		{"the last non-empty segment is read", "", "https://example.com/files/notice.pdf/", "notice"},
		{"no .pdf segment gives the provisional title", "", "https://example.com/download?id=7", provisional},
		{"a query ending with .pdf is not a file name", "", "https://example.com/get?file=a.pdf", provisional},
		{"a bare .pdf gives the provisional title", "", "https://example.com/.pdf", provisional},
		{"a decoded name that is not UTF-8 is kept raw", "", "https://example.com/files/%E9t%E9.pdf", "%E9t%E9"},
		{"an invisible title is junk", "\u200b\u200b\ufeff", named, "rapport-annuel-2025"},
		{"format characters are removed from a title", "\ufeffTi\u200btre", named, "Titre"},
		{"format characters are removed from a file name", "", "https://example.com/a/gu%E2%80%8Bide.pdf", "guide"},
		{"a junk title without file name gives the provisional title", "Untitled", "https://example.com/download", provisional},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extraction.PDFTitle(tc.meta, mustURL(t, tc.url), provisional); got != tc.want {
				t.Errorf("PDFTitle(%q, %s) = %q, want %q", tc.meta, tc.url, got, tc.want)
			}
		})
	}

	t.Run("a percent sign that is no escape is kept", func(t *testing.T) {
		// url.Parse refuses an invalid escape: such a name reaches the title through Path.
		u := &url.URL{Scheme: "https", Host: "example.com", Path: "/files/taux%ZZ-2025.pdf"}
		if got := extraction.PDFTitle("", u, provisional); got != "taux%ZZ-2025" {
			t.Errorf("PDFTitle = %q, want taux%%ZZ-2025", got)
		}
	})

	t.Run("a title is bound to 1 000 code points", func(t *testing.T) {
		long := strings.Repeat("très long titre ", 200)
		got := extraction.PDFTitle(long, mustURL(t, named), provisional)
		if n := utf8.RuneCountInString(got); n > extraction.MaxTitleLength || n < extraction.MaxTitleLength-20 {
			t.Errorf("title of %d code points, want at most %d, cut on a word", n, extraction.MaxTitleLength)
		}
		name := extraction.PDFTitle("", mustURL(t, "https://example.com/"+strings.Repeat("é", 1500)+".pdf"), provisional)
		if n := utf8.RuneCountInString(name); n != extraction.MaxTitleLength {
			t.Errorf("file name title of %d code points, want %d", n, extraction.MaxTitleLength)
		}
	})
}
