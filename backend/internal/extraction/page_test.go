package extraction_test

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
)

// fixture reads a recorded fictional page of backend/testdata/pages.
func fixture(t testing.TB, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "pages", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

func mustURL(t testing.TB, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	return u
}

func deref[T any](v *T) any {
	if v == nil {
		return nil
	}
	return *v
}

func TestReadResponse(t *testing.T) {
	const provisional = "example.com"
	cases := []struct {
		name        string
		status      int
		contentType string
		body        []byte
		tooLarge    bool
		wantMode    string // "" when deferred
		wantReason  string
		wantTitle   any // the title written, the provisional one when the page gives none
		wantSite    string
		wantContent func(t *testing.T, content any)
	}{
		{
			name: "article page", status: 200, contentType: "text/html; charset=utf-8", body: fixture(t, "article.html"),
			wantMode: "full", wantReason: "ok", wantTitle: "Tuning SQLite", wantSite: "Example Blog",
		},
		{
			name: "minified page", status: 200, contentType: "text/html", body: fixture(t, "minified.html"),
			wantMode: "full", wantReason: "ok", wantTitle: "A minified page about SQLite settings", wantSite: "Example Blog",
		},
		{
			name: "page without site name keeps the host", status: 200, contentType: "text/html", body: fixture(t, "no-site-name.html"),
			wantMode: "full", wantReason: "ok", wantTitle: "A post without site name", wantSite: provisional,
		},
		{
			name: "ISO-8859-1 declared by a meta element", status: 200, contentType: "text/html", body: fixture(t, "latin1.html"),
			wantMode: "full", wantReason: "ok", wantTitle: "Crème brûlée", wantSite: provisional,
			wantContent: func(t *testing.T, content any) {
				if !strings.Contains(content.(string), "très légère") {
					t.Errorf("content does not hold the decoded accents")
				}
			},
		},
		{
			name: "windows-1252 declared by the header", status: 200, contentType: "text/html; charset=windows-1252", body: fixture(t, "windows-1252.html"),
			wantMode: "full", wantReason: "ok", wantTitle: "“Œuvres” complètes – 5 €", wantSite: provisional,
			wantContent: func(t *testing.T, content any) {
				if !strings.Contains(content.(string), "L’œuvre coûte 5 €") {
					t.Errorf("content does not hold the decoded windows-1252 characters")
				}
			},
		},
		{
			name: "page whose content needs JavaScript", status: 200, contentType: "text/html", body: fixture(t, "javascript-only.html"),
			wantMode: "fallback", wantReason: "short", wantTitle: "App", wantSite: provisional,
			wantContent: func(t *testing.T, content any) {
				if content != "Loading" {
					t.Errorf("content = %v, want the og:description", content)
				}
			},
		},
		{
			name: "fallback keeps the description as content", status: 200, contentType: "text/html", body: fixture(t, "og-only.html"),
			wantMode: "fallback", wantReason: "short", wantTitle: provisional, wantSite: provisional,
			wantContent: func(t *testing.T, content any) {
				if content != "A short summary" {
					t.Errorf("content = %v, want the og:description", content)
				}
			},
		},
		{
			name: "error status ignores the page metadata", status: 404, contentType: "text/html", body: fixture(t, "not-found.html"),
			wantMode: "fallback", wantReason: "status", wantTitle: provisional, wantSite: provisional,
			wantContent: func(t *testing.T, content any) {
				if content != nil {
					t.Errorf("content = %v, want none", content)
				}
			},
		},
		{
			name: "a type that is not HTML falls back", status: 200, contentType: "image/png", body: []byte("\x89PNG\r\n\x1a\n"),
			wantMode: "fallback", wantReason: "not_html", wantTitle: provisional, wantSite: provisional,
		},
		{
			name: "no type and an HTML body is read", status: 200, contentType: "", body: fixture(t, "article.html"),
			wantMode: "full", wantReason: "ok", wantTitle: "Tuning SQLite", wantSite: "Example Blog",
		},
		{
			name: "an oversized body falls back", status: 200, contentType: "text/html", body: fixture(t, "article.html"), tooLarge: true,
			wantMode: "fallback", wantReason: "too_large", wantTitle: provisional, wantSite: provisional,
		},
		{
			name: "a PDF type is deferred", status: 200, contentType: "application/pdf", body: nil,
			wantReason: "deferred_pdf",
		},
		{
			name: "a PDF body is deferred", status: 200, contentType: "application/octet-stream", body: []byte("%PDF-1.7\n"),
			wantReason: "deferred_pdf",
		},
		{
			name: "a PDF served as HTML is deferred", status: 200, contentType: "text/html", body: []byte("%PDF-1.4\n"),
			wantReason: "deferred_pdf",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extraction.ReadResponse(extraction.Response{
				Status: tc.status, ContentType: tc.contentType, Body: tc.body, TooLarge: tc.tooLarge,
				URL: mustURL(t, "https://example.com/post"),
			}, provisional)
			if got.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if tc.wantMode == "" {
				if !got.Deferred {
					t.Errorf("deferred = false, want true")
				}
				return
			}
			if got.Deferred || got.Extraction.Mode != tc.wantMode {
				t.Fatalf("deferred %v mode %q, want %q", got.Deferred, got.Extraction.Mode, tc.wantMode)
			}
			if title := deref(got.Extraction.Title); title != tc.wantTitle {
				t.Errorf("title = %v, want %v", title, tc.wantTitle)
			}
			if site := deref(got.Extraction.SiteName); site != tc.wantSite {
				t.Errorf("site = %v, want %q", site, tc.wantSite)
			}
			if tc.wantMode == "fallback" && (got.Extraction.ReadingMinutes != nil || got.Extraction.ContentTruncated) {
				t.Errorf("fallback reading %v truncated %v, want none", got.Extraction.ReadingMinutes, got.Extraction.ContentTruncated)
			}
			if tc.wantMode == "full" && (got.Extraction.ContentText == nil || got.Extraction.ReadingMinutes == nil) {
				t.Errorf("full without content or reading time: %+v", got.Extraction)
			}
			if tc.wantContent != nil {
				tc.wantContent(t, deref(got.Extraction.ContentText))
			}
		})
	}

	t.Run("article content holds the text but neither the menu nor the comments", func(t *testing.T) {
		got := extraction.ReadResponse(extraction.Response{
			Status: 200, ContentType: "text/html", Body: fixture(t, "article.html"), URL: mustURL(t, "https://example.com/post"),
		}, provisional)
		content := deref(got.Extraction.ContentText).(string)
		if !strings.HasPrefix(content, "SQLite is a fine engine") {
			t.Errorf("content starts with %q, want the article", content[:min(len(content), 60)])
		}
		for _, unwanted := range []string{"menu entry", "Great post", "Comment section", "Footer"} {
			if strings.Contains(content, unwanted) {
				t.Errorf("content holds %q", unwanted)
			}
		}
		if got := len(strings.Fields(content)); got != 920 {
			t.Errorf("content has %d words, want the 920 of the article", got)
		}
		if minutes := deref(got.Extraction.ReadingMinutes); minutes != int64(4) {
			t.Errorf("reading minutes = %v, want 4", minutes)
		}
	})

	t.Run("a page of more than 20 000 elements falls back", func(t *testing.T) {
		got := extraction.ReadResponse(extraction.Response{
			Status: 200, ContentType: "text/html", Body: pagestest.Tiny(200_000), URL: mustURL(t, "https://example.com/post"),
		}, provisional)
		if got.Extraction.Mode != "fallback" || got.Reason != "too_large" {
			t.Errorf("mode %q reason %q, want fallback and too_large", got.Extraction.Mode, got.Reason)
		}
	})

	t.Run("a long description kept by a fallback is truncated and marked", func(t *testing.T) {
		description, _ := pagestest.Words(200_000)
		body := pagestest.MetaOnly("Short page", description)
		got := extraction.ReadResponse(extraction.Response{
			Status: 200, ContentType: "text/html", Body: body, URL: mustURL(t, "https://example.com/post"),
		}, provisional)
		content, _ := deref(got.Extraction.ContentText).(string)
		fields := strings.Fields(content)
		if got.Extraction.Mode != "fallback" || !got.Extraction.ContentTruncated || got.Extraction.ReadingMinutes != nil {
			t.Fatalf("mode %q truncated %v reading %v, want a truncated fallback without reading time",
				got.Extraction.Mode, got.Extraction.ContentTruncated, got.Extraction.ReadingMinutes)
		}
		if n := utf8.RuneCountInString(content); n > extraction.MaxContentLength || len(fields) == 0 || !pagestest.IsWord(fields[len(fields)-1]) {
			t.Errorf("content of %d code points, want at most %d ending on a whole word", n, extraction.MaxContentLength)
		}
	})

	t.Run("a title kept from the page is cut at its technical bound", func(t *testing.T) {
		title, _ := pagestest.Words(5_000)
		for name, body := range map[string][]byte{
			"og:title of a fallback": pagestest.MetaOnly(title, "A short summary"),
			"title of a full page":   pagestest.Article(title, strings.Repeat("a sentence of the article ", 20)),
		} {
			got := extraction.ReadResponse(extraction.Response{
				Status: 200, ContentType: "text/html", Body: body, URL: mustURL(t, "https://example.com/post"),
			}, provisional)
			kept, _ := deref(got.Extraction.Title).(string)
			fields := strings.Fields(kept)
			if n := utf8.RuneCountInString(kept); n > extraction.MaxTitleLength || n < extraction.MaxTitleLength/2 || !pagestest.IsWord(fields[len(fields)-1]) {
				t.Errorf("%s: title of %d code points, want at most %d ending on a whole word", name, n, extraction.MaxTitleLength)
			}
			if got.Extraction.ContentTruncated && name == "og:title of a fallback" {
				t.Errorf("%s: a cut title marks the content truncated", name)
			}
		}
	})

	t.Run("the full threshold is 200 characters of text", func(t *testing.T) {
		for _, tc := range []struct {
			length int
			want   string
		}{{199, "fallback"}, {200, "full"}} {
			body := pagestest.Article("Threshold", strings.Repeat("a", tc.length))
			got := extraction.ReadResponse(extraction.Response{
				Status: 200, ContentType: "text/html", Body: body, URL: mustURL(t, "https://example.com/post"),
			}, provisional)
			if got.Extraction.Mode != tc.want {
				t.Errorf("%d characters: mode %q, want %q", tc.length, got.Extraction.Mode, tc.want)
			}
		}
	})

	t.Run("very long article is truncated at a word boundary, reading time from the whole text", func(t *testing.T) {
		body, words := pagestest.LongArticle(200_000)
		got := extraction.ReadResponse(extraction.Response{
			Status: 200, ContentType: "text/html", Body: body, URL: mustURL(t, "https://example.com/post"),
		}, provisional)
		content := deref(got.Extraction.ContentText).(string)
		if !got.Extraction.ContentTruncated {
			t.Error("content_truncated = false, want true")
		}
		if n := utf8.RuneCountInString(content); n > extraction.MaxContentLength || n < extraction.MaxContentLength-1000 {
			t.Errorf("content has %d code points, want at most %d and close to it", n, extraction.MaxContentLength)
		}
		fields := strings.Fields(content)
		if last, _ := utf8.DecodeLastRuneInString(content); unicode.IsSpace(last) || !pagestest.IsWord(fields[len(fields)-1]) {
			t.Errorf("content ends with %q, want a whole word", content[len(content)-20:])
		}
		wantMinutes := int64((words + 229) / 230)
		if minutes := deref(got.Extraction.ReadingMinutes); minutes != wantMinutes {
			t.Errorf("reading minutes = %v, want %d from the %d words before truncation", minutes, wantMinutes, words)
		}
	})
}

