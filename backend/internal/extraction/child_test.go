package extraction_test

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pagestest"
)

// startedFIFO makes a FIFO that a hang child writes to once started; the channel is closed
// then. Opening a FIFO blocks until both ends are open: no sleep is needed.
func startedFIFO(t *testing.T) (string, <-chan struct{}) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "started")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	started := make(chan struct{})
	go func() {
		fifo, err := os.Open(path)
		if err != nil {
			return
		}
		defer func() { _ = fifo.Close() }()
		if _, err := bufio.NewReader(fifo).ReadString('\n'); err == nil {
			close(started)
		}
	}()
	t.Cleanup(func() {
		// Unblocks the reader when no child ever opened the FIFO.
		if fifo, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = fifo.Close()
		}
	})
	return path, started
}

func htmlResponse(t testing.TB, body []byte) extraction.Response {
	return extraction.Response{Status: 200, ContentType: "text/html", Body: body, URL: mustURL(t, "https://example.com/page")}
}

func TestChildEnvironment(t *testing.T) {
	t.Run("the child gets no variable of the service", func(t *testing.T) {
		t.Setenv("API_TOKEN", "test-token-that-must-not-reach-the-child")
		t.Setenv("ANTHROPIC_API_KEY", "test-key-that-must-not-reach-the-child")
		cmd := extraction.ChildCommandForTest(context.Background(), childArgv(extraction.ChildCommand))
		// A nil Env would inherit the environment of the service.
		if cmd.Env == nil || !slices.Equal(cmd.Env, []string{"GOMAXPROCS=2"}) {
			t.Errorf("child environment = %q, want only GOMAXPROCS=2", cmd.Env)
		}
		if cmd.Stderr != nil {
			t.Error("the stderr of the child is read: a panic message may quote the page")
		}
	})
}

func TestReadInChild(t *testing.T) {
	ctx := context.Background()
	const provisional = "example.com"

	accepted := []struct {
		name string
		body []byte
	}{
		{"article", fixture(t, "article.html")},
		{"formatted article", pagestest.FormattedArticle(400)},
		{"list of cards", pagestest.Cards(700)},
		{"old page with fonts never closed", pagestest.SloppyFont(1500)},
		{"article of 2 MB", pagestest.Large(extraction.MaxBodySize)},
	}
	for _, page := range accepted {
		t.Run("reads in full: "+page.name, func(t *testing.T) {
			got, err := extraction.ReadInChild(ctx, childArgv(extraction.ChildCommand), extraction.ReadTimeout, htmlResponse(t, page.body), provisional)
			if err != nil || got.Extraction.Mode != "full" || got.Extraction.ContentText == nil {
				t.Errorf("outcome %q, %q, %v; want full with content", got.Extraction.Mode, got.Reason, err)
			}
		})
	}

	hostile := []struct {
		name string
		body []byte
	}{
		{"tiny elements", pagestest.Tiny(extraction.MaxBodySize)},
		{"reopened formatting elements", pagestest.Reopened(19_900)},
		{"stray end tags", pagestest.StrayEnds(18_000)},
		{"end tags beyond an object", pagestest.ScopedEnds(18_000)},
		{"end tags inside a select", pagestest.SelectEnds(17_000)},
		{"end tags inside foreign content", pagestest.ForeignEnds(17_000)},
		{"elements in a style inside SVG", pagestest.StyleInSVG(extraction.MaxBodySize)},
	}
	for _, page := range hostile {
		t.Run("falls back without growing the service: "+page.name, func(t *testing.T) {
			var got extraction.Outcome
			var err error
			grown := heapPeak(func() {
				got, err = extraction.ReadInChild(ctx, childArgv(extraction.ChildCommand), extraction.ReadTimeout, htmlResponse(t, page.body), provisional)
			})
			if err != nil || got.Extraction.Mode != "fallback" || got.Reason != "too_large" {
				t.Errorf("outcome %q, %q, %v; want fallback, too_large", got.Extraction.Mode, got.Reason, err)
			}
			if got.Extraction.Title == nil || *got.Extraction.Title != provisional || got.Extraction.ContentText != nil {
				t.Errorf("title %v content %v, want the provisional title and no content", got.Extraction.Title, got.Extraction.ContentText)
			}
			if grown > 8<<20 {
				t.Errorf("the heap of the service grew by %d MB, want a few at most", grown>>20)
			}
		})
	}

	t.Run("a child killed at the time limit falls back on timeout", func(t *testing.T) {
		fifo, _ := startedFIFO(t)
		begin := time.Now()
		got, err := extraction.ReadInChild(ctx, childArgv(hangChild, fifo), 300*time.Millisecond, htmlResponse(t, fixture(t, "article.html")), provisional)
		if err != nil || got.Extraction.Mode != "fallback" || got.Reason != "timeout" {
			t.Errorf("outcome %q, %q, %v; want fallback, timeout", got.Extraction.Mode, got.Reason, err)
		}
		if elapsed := time.Since(begin); elapsed > 5*time.Second {
			t.Errorf("answer after %v, want about the time limit", elapsed)
		}
	})

	t.Run("a stop of the worker during the reading leaves the item to retry", func(t *testing.T) {
		fifo, started := startedFIFO(t)
		stopCtx, stop := context.WithCancel(ctx)
		go func() {
			<-started
			stop()
		}()
		_, err := extraction.ReadInChild(stopCtx, childArgv(hangChild, fifo), extraction.ReadTimeout, htmlResponse(t, fixture(t, "article.html")), provisional)
		if !errors.Is(err, extraction.ErrReaderStopped) {
			t.Errorf("error = %v, want the reading stopped", err)
		}
	})

	for _, mode := range []string{crashChild, garbageChild, unknownChild} {
		t.Run("a child that fails falls back: "+mode, func(t *testing.T) {
			got, err := extraction.ReadInChild(ctx, childArgv(mode), extraction.ReadTimeout, htmlResponse(t, fixture(t, "article.html")), provisional)
			if err != nil || got.Extraction.Mode != "fallback" || got.Reason != "reading_failed" {
				t.Errorf("outcome %q, %q, %v; want fallback, reading_failed", got.Extraction.Mode, got.Reason, err)
			}
		})
	}
}
