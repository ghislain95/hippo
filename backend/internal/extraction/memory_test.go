package extraction_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
	"github.com/ghislain95/hippo/backend/internal/extraction/pdftest"
)

// BenchmarkReadInChild measures the child that reads a page (design.md, task 3.4): for each
// page, child-held-MB is the highest memory its runtime held (0 when it exited at its limit
// before answering), child-rss-MB its resident set at its peak, exit its exit code; read-ms is
// the time of one reading, the cost of the spawn included. Run it with:
//
//	go test -run '^$' -bench ReadInChild -benchtime 3x ./internal/extraction/
//
// Without -race: the race detector changes the memory of the child.
func BenchmarkReadInChild(b *testing.B) {
	pages := []struct {
		name string
		body []byte
	}{
		{name: "tiny-page", body: pagestest.Article("Tiny", "a few words")}, // the cost of a spawn
		{name: "article-2MB", body: pagestest.Large(extraction.MaxBodySize)},
		{name: "cards", body: pagestest.Cards(700)},
		{name: "hostile-tiny-elements", body: pagestest.Tiny(extraction.MaxBodySize)},
		{name: "hostile-reopened", body: pagestest.Reopened(19_900)},
		{name: "hostile-stray-ends", body: pagestest.StrayEnds(18_000)},
		{name: "hostile-scoped-ends", body: pagestest.ScopedEnds(18_000)},
		{name: "hostile-select-ends", body: pagestest.SelectEnds(17_000)},
		{name: "hostile-foreign-ends", body: pagestest.ForeignEnds(17_000)},
		{name: "hostile-style-in-svg", body: pagestest.StyleInSVG(extraction.MaxBodySize)},
	}
	for _, page := range pages {
		b.Run(page.name, func(b *testing.B) {
			resp := htmlResponse(b, page.body)
			var worst extraction.ChildMeasure
			begin := time.Now()
			for b.Loop() {
				m, err := extraction.MeasureChild(childArgv(extraction.ChildCommand), resp)
				if err != nil {
					b.Fatalf("MeasureChild: %v", err)
				}
				worst.ExitCode = m.ExitCode
				worst.PeakBytes = max(worst.PeakBytes, m.PeakBytes)
				worst.MaxRSS = max(worst.MaxRSS, m.MaxRSS)
			}
			b.ReportMetric(float64(time.Since(begin).Milliseconds())/float64(b.N), "read-ms")
			b.ReportMetric(float64(worst.PeakBytes)/(1<<20), "child-held-MB")
			b.ReportMetric(float64(worst.MaxRSS)/(1<<20), "child-rss-MB")
			b.ReportMetric(float64(worst.ExitCode), "exit")
		})
	}
}

// heapPeak runs f while sampling the heap in use, and returns its highest value above the heap
// in use before f.
func heapPeak(f func()) uint64 {
	var stats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&stats)
	base := stats.HeapAlloc

	done := make(chan struct{})
	result := make(chan uint64)
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		var peak uint64
		var s runtime.MemStats
		for {
			runtime.ReadMemStats(&s)
			peak = max(peak, s.HeapAlloc)
			select {
			case <-done:
				result <- peak
				return
			case <-ticker.C:
			}
		}
	}()
	f()
	close(done)
	peak := <-result
	if peak < base {
		return 0
	}
	return peak - base
}

