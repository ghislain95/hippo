package extraction_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghislain95/hippo/backend/internal/extraction"
	"github.com/ghislain95/hippo/backend/internal/extraction/pdftest"
)

func TestFetchPDF(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T, timeout time.Duration) (*web, *extraction.TempDir, func(context.Context, string) (extraction.Response, error)) {
		w := newWeb(t)
		dir, _ := tempDir(t)
		fetch := extraction.Fetcher(testPolicy, w, w.roots, timeout, dir)
		return w, dir, func(ctx context.Context, raw string) (extraction.Response, error) {
			return fetch(ctx, mustURL(t, raw))
		}
	}
	pdf := articlePDF()

	for _, contentType := range []string{"application/pdf", "application/PDF; charset=binary", "application/octet-stream", "text/html; charset=utf-8", ""} {
		t.Run("a PDF is recognised by its type or its first bytes: "+contentType, func(t *testing.T) {
			w, _, fetch := setup(t, 5*time.Second)
			const link = "https://docs.example.com/a.pdf"
			w.handle(link, func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header()["Content-Type"] = []string{contentType} // "" stays empty, without sniffing
				_, _ = rw.Write(pdf)
			})
			resp, err := fetch(ctx, link)
			if err != nil || resp.Format != "pdf" || resp.TooLarge {
				t.Fatalf("format %q too large %v, %v; want a PDF", resp.Format, resp.TooLarge, err)
			}
			got, err := extraction.PDFBytes(resp)
			if err != nil || !bytes.Equal(got, pdf) {
				t.Errorf("file holds %d bytes (%v), want the %d of the PDF", len(got), err, len(pdf))
			}
		})
	}

	t.Run("the Accept header asks for a PDF after HTML", func(t *testing.T) {
		w, _, fetch := setup(t, 5*time.Second)
		const link = "https://docs.example.com/accept.pdf"
		w.page(link, "application/pdf", pdf)
		resp, err := fetch(ctx, link)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = extraction.PDFBytes(resp)
		if got := w.headersOf(link).Get("Accept"); got != "text/html,application/xhtml+xml,application/pdf;q=0.9" {
			t.Errorf("Accept = %q", got)
		}
	})

	t.Run("a PDF announced over 50 MiB is not read", func(t *testing.T) {
		w, _, fetch := setup(t, 5*time.Second)
		const link = "https://docs.example.com/huge.pdf"
		w.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/pdf")
			rw.Header().Set("Content-Length", strconv.Itoa(extraction.MaxPDFSize+1))
			rw.WriteHeader(http.StatusOK)
			rw.(http.Flusher).Flush()
			<-r.Context().Done() // a reading of the body would wait for the time limit
		})
		begin := time.Now()
		resp, err := fetch(ctx, link)
		if err != nil || resp.Format != "pdf" || !resp.TooLarge || resp.PDF != nil {
			t.Errorf("format %q too large %v file %v, %v; want a PDF too large, without file", resp.Format, resp.TooLarge, resp.PDF != nil, err)
		}
		if elapsed := time.Since(begin); elapsed > 2*time.Second {
			t.Errorf("answer after %v: the body was waited for", elapsed)
		}
	})

	for _, variant := range []string{"exact length", "no length", "gzip"} {
		t.Run("a body of 50 MiB plus one byte stops at the limit: "+variant, func(t *testing.T) {
			w, _, fetch := setup(t, 20*time.Second)
			link := "https://docs.example.com/big-" + strings.ReplaceAll(variant, " ", "-") + ".pdf"
			var served atomic.Int64
			w.handle(link, func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Content-Type", "application/pdf")
				var out io.Writer = rw
				switch variant {
				case "exact length":
					rw.Header().Set("Content-Length", strconv.Itoa(extraction.MaxPDFSize+1))
				case "gzip":
					rw.Header().Set("Content-Encoding", "gzip")
					gz := gzip.NewWriter(rw)
					defer func() { _ = gz.Close() }()
					out = gz
				}
				_ = pdftest.Filler(writerFunc(func(p []byte) (int, error) {
					n, err := out.Write(p)
					served.Add(int64(n))
					return n, err
				}), extraction.MaxPDFSize+1)
			})
			resp, err := fetch(ctx, link)
			if err != nil || resp.Format != "pdf" || !resp.TooLarge || resp.PDF != nil {
				t.Errorf("format %q too large %v file %v, %v; want a PDF too large, without file", resp.Format, resp.TooLarge, resp.PDF != nil, err)
			}
			// With its exact length, it is refused on its Content-Length before its body.
			if got := served.Load(); variant != "exact length" && got < extraction.MaxPDFSize {
				t.Errorf("%d bytes served, want the body read up to the limit", got)
			}
		})
	}

	t.Run("a body that times out keeps its format and final URL", func(t *testing.T) {
		w, _, fetch := setup(t, 300*time.Millisecond)
		const link, final = "https://docs.example.com/download", "https://cdn.example.com/files/lent.pdf"
		w.handle(link, func(rw http.ResponseWriter, r *http.Request) { http.Redirect(rw, r, final, http.StatusFound) })
		w.handle(final, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "application/pdf")
			_, _ = rw.Write(pdf[:1000])
			rw.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		resp, err := fetch(ctx, link)
		if err == nil || extraction.Classify(ctx, err) != "timeout" {
			t.Fatalf("error %v, want a timeout", err)
		}
		if resp.Format != "pdf" || resp.URL == nil || resp.URL.String() != final || resp.PDF != nil {
			t.Errorf("format %q URL %v file %v; want pdf, the final URL, no file left open", resp.Format, resp.URL, resp.PDF != nil)
		}
	})

	t.Run("a write error of the file is the NAS's, a cut body is the site's", func(t *testing.T) {
		w, dir, fetch := setup(t, 5*time.Second)
		const full, cut = "https://docs.example.com/full.pdf", "https://docs.example.com/cut.pdf"
		w.page(full, "application/pdf", pdf)
		w.handle(cut, func(rw http.ResponseWriter, _ *http.Request) {
			rw.Header().Set("Content-Type", "application/pdf")
			rw.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
			_, _ = rw.Write(pdf[:len(pdf)/2]) // the connection ends before the length announced
		})
		resp, err := fetch(ctx, cut)
		if err == nil || extraction.Classify(ctx, err) != "unreachable" || resp.Format != "pdf" {
			t.Errorf("cut body: format %q, %v (%s); want unreachable", resp.Format, err, extraction.Classify(ctx, err))
		}
		extraction.FailWrites(dir)
		resp, err = fetch(ctx, full)
		if err == nil || extraction.Classify(ctx, err) != "storage_unavailable" || resp.PDF != nil {
			t.Errorf("failed write: file %v, %v (%s); want storage_unavailable", resp.PDF != nil, err, extraction.Classify(ctx, err))
		}
	})

	t.Run("a PDF is not written without room for it on the NAS", func(t *testing.T) {
		const known, unknown = "https://docs.example.com/known.pdf", "https://docs.example.com/unknown.pdf"
		cases := []struct {
			name, link string
			free       uint64
			want       string
		}{
			{"known length, room enough", known, uint64(len(pdf)) + extraction.FreeSpaceMargin, ""},
			{"known length, one byte short", known, uint64(len(pdf)) + extraction.FreeSpaceMargin - 1, "storage_unavailable"},
			{"unknown length, room for the largest PDF", unknown, extraction.MaxPDFSize + extraction.FreeSpaceMargin, ""},
			{"unknown length, one byte short", unknown, extraction.MaxPDFSize + extraction.FreeSpaceMargin - 1, "storage_unavailable"},
		}
		for _, tc := range cases {
			w, dir, fetch := setup(t, 5*time.Second)
			w.handle(known, func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Content-Type", "application/pdf")
				rw.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
				_, _ = rw.Write(pdf)
			})
			w.handle(unknown, func(rw http.ResponseWriter, _ *http.Request) {
				rw.Header().Set("Content-Type", "application/pdf")
				rw.(http.Flusher).Flush() // chunked: no length
				_, _ = rw.Write(pdf)
			})
			extraction.SetFreeSpace(dir, func() (uint64, bool, error) { return tc.free, true, nil })
			resp, err := fetch(ctx, tc.link)
			if tc.want == "" {
				if err != nil || resp.PDF == nil {
					t.Errorf("%s: file %v, %v; want the PDF written", tc.name, resp.PDF != nil, err)
				}
				_, _ = extraction.PDFBytes(resp)
				continue
			}
			if err == nil || extraction.Classify(ctx, err) != tc.want || resp.PDF != nil || resp.Format != "pdf" {
				t.Errorf("%s: file %v, %v (%s); want %s", tc.name, resp.PDF != nil, err, extraction.Classify(ctx, err), tc.want)
			}
		}
	})

	t.Run("a directory that cannot be written is the NAS's", func(t *testing.T) {
		w := newWeb(t)
		dir, path := tempDir(t)
		fetch := extraction.Fetcher(testPolicy, w, w.roots, 5*time.Second, dir)
		const link = "https://docs.example.com/nas.pdf"
		w.page(link, "application/pdf", pdf)
		readOnly(t, path)
		_, err := fetch(ctx, mustURL(t, link))
		if err == nil || extraction.Classify(ctx, err) != "storage_unavailable" || strings.Contains(err.Error(), path) {
			t.Errorf("error %v (%s); want storage_unavailable without path", err, extraction.Classify(ctx, err))
		}
	})
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestFetchModes(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*web, *extraction.TempDir, string, func(context.Context, string, extraction.FetchRequest) (extraction.Response, error)) {
		w := newWeb(t)
		dir, path := tempDir(t)
		fetch := extraction.FetcherWithRequest(testPolicy, w, w.roots, dir)
		return w, dir, path, func(ctx context.Context, raw string, req extraction.FetchRequest) (extraction.Response, error) {
			return fetch(ctx, mustURL(t, raw), req)
		}
	}
	jsonRequest := extraction.FetchRequest{Timeout: 5 * time.Second, Accept: "application/json", Mode: extraction.ModeJSON}
	htmlRequest := extraction.FetchRequest{Timeout: 5 * time.Second, Accept: "text/html", Mode: extraction.ModeHTML}

	t.Run("a JSON body is read whole up to 64 KiB, whatever its type", func(t *testing.T) {
		w, _, _, fetch := setup(t)
		const link = "https://api.example.com/fits"
		body := bytes.Repeat([]byte("x"), extraction.MaxJSONSize)
		w.page(link, "text/html", body)
		resp, err := fetch(ctx, link, jsonRequest)
		if err != nil || resp.TooLarge || !bytes.Equal(resp.Body, body) || resp.Status != 200 {
			t.Errorf("status %d, %d bytes, too large %v, %v; want the 64 KiB read", resp.Status, len(resp.Body), resp.TooLarge, err)
		}
		if got := w.headersOf(link).Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want the Accept of the call", got)
		}
	})

	t.Run("a JSON body over 64 KiB is too large", func(t *testing.T) {
		w, _, _, fetch := setup(t)
		const link = "https://api.example.com/over"
		w.page(link, "application/json", bytes.Repeat([]byte("x"), extraction.MaxJSONSize+1))
		resp, err := fetch(ctx, link, jsonRequest)
		if err != nil || !resp.TooLarge || resp.Body != nil {
			t.Errorf("%d bytes, too large %v, %v; want too large without body", len(resp.Body), resp.TooLarge, err)
		}
	})

	t.Run("a PDF is never written to disk in HTML mode", func(t *testing.T) {
		w, dir, path, fetch := setup(t)
		const link = "https://docs.example.com/video.pdf"
		w.page(link, "application/pdf", articlePDF())
		extraction.FailWrites(dir) // a write would fail as storage_unavailable
		resp, err := fetch(ctx, link, htmlRequest)
		if err != nil || resp.PDF != nil || resp.Format != "pdf" || len(resp.Body) > 512 {
			t.Errorf("format %q file %v body %d, %v; want a PDF not read beyond its first bytes, without file", resp.Format, resp.PDF != nil, len(resp.Body), err)
		}
		if names := entries(t, path); len(names) != 0 {
			t.Errorf("temporary directory holds %v", names)
		}
	})

	t.Run("an HTML page is read in HTML mode", func(t *testing.T) {
		w, _, _, fetch := setup(t)
		const link = "https://blog.example.com/page"
		w.page(link, "text/html", fixture(t, "article.html"))
		resp, err := fetch(ctx, link, htmlRequest)
		if err != nil || resp.Format != "html" || !bytes.Equal(resp.Body, fixture(t, "article.html")) {
			t.Errorf("format %q, %d bytes, %v; want the page", resp.Format, len(resp.Body), err)
		}
	})

	t.Run("a deadline of the call is a timeout, never a stop", func(t *testing.T) {
		w, _, _, fetch := setup(t)
		const link = "https://slow.example.com/page"
		w.handle(link, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "text/html")
			rw.(http.Flusher).Flush()
			<-r.Context().Done()
		})
		for _, req := range []extraction.FetchRequest{htmlRequest, jsonRequest} {
			req.Timeout = 200 * time.Millisecond
			begin := time.Now()
			_, err := fetch(ctx, link, req)
			if err == nil || extraction.Classify(ctx, err) != "timeout" {
				t.Errorf("mode %v: error %v (%s), want timeout", req.Mode, err, extraction.Classify(ctx, err))
			}
			if elapsed := time.Since(begin); elapsed > 3*time.Second {
				t.Errorf("mode %v: answer after %v, want the timeout of the call", req.Mode, elapsed)
			}
		}
	})
}
