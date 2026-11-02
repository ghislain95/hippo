package extraction_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pdftest"
)

// filler is enough text for a document to be read in full, after the lines a test looks at.
func filler() []string {
	return pdftest.Pages(pdftest.Sentences(60, 3), 60, 12)[0].Lines
}

// withFiller is a page of lines followed by the filler.
func withFiller(lines ...string) pdftest.Page {
	return pdftest.Page{Lines: append(lines, filler()...)}
}

func readPDF(t *testing.T, doc pdftest.Doc) (extraction.Outcome, string, int64) {
	t.Helper()
	return readPDFBytes(t, doc.Bytes(), "https://docs.example.com/papers/tuning.pdf")
}

func readPDFBytes(t *testing.T, pdf []byte, finalURL string) (extraction.Outcome, string, int64) {
	t.Helper()
	out, words := extraction.ReadPDF(pdf, mustURL(t, finalURL), "docs.example.com")
	content := ""
	if out.Extraction.ContentText != nil {
		content = *out.Extraction.ContentText
	}
	return out, content, words
}

func assertFull(t *testing.T, out extraction.Outcome) {
	t.Helper()
	if out.Extraction.Mode != "full" || out.Reason != "ok" {
		t.Fatalf("outcome %q, %q; want full, ok", out.Extraction.Mode, out.Reason)
	}
}

