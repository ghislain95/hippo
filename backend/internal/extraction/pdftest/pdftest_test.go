package pdftest_test

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ghislain95/hippo/backend/internal/extraction/pdftest"
)

func TestGenerator(t *testing.T) {
	article, _ := pdftest.Article("Tuning SQLite", 3, 400)
	docs := map[string]pdftest.Doc{
		"article":       article,
		"two-byte font": {Font: pdftest.TwoByte, Placement: pdftest.Glyphs, Compress: true, Pages: []pdftest.Page{{Lines: []string{"Crème brûlée à l'œuf"}}}},
		"aes-128":       {Title: []byte("Copie fictive"), Encryption: pdftest.AES128, Pages: article.Pages},
		"aes-256":       {Encryption: pdftest.AES256, Pages: article.Pages},
		"with images":   {ImageBytes: 10_000, Pages: article.Pages, Encryption: pdftest.AES128},
		"broken info":   {Title: []byte("Notice"), BrokenInfo: true, Pages: article.Pages},
	}
	for name, doc := range docs {
		t.Run("every cross-reference offset points to its object: "+name, func(t *testing.T) {
			pdf := doc.Bytes()
			offsets := xrefOffsets(t, pdf)
			for i, off := range offsets {
				num := i + 1
				if doc.BrokenInfo && num == 6 {
					continue // on purpose: the metadata points at the catalog
				}
				if want := fmt.Sprintf("%d 0 obj", num); !bytes.HasPrefix(pdf[off:], []byte(want)) {
					t.Errorf("offset %d of object %d holds %q, want %q", off, num, pdf[off:off+12], want)
				}
			}
		})
	}

	t.Run("documents are deterministic", func(t *testing.T) {
		for name, doc := range docs {
			if !bytes.Equal(doc.Bytes(), doc.Bytes()) {
				t.Errorf("%s: two writes differ", name)
			}
		}
	})

	t.Run("large documents are streamed to their size", func(t *testing.T) {
		counter := &countingWriter{}
		if _, err := pdftest.Large(5<<20, 10).WriteTo(counter); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		if counter.n < 4<<20 || counter.n > 6<<20 {
			t.Errorf("document of %d bytes, want about 5 MiB", counter.n)
		}
	})

	t.Run("a filler body has its size and starts as a PDF", func(t *testing.T) {
		var b bytes.Buffer
		if err := pdftest.Filler(&b, 5000); err != nil {
			t.Fatalf("Filler: %v", err)
		}
		if b.Len() != 5000 || !bytes.HasPrefix(b.Bytes(), []byte("%PDF-")) {
			t.Errorf("filler of %d bytes starting %q", b.Len(), b.Bytes()[:8])
		}
	})

	t.Run("no PDF file is kept in the backend", func(t *testing.T) {
		err := filepath.WalkDir(filepath.Join("..", "..", ".."), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".pdf") {
				t.Errorf("PDF file in the repository: %s (generate it with pdftest)", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
	})
}

var xrefEntry = regexp.MustCompile(`(\d{10}) 00000 n `)

// xrefOffsets reads the offsets of the cross-reference table of pdf, by object number - 1.
func xrefOffsets(t *testing.T, pdf []byte) []int {
	t.Helper()
	start := bytes.LastIndex(pdf, []byte("\nxref\n"))
	if start < 0 {
		t.Fatal("no cross-reference table")
	}
	end := bytes.Index(pdf[start:], []byte("trailer"))
	var offsets []int
	for _, m := range xrefEntry.FindAllSubmatch(pdf[start:start+end], -1) {
		off, err := strconv.Atoi(string(m[1]))
		if err != nil {
			t.Fatal(err)
		}
		offsets = append(offsets, off)
	}
	tail := pdf[start+end:]
	var startxref int
	if _, err := fmt.Sscanf(string(tail[bytes.Index(tail, []byte("startxref"))+len("startxref\n"):]), "%d", &startxref); err != nil || startxref != start+1 {
		t.Errorf("startxref = %d, %v; want %d", startxref, err, start+1)
	}
	return offsets
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

var _ io.Writer = (*countingWriter)(nil)
