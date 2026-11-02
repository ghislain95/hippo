# Design

## Context

See `proposal.md`. E2-05 (`openspec/changes/archive/2026-11-02-extract-web-page/design.md`) built the extraction: one worker goroutine, one item at a time; a fetcher with its own client, address policy and a single 15 s deadline; HTML bodies read in memory up to 2 MiB; each page read by a child process of the same binary (`hippo extract-page`) bounded at 48 MiB by a watchdog and at 10 s by the parent; an attempt row whose `started_at` is the crash marker. Today `readBody` (`internal/extraction/page.go`) sees `application/pdf` or a body starting with `%PDF-` and the worker calls `store.DeferExtraction`: the row gets `outcome = 'deferred_pdf'` and `QueuedItems` leaves it out for ever. For a type that is neither HTML nor PDF, `fetch` reads only 512 bytes to sniff `%PDF-`; an HTML-labelled body is read whole (2 MiB) before the sniff.

Library: `github.com/ledongthuc/pdf` at `v0.0.0-20261020125853-5e46805f718e`, the version approved by Ghislain and to be pinned in `backend/go.mod`. The behaviours this design relies on, read in the installed module source (`$GOPATH/pkg/mod/github.com/ledongthuc/pdf@v0.0.0-20261020125853-5e46805f718e/`):

| Behaviour | Source |
| --- | --- |
| `NewReader(io.ReaderAt, size)` reads the file through `ReadAt` and section readers, never whole | `read.go:119`, `read.go:165-173`, `read.go:787`, `read.go:841` |
| The header must be `%PDF-1.0` to `%PDF-1.7` followed by CR or LF: PDF 2.0 is refused as "not a PDF file" | `read.go:141` |
| `%%EOF` must be in the last 100 bytes, once trailing CR, LF, space and tab are trimmed | `read.go:145-154` |
| Panics while opening are recovered by `NewReaderEncrypted`; everywhere else the library panics on malformed input (`lex.go:81`, `read.go:759-797`, `page.go:450-1033`, `ps.go:100-113`) | `read.go:128-137` |
| The `/Prev` chain of cross-reference sections has no loop detection: a loop never returns from `NewReader` | `read.go:224-238` |
| Encryption: key over 128 bits refused (`malformed PDF: 256-bit encryption key`), `V` other than 1, 2, 4 refused; the messages quote the encryption dictionary | `read.go:943-975` |
| AES strings are decrypted without removing their padding: a title ends with up to 16 control characters | `read.go:1117-1135` |
| `Reader.Page(n)` walks the tree from the root for each page (quadratic) and loops for ever on a cycle (`continue Search`); `NumPage` trusts `/Count` | `page.go:25-61` |
| `Page.Resources()` follows `/Parent` without bound: a cycle loops for ever | `page.go:115-139` |
| `Value.Reader()` streams a content stream through its filters; `Interpret` lexes it as a stream | `read.go:829-861`, `ps.go:56-70` |
| A literal string token is accumulated whole in memory | `lex.go:230-250` |
| `read.go` imports `os` only for `Open(file)`, which Hippo does not call; no package of the module imports `net` | `read.go:72`, `read.go:99` |
| `Value.Text()` decodes PDFDocEncoding and UTF-16BE with BOM | `read.go:642-657` |

## Goals / Non-Goals

**Goals:**
- No PDF can crash the service or grow its memory: the service streams the body to disk, the child reads it bounded in memory and time, as for pages.
- Nothing of a PDF stays on the NAS, crash included.
- A PDF whose reading runs out of time still gives its text read so far and a reading time for the whole document.

**Non-Goals:**
- A text layout as good as a PDF viewer's: multi-column order, tables and rotated text come out in the order of the content stream.
- Concurrency: still one extraction at a time.

## Decisions

### Packages touched