func TestReadPDF(t *testing.T) {
	t.Run("article", func(t *testing.T) {
		doc, sentences := pdftest.Article("Tuning SQLite", 10, 1951)
		out, content, words := readPDF(t, doc)
		assertFull(t, out)
		if words != 1951 {
			t.Errorf("words = %d, want 1951", words)
		}
		if got := len(strings.Fields(content)); got != 1951 {
			t.Errorf("content holds %d words, want 1951", got)
		}
		// Sentences run over lines and pages.
		if flat := strings.Join(strings.Fields(content), " "); !strings.HasPrefix(flat, sentences[0]) || !strings.HasSuffix(flat, sentences[len(sentences)-1]) {
			t.Errorf("content does not start with the first sentence nor end with the last: %q … %q", flat[:80], flat[len(flat)-80:])
		}
		if got := strings.Count(content, "\n\n"); got != 9 {
			t.Errorf("content holds %d page breaks, want 9", got)
		}
		if *out.Extraction.Title != "Tuning SQLite" || *out.Extraction.SiteName != "docs.example.com" {
			t.Errorf("title %q site %q", *out.Extraction.Title, *out.Extraction.SiteName)
		}
		if *out.Extraction.ReadingMinutes != 9 || out.Extraction.ContentTruncated {
			t.Errorf("reading %d truncated %v, want 9 and false", *out.Extraction.ReadingMinutes, out.Extraction.ContentTruncated)
		}
	})

	for _, placement := range []struct {
		name string
		p    pdftest.Placement
		font pdftest.Font
	}{
		{"glyph by glyph", pdftest.Glyphs, pdftest.TwoByte},
		{"glyph by glyph in a simple font", pdftest.Glyphs, pdftest.Simple},
		{"a move per word", pdftest.WordMoves, pdftest.Simple},
	} {
		t.Run("words placed without spaces: "+placement.name, func(t *testing.T) {
			out, content, _ := readPDF(t, pdftest.Doc{
				Font: placement.font, Placement: placement.p, Compress: true,
				Pages: []pdftest.Page{withFiller("Le moteur lit chaque page")},
			})
			assertFull(t, out)
			if !strings.HasPrefix(content, "Le moteur lit chaque page\n") {
				t.Errorf("content = %q", content[:60])
			}
		})
	}

	t.Run("two-byte font with a map to Unicode", func(t *testing.T) {
		out, content, _ := readPDF(t, pdftest.Doc{
			Font: pdftest.TwoByte, Placement: pdftest.Glyphs,
			Pages: []pdftest.Page{withFiller("Crème brûlée à l'œuf")},
		})
		assertFull(t, out)
		if !strings.HasPrefix(content, "Crème brûlée à l'œuf\n") {
			t.Errorf("content = %q", content[:60])
		}
	})

	t.Run("ligatures and hyphenated line ends", func(t *testing.T) {
		out, _, words := readPDF(t, pdftest.Doc{
			Font: pdftest.TwoByte, Placement: pdftest.Spaces,
			Pages: []pdftest.Page{{Lines: []string{"Une \ufb01ne réfle-", "xion sur le porte\u00ad", "monnaie", "", "un mot-", "clé, 2025-", "2026"}}},
		})
		if out.Extraction.Mode != "fallback" || out.Reason != "short" {
			t.Fatalf("outcome %q, %q; want a short fallback", out.Extraction.Mode, out.Reason)
		}
		// The text of a short document is not kept: read it with the filler.
		out, content, _ := readPDF(t, pdftest.Doc{
			Font: pdftest.TwoByte, Placement: pdftest.Spaces,
			Pages: []pdftest.Page{withFiller("Une \ufb01ne réfle-", "xion sur le porte\u00ad", "monnaie", "un mot-", "clé, 2025-", "2026")},
		})
		assertFull(t, out)
		if !strings.HasPrefix(content, "Une fine réfle-xion sur le portemonnaie\nun mot-clé, 2025-\n2026\n") {
			t.Errorf("content = %q", content[:80])
		}
		if words != 10 { // Une fine réfle-xion sur le portemonnaie un mot-clé, 2025- 2026
			t.Errorf("words = %d, want 10: a joined word counts once", words)
		}
	})

	t.Run("AES-128 title without control characters", func(t *testing.T) {
		article, _ := pdftest.Article("", 2, 300)
		out, content, _ := readPDF(t, pdftest.Doc{Title: []byte("Copie fictive"), Encryption: pdftest.AES128, Compress: true, Pages: article.Pages})
		assertFull(t, out)
		if *out.Extraction.Title != "Copie fictive" {
			t.Errorf("title = %q, want Copie fictive", *out.Extraction.Title)
		}
		if len(strings.Fields(content)) != 300 {
			t.Errorf("content holds %d words, want 300", len(strings.Fields(content)))
		}
	})

	t.Run("PDF 2.0 header overlay", func(t *testing.T) {
		article, _ := pdftest.Article("Version deux", 1, 100)
		article.Version = "2.0"
		out, content, _ := readPDF(t, article)
		assertFull(t, out)
		if len(strings.Fields(content)) != 100 || *out.Extraction.Title != "Version deux" {
			t.Errorf("content of %d words, title %q", len(strings.Fields(content)), *out.Extraction.Title)
		}
	})

	t.Run("a page that cannot be decoded is skipped", func(t *testing.T) {
		out, content, _ := readPDF(t, pdftest.Doc{Pages: []pdftest.Page{
			withFiller("Texte de la page un."), {Broken: true}, withFiller("Texte de la page trois."),
		}})
		assertFull(t, out)
		if !strings.HasPrefix(content, "Texte de la page un.") || !strings.Contains(content, "\n\nTexte de la page trois.") {
			t.Errorf("content = %q", content)
		}
	})

	t.Run("broken metadata keeps the text", func(t *testing.T) {
		article, _ := pdftest.Article("Notice fictive", 2, 300)
		article.BrokenInfo = true
		out, content, _ := readPDFBytes(t, article.Bytes(), "https://example.com/docs/notice.pdf")
		assertFull(t, out)
		if *out.Extraction.Title != "notice" || len(strings.Fields(content)) != 300 {
			t.Errorf("title %q, content of %d words; want notice and 300 words", *out.Extraction.Title, len(strings.Fields(content)))
		}
	})

	t.Run("400 pages", func(t *testing.T) {
		doc, _ := pdftest.Article("Long", 400, 78_000)
		out, content, words := readPDF(t, doc)
		assertFull(t, out)
		if words != 78_000 || *out.Extraction.ReadingMinutes != 340 {
			t.Errorf("words %d reading %d, want 78 000 and 340", words, *out.Extraction.ReadingMinutes)
		}
		fields := strings.Fields(content)
		if n := utf8.RuneCountInString(content); n > extraction.MaxContentLength || n < extraction.MaxContentLength-1000 ||
			!out.Extraction.ContentTruncated || !pdftest.IsWord(fields[len(fields)-1]) {
			t.Errorf("content of %d code points ending with %q, truncated %v", n, fields[len(fields)-1], out.Extraction.ContentTruncated)
		}
	})

	t.Run("enormous page", func(t *testing.T) {
		const size = 4 << 20 // enough to show that the keeping stops within the page
		out, content, words := readPDF(t, pdftest.Doc{Compress: true, Pages: []pdftest.Page{pdftest.EnormousPage(size)}})
		assertFull(t, out)
		if n := utf8.RuneCountInString(content); n > extraction.MaxContentLength || !out.Extraction.ContentTruncated {
			t.Errorf("content of %d code points, truncated %v", n, out.Extraction.ContentTruncated)
		}
		// Each line of the page shows 6 words in about 60 bytes of operators.
		if words < size/12 {
			t.Errorf("words = %d, want the whole page counted", words)
		}
	})

	t.Run("scanned is short", func(t *testing.T) {
		out, content, _ := readPDF(t, pdftest.Doc{Title: []byte("Facture fictive"), Pages: []pdftest.Page{{Image: true}, {Image: true}, {Image: true}}})
		if out.Extraction.Mode != "fallback" || out.Reason != "short" || content != "" || *out.Extraction.Title != "Facture fictive" {
			t.Errorf("outcome %q, %q, title %q, content %q; want a short fallback with the title", out.Extraction.Mode, out.Reason, *out.Extraction.Title, content)
		}
	})

	article, _ := pdftest.Article("", 2, 300)
	refused := []struct {
		name, reason string
		pdf          []byte
	}{
		{"password", "encrypted", pdftest.Doc{Encryption: pdftest.AES128, UserPassword: "ouvrir", Pages: article.Pages}.Bytes()},
		{"AES-256", "encrypted", pdftest.Doc{Encryption: pdftest.AES256, Pages: article.Pages}.Bytes()},
		{"truncated", "damaged", pdftest.Truncated(article.Bytes())},
		{"wrong startxref", "damaged", pdftest.WrongStartxref(article.Bytes())},
		{"an HTML page", "damaged", pagestestArticle()},
		{"page tree that contains itself", "damaged", pdftest.Doc{TreeCycle: true, Pages: article.Pages}.Bytes()},
		{"page tree that lists a page twice", "damaged", pdftest.Doc{RepeatKids: true, Pages: article.Pages}.Bytes()},
		{"page tree that lists a page twice among other values", "damaged", pdftest.Doc{RepeatKids: true, ExtraKids: "null 7", Pages: article.Pages}.Bytes()},
	}
	for _, tc := range refused {
		t.Run(tc.name+" falls back as "+tc.reason, func(t *testing.T) {
			out, content, _ := readPDFBytes(t, tc.pdf, "https://example.com/docs/rapport-fictif.pdf")
			if out.Extraction.Mode != "fallback" || out.Reason != tc.reason || content != "" {
				t.Errorf("outcome %q, %q, content %q; want fallback, %s, no content", out.Extraction.Mode, out.Reason, content, tc.reason)
			}
			if *out.Extraction.Title != "rapport-fictif" || out.Extraction.ReadingMinutes != nil {
				t.Errorf("title %q reading %v, want the file name and no reading time", *out.Extraction.Title, out.Extraction.ReadingMinutes)
			}
		})
	}
}

func pagestestArticle() []byte {
	return []byte("<!DOCTYPE html><html><head><title>Not a PDF</title></head><body><p>An HTML page served as a PDF.</p></body></html>")
}