func TestTruncate(t *testing.T) {
	limit := extraction.MaxContentLength
	cases := []struct {
		name      string
		text      string
		want      string
		truncated bool
	}{
		{name: "short text is kept whole", text: "a few words", want: "a few words"},
		{name: "text at the limit is kept whole", text: strings.Repeat("a", limit), want: strings.Repeat("a", limit)},
		{
			name: "cut at the last whitespace before the limit",
			text: strings.Repeat("a", limit-10) + " " + strings.Repeat("b", 20),
			want: strings.Repeat("a", limit-10), truncated: true,
		},
		{
			name: "a space right after the limit keeps the last word",
			text: strings.Repeat("a", limit) + " tail",
			want: strings.Repeat("a", limit), truncated: true,
		},
		{
			name: "no whitespace in the last 1 000 code points cuts at the limit",
			text: strings.Repeat("a", limit-1500) + " " + strings.Repeat("b", 2000),
			want: strings.Repeat("a", limit-1500) + " " + strings.Repeat("b", 1499), truncated: true,
		},
		{
			name: "counts code points, never cuts inside a UTF-8 sequence",
			text: strings.Repeat("é", limit+5),
			want: strings.Repeat("é", limit), truncated: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, truncated := extraction.Truncate(tc.text)
			if truncated != tc.truncated || got != tc.want {
				t.Errorf("Truncate = %d code points, %v; want %d, %v", utf8.RuneCountInString(got), truncated, utf8.RuneCountInString(tc.want), tc.truncated)
			}
			if !utf8.ValidString(got) {
				t.Error("truncated text is not valid UTF-8")
			}
		})
	}
}

func TestReadingMinutes(t *testing.T) {
	for _, tc := range []struct {
		words int
		want  int64
	}{{1, 1}, {230, 1}, {231, 2}, {920, 4}, {921, 5}} {
		if got := extraction.ReadingMinutes(strings.Repeat("word ", tc.words)); got != tc.want {
			t.Errorf("%d words: %d minutes, want %d", tc.words, got, tc.want)
		}
	}
}