| Area | Change |
| --- | --- |
| `backend/go.mod`, `go.sum` | `github.com/ledongthuc/pdf v0.0.0-20261020125853-5e46805f718e` |
| `backend/.golangci.yml` | The `depguard` rule `page-reading-only-in-extraction` also lists `github.com/ledongthuc/pdf` |
| `backend/internal/extraction` | `fetch.go`: sniff before choosing a sink, PDF streamed to a temporary file, 50 MiB, `Accept`. `page.go`: routing and reasons. `child.go`: request `format` and `deadline_ms`, the PDF as file descriptor 3, PDF reasons. New `pdf.go` (open, header overlay, bounded page walk, internal deadline, word count), `pdflayout.go` (the layout layer), `pdftitle.go` (RG-28 title order), `tempfile.go` (the temporary directory). `worker.go`: PDF fallbacks with their title, storage failure, deferral removed. New `pdftest/` (generator of test PDFs) |
| `backend/internal/store` | `extraction.go`: `QueuedItems` no longer leaves `deferred_pdf` rows out; `DeferExtraction` removed |
| `backend/cmd/hippo` | Opens `DATA_DIR/tmp` (created `0700`, leftovers removed, writability checked, as `openAssets` does) and passes it to the worker |
| `api/openapi.yaml` | None |
| Migration | None (see "Links left waiting as PDFs") |
| Prompt, fiche schemas | None: a PDF's content reaches the analysis as a page's does, `full` or `fallback` with the meaning the prompt gives them |
| `infra/`, `backend/Dockerfile` | None: `DATA_DIR` is the `./data` volume, already writable by uid 65532 |

`internal/extraction` stays the only package that downloads and reads (`docs/dev/conventions-backend.md`, "Dépendances entre couches"); the temporary directory is its own, not `assetfs`, which holds images and is imported only by `httpapi` and `cmd/hippo`.

### Library and layout layer

Four libraries were compared on 2026-11-02 on generated fictional PDFs and PDFs printed by Chrome (bench kept outside the repository; darwin/arm64; "peak" is the memory the Go runtime held):

| PDF | `ledongthuc` + our layer, read from a file | Note |
| --- | --- | --- |
| Article, 10 pages | 8.6 MiB, 3 ms, 1 951 words | Same words as pdfium |
| Chrome article, Type0 fonts | 9.9 MiB, 7 ms, 2 030 words | `digitorus`: 11 719 "words", Type0 not decoded |
| 400 pages, every page counted | 10.3 MiB, 98 ms, 78 040 words | |
| 600 pages, 100 MB, every page counted | 10.3 MiB, 103 ms | The same file read from memory: 191 MiB |
| 35 MB | 9.8 MiB | From memory: 78.7 MiB |
| Glyphs placed without spaces | 58 words | `GetPlainText`: 6 words; pdfium: 70 |
| Flate bomb, one page | 8.8 MiB, 6 ms (stop within the page) | `GetPlainText`: 231 MiB, 3.9 s |
| Page tree cycle, `/Prev` loop | killed at the time limit | Every Go library loops; pdfium reads them |
| AES-128, no open password | read; title with 9 trailing `\t` | |
| AES-256 (owner or user password) | refused at open | |
| 300 mutations of a valid PDF | about 8 % panics, all recovered | |
| pdfium (WebAssembly) | 36 to 38 MiB for any PDF, 264 MiB at the first start | Over the child's 48 MiB |

