package pdftest

import (
	"bytes"
	"fmt"
	"io"
	"strings"
)

// vocabulary is prefix-free, so that a word cut in its middle is never another word.
var vocabulary = strings.Fields(`amber basalt cedar dune ember fjord glacier harbor island jungle
	kelp lagoon meadow nectar orchard prairie quartz reef savanna tundra upland valley willow yarrow zephyr`)

// IsWord tells whether w, without a final period nor a capital first letter, is a whole word of
// the generated texts.
func IsWord(w string) bool {
	w = strings.ToLower(strings.TrimSuffix(w, "."))
	for _, v := range vocabulary {
		if v == w {
			return true
		}
	}
	return false
}

// Sentences returns words words of the vocabulary as sentences of 9 to 15 words, each starting
// with a capital letter and ending with a period; seed varies the text.
func Sentences(words, seed int) []string {
	var sentences []string
	for written, n := 0, 0; written < words; n++ {
		length := min(9+(n*5+seed)%7, words-written)
		parts := make([]string, length)
		for i := range parts {
			parts[i] = vocabulary[(seed+written+i*7+n*3)%len(vocabulary)]
		}
		parts[0] = strings.ToUpper(parts[0][:1]) + parts[0][1:]
		sentences = append(sentences, strings.Join(parts, " ")+".")
		written += length
	}
	return sentences
}

// Pages lays sentences out on pages: their words in lines of perLine words, wordsPerPage words
// a page (the last page takes what is left).
func Pages(sentences []string, wordsPerPage, perLine int) []Page {
	words := strings.Fields(strings.Join(sentences, " "))
	var pages []Page
	for start := 0; start < len(words); start += wordsPerPage {
		pageWords := words[start:min(start+wordsPerPage, len(words))]
		var page Page
		for i := 0; i < len(pageWords); i += perLine {
			page.Lines = append(page.Lines, strings.Join(pageWords[i:min(i+perLine, len(pageWords))], " "))
		}
		pages = append(pages, page)
	}
	return pages
}

// Article is a document of pages pages holding words words of the vocabulary, titled title in
// its metadata, compressed, with a simple font; it returns its sentences too.
func Article(title string, pages, words int) (Doc, []string) {
	sentences := Sentences(words, 0)
	perPage := (words + pages - 1) / pages
	return Doc{Title: []byte(title), Compress: true, Pages: Pages(sentences, perPage, 12)}, sentences
}

// Large is a document of about size bytes: an article of pages pages of 300 words, and on
// every page an image of pseudo-random bytes that the page does not draw, as a report with
// photographs.
func Large(size, pages int) Doc {
	doc, _ := Article("Large fictional report", pages, 300*pages)
	doc.ImageBytes = max(0, size/pages-4<<10)
	return doc
}

// EnormousPage is a page whose content stream holds more than size bytes of text operators,
// lines of words of the vocabulary; compressed with its Doc, it is small on the wire.
func EnormousPage(size int) Page {
	return Page{Content: func(w io.Writer) error {
		var line bytes.Buffer
		line.WriteString("BT /F1 11 Tf 72 770 Td 14 TL\n")
		for i := 0; line.Len() < 64<<10; i++ {
			fmt.Fprintf(&line, "(%s %s %s %s %s %s) Tj T*\n", vocabulary[i%25], vocabulary[(i+3)%25], vocabulary[(i+6)%25],
				vocabulary[(i+9)%25], vocabulary[(i+12)%25], vocabulary[(i+15)%25])
		}
		for written := 0; written <= size; written += line.Len() {
			if _, err := w.Write(line.Bytes()); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "ET\n")
		return err
	}}
}

// HugeString is a page that shows a single literal string of more than size bytes.
func HugeString(size int) Page {
	return Page{Content: func(w io.Writer) error {
		if _, err := io.WriteString(w, "BT /F1 11 Tf 72 770 Td ("); err != nil {
			return err
		}
		chunk := bytes.Repeat([]byte("fictional "), 6554) // 64 KiB
		for written := 0; written <= size; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, ") Tj ET\n")
		return err
	}}
}

// Truncated is pdf cut in its middle.
func Truncated(pdf []byte) []byte {
	return pdf[:len(pdf)/2]
}

// WrongStartxref is pdf whose pointer to its cross-reference table points elsewhere.
func WrongStartxref(pdf []byte) []byte {
	i := bytes.LastIndex(pdf, []byte("startxref\n"))
	if i < 0 {
		panic("pdftest: no startxref")
	}
	head := pdf[:i+len("startxref\n")]
	var offset int
	if _, err := fmt.Sscanf(string(pdf[len(head):]), "%d", &offset); err != nil {
		panic(err)
	}
	return fmt.Appendf(bytes.Clone(head), "%d\n%%%%EOF\n", offset/2)
}

// Filler writes a body of size bytes that starts as a PDF and holds nothing else: the bodies
// over 50 MiB are streamed from it, never built in memory nor on disk.
func Filler(w io.Writer, size int64) error {
	head := []byte("%PDF-1.7\n")
	if _, err := w.Write(head[:min(int64(len(head)), size)]); err != nil {
		return err
	}
	line := append(bytes.Repeat([]byte{'%'}, 1023), '\n')
	for left := size - int64(len(head)); left > 0; left -= int64(len(line)) {
		if _, err := w.Write(line[:min(left, int64(len(line)))]); err != nil {
			return err
		}
	}
	return nil
}