// BenchmarkReadPDFInChild measures the child that reads a PDF (design.md, task 3.4), as
// BenchmarkReadInChild does for pages. Run it with:
//
//	go test -run '^$' -bench ReadPDFInChild -benchtime 3x ./internal/extraction/
func BenchmarkReadPDFInChild(b *testing.B) {
	long, _ := pdftest.Article("Long", 400, 78_000)
	docs := []struct {
		name string
		doc  pdftest.Doc
	}{
		{name: "article-10-pages", doc: func() pdftest.Doc { d, _ := pdftest.Article("Article", 10, 1951); return d }()},
		{name: "400-pages", doc: long},
		{name: "45MB", doc: pdftest.Large(45_000_000, 90)},
		{name: "enormous-page-100MB", doc: pdftest.Doc{Compress: true, Pages: []pdftest.Page{pdftest.EnormousPage(100 << 20)}}},
		{name: "hostile-string-100MB", doc: pdftest.Doc{Compress: true, Pages: []pdftest.Page{pdftest.HugeString(100 << 20)}}},
		{name: "hostile-tree-cycle", doc: pdftest.Doc{TreeCycle: true, Pages: long.Pages[:2]}},
		{name: "hostile-prev-loop", doc: pdftest.Doc{PrevLoop: true, Pages: long.Pages[:2]}},
	}
	for _, tc := range docs {
		b.Run(tc.name, func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "doc")
			out, err := os.Create(path)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := tc.doc.WriteTo(out); err != nil {
				b.Fatal(err)
			}
			info, _ := out.Stat()
			_ = out.Close()
			var worst extraction.ChildMeasure
			begin := time.Now()
			for b.Loop() {
				file, err := os.Open(path)
				if err != nil {
					b.Fatal(err)
				}
				m, err := extraction.MeasurePDFChild(childArgv(extraction.ChildCommand), file)
				_ = file.Close()
				if err != nil {
					b.Fatalf("MeasurePDFChild: %v", err)
				}
				worst.ExitCode, worst.Mode, worst.Reason = m.ExitCode, m.Mode, m.Reason
				worst.PeakBytes = max(worst.PeakBytes, m.PeakBytes)
				worst.MaxRSS = max(worst.MaxRSS, m.MaxRSS)
			}
			b.ReportMetric(float64(info.Size())/(1<<20), "file-MB")
			b.ReportMetric(float64(time.Since(begin).Milliseconds())/float64(b.N), "read-ms")
			b.ReportMetric(float64(worst.PeakBytes)/(1<<20), "child-held-MB")
			b.ReportMetric(float64(worst.MaxRSS)/(1<<20), "child-rss-MB")
			b.ReportMetric(float64(worst.ExitCode), "exit")
			b.Logf("%s: %s %s", tc.name, worst.Mode, worst.Reason)
		})
	}
}

// BenchmarkReadYouTubeInChild measures the child that reads the player data of a video page
// (design.md of extract-youtube, task 5.1), as BenchmarkReadInChild does for pages, on
// fictional pages of 1.9 MiB. Run it with:
//
//	go test -run '^$' -bench ReadYouTubeInChild -benchtime 3x ./internal/extraction/
func BenchmarkReadYouTubeInChild(b *testing.B) {
	const id, size = "aaaaaaaaaa1", 1_900 << 10
	video := pagestest.Video{ID: id, Title: "Tuning SQLite", Author: "Example Talks", LengthSeconds: "754"}
	withDescription := func(description string) []byte {
		v := video
		v.Description = description
		return pagestest.PlayerResponse(pagestest.Details(v), 30)
	}
	// The description is twice in the player data (videoDetails, microformat): it fills the page
	// at half its size.
	empty := len(pagestest.VideoPage("Tuning SQLite", pagestest.PlayerScript(withDescription(""))))
	room := (size - empty - 1024) / 2
	ascii, _ := pagestest.Words(room)
	pages := []struct {
		name string
		body []byte
	}{
		{name: "real-shape-900-chars", body: pagestest.LargeVideoPage("Tuning SQLite", withDescription(strings.Repeat("A line of chapters and links.\n", 30)), size)},
		{name: "description-fills-page-ascii", body: pagestest.VideoPage("Tuning SQLite", pagestest.PlayerScript(withDescription(ascii)))},
		{name: "description-fills-page-4-byte", body: pagestest.VideoPage("Tuning SQLite", pagestest.PlayerScript(withDescription(strings.Repeat("\U0001F600", room/4))))},
	}
	for _, page := range pages {
		b.Run(page.name, func(b *testing.B) {
			if len(page.body) > extraction.MaxBodySize {
				b.Fatalf("page of %d bytes, over the limit", len(page.body))
			}
			var worst extraction.ChildMeasure
			begin := time.Now()
			for b.Loop() {
				m, err := extraction.MeasureVideoChild(childArgv(extraction.ChildCommand), page.body, id)
				if err != nil {
					b.Fatalf("MeasureVideoChild: %v", err)
				}
				worst.ExitCode, worst.Mode, worst.Reason = m.ExitCode, m.Mode, m.Reason
				worst.PeakBytes = max(worst.PeakBytes, m.PeakBytes)
				worst.MaxRSS = max(worst.MaxRSS, m.MaxRSS)
			}
			b.ReportMetric(float64(len(page.body))/(1<<20), "page-MB")
			b.ReportMetric(float64(time.Since(begin).Milliseconds())/float64(b.N), "read-ms")
			b.ReportMetric(float64(worst.PeakBytes)/(1<<20), "child-held-MB")
			b.ReportMetric(float64(worst.MaxRSS)/(1<<20), "child-rss-MB")
			b.ReportMetric(float64(worst.ExitCode), "exit")
			b.Logf("%s: %s %s", page.name, worst.Mode, worst.Reason)
		})
	}
}