Decisions:
- **Read through `io.ReaderAt` on a file**, never from memory: the memory then follows the page being read, not the file.
- **Our own layout layer over `pdf.Interpret`** (adapted from the prototype of the comparison), not `Page.GetPlainText`: the latter keeps the whole text of a page (231 MiB on the bomb) and drops spaces that only positioning gives. The layer tracks the text matrix, glyph widths (`/Widths`, `/FirstChar`; `/W` and `/DW` of a CID font, at most 65 536 entries), `Tc`, `Tw`, `Tz`, `TL`, `Ts`, `TJ` offsets; it writes a space when a glyph starts more than 0.15 × font size after the end of the previous one, a line break when the baseline moves by more than half the font size, `\n\n` between pages; it follows form XObjects 4 levels deep. Text in render mode 3 (invisible: the OCR layer of a scanned PDF) is read.
- **Text clean-up** (lossless): ligatures U+FB00 to U+FB06 expanded to their letters; a soft hyphen (U+00AD) at a line end is removed with the line break, elsewhere removed; a line ending with `-` right after a letter is joined to the next line, the hyphen kept (`réfle-xion`, `porte-monnaie`), so that no letter is dropped and the word is counted once; `strings.ToValidUTF8`; control characters other than `\n` and `\t` removed.
- **Bounded page walk**: our own walk of `/Root /Pages`, depth first, in `/Kids` order, at most 32 levels, 40 000 nodes and `MaxPDFPages` = 20 000 pages; inherited `/Resources` carried down the walk, never looked up through `/Parent`. `Reader.Page`, `NumPage` and `Page.Resources` are never called. A cycle ends at the depth or node bound (its pages may be read twice: hostile files only).
- **Header overlay**: the `io.ReaderAt` given to the library returns `%PDF-1.7\n` for the first 9 bytes when the file starts with `%PDF-` followed by a digit, a dot and a digit; offsets do not move, the header line is a comment. This accepts PDF 2.0 and a header followed by a space. The rest of a 2.0 file is read as 1.7 syntax, which it shares; what it does not share (AES-256 is the only encryption of 2.0) falls back. *Unverified* on real 2.0 producers; the generated 2.0 PDF of the tests proves the overlay only.
- **Panics**: recovered around the opening of the document, around the reading of its title, and around each page: a page that panics is skipped (`A page that cannot be read is skipped`), and the document falls back only when no text is left. A panic outside these is recovered by the child as `reading_failed`.

### Reading a PDF in the child

The child keeps E2-05's limits: watchdog at 48 MiB (`ChildMemoryLimit`, exit 86), killed by the parent at `ReadTimeout`, 10 s. A literal string of 100 MB, held whole by the lexer, reaches the watchdog: `too_large`.

