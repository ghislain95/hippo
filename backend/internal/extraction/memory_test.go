package extraction_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
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