- **Request.** The JSON line gains `format` (`html`, the default, or `pdf`) and, for a PDF, `deadline_ms`. For `pdf`, stdin holds the line only; the PDF is file descriptor 3 (`exec.Cmd.ExtraFiles[0]`), opened read-only; the child takes its size by `Stat` on the descriptor. Go opens every other descriptor close-on-exec: the child inherits none of the database's.
- **Why a descriptor rather than a path** (decided here): the file can lose its name before the download starts, so that nothing remains after any crash; no path of `DATA_DIR` crosses the process boundary nor reaches a log; no file can be swapped between the download and the reading. A path would need the name to live until the child opens it, and a sweep after every crash.
- **Internal deadline.** The child reads the document in a goroutine; the main goroutine waits for it or for `deadline_ms`, which the parent sets at 80 % of its `ReadTimeout` (8 s in production). At the deadline, the main goroutine takes what the reading goroutine has committed, writes the answer and exits: an infinite loop inside the library (a `/Prev` loop at the opening, an unknown one later) costs the text not yet read, not the item. `GOMAXPROCS=2` and Go's asynchronous preemption keep the main goroutine running. The 10 s kill stays for a child that does not answer at all.
- **What the reading commits**, under a mutex, once per page and when the kept text reaches its limit: the text kept, the words counted, the pages started.
- **Kept text and words.** The text is kept until it holds `MaxContentLength` + 1 code points, even within a page; then the layer stops keeping and only counts words: a word is a run of non-space code points (`unicode.IsSpace`), so that the count equals `strings.Fields` over the whole text. The kept text goes through `truncate` (E2-05, `text.go`). Reading time: `max(1, ceil(estimatedWords / 230))` with `estimatedWords = words × pagesFound ÷ pagesStarted`, which is `words` when every page was read. A reading stopped by the deadline marks `content_truncated` even when the text kept is under 120 000 code points: the content does not hold the whole document (RG-24).
- **Outcome.** `full`, reason `ok`, when the kept text holds at least `minFullLength`, 200 code points (E2-05). Otherwise `fallback`: `short` (scanned, no text), `encrypted` (needs a password, or an encryption the library refuses: AES-256, unknown `V`), `damaged` (any other failure to open, a file that is not a PDF despite its type included), `timeout` (deadline reached with less than 200 code points). The parent accepts these reasons for `format = pdf` only.
- **Title** (`pdftitle.go`, RG-28): the `/Info /Title` text, control characters removed (the AES padding), whitespace collapsed; junk when empty, starting with `Microsoft Word - `, `Microsoft PowerPoint - ` or `Microsoft Excel - `, equal to `Untitled`, `Sans titre`, `Document1` or `PowerPoint Presentation` ignoring case, or a file name: its last component after `/` or `\` ends, right after a character that is not a space, with `.` and one of `doc docx odt rtf txt pdf ps tex dvi htm html xhtml md ppt pptx odp key xls xlsx ods numbers pages indd tif tiff jpg jpeg png`, ignoring case (so `Node.js en production` is a title). Else the last non-empty path segment of the final URL when it ends with `.pdf` ignoring case, percent-decoded (`url.PathUnescape`, the raw segment when it fails), suffix removed, cleaned the same way. Else the provisional title. Then `cutTitle` (1 000 code points, E2-05). The site name is always the host of the captured URL in the provisional-title form, as for a page's fallback.

### Downloading a PDF

- **Sniff before the sink.** After the headers of a 2xx answer, the body is wrapped in a `bufio.Reader` of 4 KiB and its first 512 bytes peeked. A PDF is an answer whose media type is `application/pdf` or whose first bytes are `%PDF-`, whatever its declared type (an HTML-labelled PDF included). Else E2-05's paths: HTML in memory up to `MaxBodySize` (2 MiB), anything else not read beyond the peek.
- **Size.** `MaxPDFSize` = 50 MiB (52 428 800 bytes), the "50 MB" of the proposal as the "2 MB" of pages are 2 MiB. A `Content-Length` above it: fallback `too_large` without reading the body. Otherwise the body is copied through `io.LimitReader(MaxPDFSize + 1)` on the decompressed stream: a lying `Content-Length` or a gzip bomb stops at the limit; a body that ends before its `Content-Length` is a read error (`unreachable`), one longer is cut by the transport and arrives damaged.
- **Memory of the service**: `io.Copy` with its 32 KiB buffer, the 4 KiB peek; nothing grows with the PDF (scenario `A large PDF does not grow the memory of the service`).
- **Time.** The single 15 s deadline of the download covers the PDF as a page (RG-23, decided here: RG-23 names 15 s and does not distinguish PDFs). 50 MiB in 15 s needs about 28 Mbit/s from the site to the NAS; a slower PDF falls back (`timeout`). The reading has its own 10 s, as for a page.
- **Headers.** `Accept: text/html,application/xhtml+xml,application/pdf;q=0.9`: a site that negotiates the type answers a PDF link instead of `406`, and keeps preferring HTML when it has both.
- **Format known on failure.** `fetch` returns the format it recognised with its error, so that a PDF whose body times out or is too large falls back with the PDF title order (file name of the URL), and a failure before the headers with the provisional title, as today.

### Temporary directory

- `DATA_DIR/tmp`, created `0700` by `cmd/hippo` at start-up, accessed through an `os.Root`, used by the extraction only. At start-up every regular file in it is removed (count logged, as for assets) and a probe file is created and removed: the service fails at start rather than at the first PDF when it cannot write. Not `os.TempDir()`: `/tmp` in the container is the image's writable layer, or a tmpfs that would count against the 128 MB limit; not `DATA_DIR/assets`, whose files and `.tmp-` convention belong to images.
- Per PDF: create `.pdf-<16 random bytes in hex>` (`O_RDWR|O_CREATE|O_EXCL`, `0600`), open it a second time read-only, remove its name, then download into the first descriptor. The read-only descriptor goes to the child; both are closed by the worker on every path (written, fallback, released, stopped). The data disappears with the last descriptor, crash of the service or of the child included. Only the instant between create and remove can leave a name: the start-up sweep removes it.
- **Storage failure.** A failure to create, open or write the file (no room, read-only volume, I/O error) is the NAS's: the attempt is released, the item stays `queued`, an error is logged (`item_id`, reason `storage_unavailable`, the error made pathless as in `assetfs`), and the pass ends and pauses `OutagePause`, as for `reader_unavailable`. A write error is told from a read error of the network by wrapping the file's writer.
- **Backups** (ADR-0020) copy the database and `assets`: a file without name is never seen.
- **Memory of the container.** The pages written to the file are page cache charged to the container's cgroup until written back and reclaimed; they are reclaimable, unlike the service's heap. Measured in task 6.1 (see Risks).

### Links left waiting as PDFs

`QueuedItems` drops its `a.outcome IS NULL` condition: a row left `deferred_pdf` by E2-05 has `started_at` null, so the link is an ordinary queued link (not a crash), downloaded once more, then its row is deleted by `WriteExtraction` with the result, as any row. `DeferExtraction`, `outcome.Deferred` and `reasonDeferredPDF` are removed. The column `outcome` stays in `extraction_attempts`, no longer written nor read: dropping it would take a migration for nothing observable; to clean up with the next migration of the table.

Alternatives: a migration clearing `outcome`, or rebuilding the table without it (rejected: a schema change for a value nobody reads); a dedicated pass for deferred rows (rejected: the ordinary pass already does it, crash marker and network retries included).

### What is written for a PDF

- `full`: `title` per RG-28 order, `site_name` the host, `content_text` the kept text truncated, `content_truncated`, `reading_minutes` as above.
- `fallback`: `title` per RG-28 order from what was obtained (metadata title only when the child opened the document and answered), `site_name` the host, `content_text` null, `content_truncated` false, `reading_minutes` null. Parent-side fallbacks (download timeout or size after the headers, child killed at its memory or time limit, unreadable answer) use the file name of the final URL, else the provisional title.
- The write itself is E2-05's `WriteExtraction`, unchanged.

### Logs

One line per extraction as in E2-05, plus `format` (`pdf`) on a PDF's line. New reason classes: `encrypted`, `damaged`, `storage_unavailable` (error level, item left queued); `short`, `too_large`, `timeout`, `reading_failed` are reused. Never: the URL or its file name, the title, the content, the path of the temporary file, an error of the PDF library (its messages quote the encryption dictionary or bytes of the file) — the child maps them to a reason and never writes them out; stderr stays discarded. `deferred_pdf` disappears from the logs.

### Test PDFs

Generated by Go code, never committed as files (public repository, ADR-0027; no font program, so no font licence; sizes up to 50 MiB; hostile files that scanners might flag): a package `internal/extraction/pdftest`, as `pagestest` for pages. It writes PDF objects, a cross-reference table with exact offsets, and optionally FlateDecode streams, from a description: pages of lines, a simple font with `/Widths` (WinAnsiEncoding) or a Type0 font (`Identity-H`, `/W`, a `ToUnicode` CMap, no embedded program), glyph-by-glyph `TJ` placement without spaces, metadata title as raw bytes (UTF-16BE with BOM, tabs, junk), header version, image-only pages, AES-128 encryption without open password (real, `crypto/aes` and `crypto/md5`), an encryption dictionary that needs a password, an AES-256 dictionary (`/V 5 /R 6 /Length 256`: refused before any decryption, so no AES-256 code is needed), and the hostile shapes (page tree that contains itself, `/Prev` loop, page content that cannot be decoded, malformed `/Info`, one page decompressing to more than 100 MB, a literal string over 100 MB, truncated file, wrong `startxref`). Bodies over 50 MiB are streamed by the test server from a generator, never built in memory nor on disk; the 45 MB PDF of the memory scenario is written once under `t.TempDir()` and served with `http.ServeContent`, so that the test server does not hold it either. `backend/testdata/pdfs/` is not created.

Fixtures are fictional; the generator imitates the producers measured in the comparison (Chrome: Type0 and per-glyph `TJ`; LaTeX: per-word `TJ` offsets and ligatures; Word: `Tj` per word with `Td`), which the CI cannot read from real files.

The layout scenarios are also tested in process against the reading function (fast, no child); every scenario of the spec has its sub-test named after it in `worker_test.go`, through the worker, the child (the test binary, E2-05) and `httptest`. The scenario `PDF whose reading reaches the time limit` uses a test mode of the child whose clock passes the deadline once 50 pages are committed (no sleep). The descriptor check of `No file is left after a PDF` lists `/proc/self/fd` on Linux and `/dev/fd` on macOS.

### user_id, private items and budget

Unchanged from E2-05: every query filters by `user_id`; the pickup of deferred rows goes through `QueuedItems`, per user (scenario `Links left waiting as PDFs of two users`). Private PDFs are downloaded and read (RG-92 exception); nothing goes to the AI (RG-12); no budget check, no AI call.

## Risks / Trade-offs

- [The page cache of a 50 MiB file counts in the 128 MB cgroup] → reclaimable, but measured before merge: in Docker (`--memory 128m`, linux/arm64, the image), the server running, a 45 MB PDF downloaded and read; record `memory.peak`, `memory.stat` (anon, file) and `memory.events` (`oom_kill` must stay 0) in this design and in `docs/spec-v1.md`. If an OOM kill happens, lower `MaxPDFSize` and tell Ghislain (his decision of 50 MB is then not holdable).
- [15 s for 50 MiB on a slow link] → a large PDF falls back with its file name as title; the user keeps the link. A longer limit is a change of RG-23 (Open Questions).
- [Layout of multi-column pages, tables, rotated text] → text in content-stream order; the AI reads it; no information dropped.
- [A mixed PDF, scanned pages with a text cover] → `full` with the cover only, and a reading time that ignores the scanned pages.
- [PDF 2.0 read as 1.7] → *unverified* on real 2.0 files; a 2.0 file the library misreads falls back.
- [`%%EOF` beyond the last 100 bytes (trailing junk), a header after leading bytes] → refused by the library: `damaged`, fallback.
- [The library has no tags and few maintainers] → pinned pseudo-version in `go.sum`; its use is confined to `pdf.go` and `pdflayout.go`, behind the child.
- [Unknown infinite loops in the library] → internal deadline, then the 10 s kill; the service never waits more than 10 s.
- [A string token of 100 MB] → the watchdog kills the child: fallback `too_large`, the text of other pages lost.
- [A full disk] → items stay `queued` and each pass re-downloads the first PDF then pauses 5 minutes; SQLite fails as well on a full disk.
- [Deferred PDFs re-downloaded weeks later] → a link gone meanwhile falls back; a single-use link was already consumed (RG-92).
- [A PDF link answered as HTML to `Accept` with `application/pdf`] → no change for sites that ignore `Accept`; a site that negotiated HTML before keeps HTML (higher `q`).

## Migration Plan

No migration. At the first start of the new version, `DATA_DIR/tmp` is created and the rows left `deferred_pdf` are picked up by the first pass. Rollback to the previous image: same schema; it ignores `DATA_DIR/tmp` and defers PDF links again; the items already extracted stay extracted.

## Open Questions

None left. Resolved on 2026-11-02, within the rules Ghislain decided:

1. The download limit stays 15 s for PDFs (RG-23 unchanged); a slower PDF falls back with its file name.
2. The junk titles cover the Office prefixes (`Microsoft Word - `, `Microsoft PowerPoint - `, `Microsoft Excel - `) and `Untitled`, `Sans titre`, `Document1`, `PowerPoint Presentation`: the same kind of title RG-28 names.
3. A reading stopped by its time limit is marked `content_truncated` (RG-24: the content does not hold the whole document).
4. The file name is taken from the URL the PDF was served from, after redirects.
