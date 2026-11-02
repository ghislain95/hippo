# Proposal

## Why

A link answered as a PDF stays `queued` since E2-05 (`deferred_pdf` in `extraction_attempts`), so it never reaches the analysis. Papers, reports and manuals are often shared as PDF links; reading their text locally, without AI, gives them a fiche like any web page (ADR-0008). This change implements backlog story **E2-08 — Extraire un PDF**, added at Ghislain's request on 2026-11-01.

## What Changes

- A link answered with a success status as a PDF (`application/pdf`, or a body starting with `%PDF-` whatever its declared type) is read in the extraction child process (`hippo extract-page`, E2-05), with the same memory watchdog and time limit: its text becomes the content (`extraction_mode` `full`) when it holds at least 200 characters, kept up to 120 000 characters and marked `content_truncated` beyond (RG-24, RG-28). The site name is the host name.
- The reading time counts the words of the whole document, not only the text kept (RG-25, RG-28): the reading goes on counting after 120 000 characters without keeping the text. If the time limit comes first, the child returns what it read, the words of the pages not read are estimated pro rata of the pages, and the content is marked `content_truncated` since it does not hold the whole document.
- The title is the PDF metadata title, unless it is a junk title (`Microsoft Word - …`, a file name such as `report.docx` or `page.html`, `Untitled`, or nothing left once control characters and spaces are removed); else the file name of the URL the PDF was served from (after redirects), when its last path segment ends with `.pdf`, percent-decoded and without `.pdf` (`rapport-annuel-2025`); else the provisional title (RG-28, decided with Ghislain on 2026-11-02). The same order applies to a PDF that falls back: the metadata title when the document could be opened, else the file name of the URL, else the provisional title (RG-23: keep what can be obtained).
- A PDF up to **50 MB** (50 MiB, as the 2 MB of web pages are 2 MiB) is accepted; web pages keep 2 MB (decided with Ghislain on 2026-11-02). The backend streams the download to a temporary file under `DATA_DIR/tmp`, removed from the directory as soon as it is created, and hands it to the child as an open read-only descriptor; the file disappears with the last descriptor, crash included, and the memory of the service stays constant. Leftover files are removed at start-up. Above 50 MB, announced by `Content-Length` or read: fallback, without reading beyond the limit.
- The download keeps its single limit of **15 s** for everything (RG-23): a PDF of 50 MB needs about 28 Mbit/s from the site to the NAS; a slower one falls back like a slow page.
- A scanned PDF (less than 200 characters of text), a PDF that needs a password to open, a damaged PDF (truncated, broken cross-reference table, not a PDF despite its type), a PDF encrypted with AES-256 even without an open password, a PDF over 50 MB, and a PDF whose reading exceeds the memory or the time allowed fall back without content (RG-23, RG-28; AES-256 accepted as a V1 limit with Ghislain on 2026-11-02).
- A failure of the NAS to store the PDF (no room left, directory not writable) leaves the item `queued`: the pass goes on with the items behind it (notes, images and web pages need no temporary file), then pauses as after a NAS outage; the NAS is at fault, not the link.
- The links left `deferred_pdf` by E2-05 are picked up: they are no longer left out of the queue, and each is downloaded once more like any queued link. Nothing is deferred any more.
- Text extraction uses `github.com/ledongthuc/pdf` (BSD-3, no transitive dependency) for parsing and font decoding, with a layout layer of our own: glyph widths, spaces inferred from text positioning, line breaks from baselines, ligatures (U+FB00 to U+FB06) expanded, soft hyphens removed, a word cut by a hyphen at a line end joined with its hyphen kept (no letter is ever dropped), a stop of the kept text at 120 000 code points even within a page, a page-tree walk bounded in depth and in nodes, and an internal deadline that returns what was read (decided with Ghislain on 2026-11-02, after a comparison of four libraries). A PDF whose header says version 2.0 is presented to the library as 1.7, which it would otherwise refuse.
- The `Accept` header of the download adds `application/pdf`, so that a site that negotiates the type does not refuse a PDF link.

## Non-goals

- OCR of scanned PDFs. A scanned PDF that already carries an invisible text layer is read like any PDF.
- Sharing a PDF file (not a link) from the share sheet or the Files app: capture handles links, notes and images; noted in the backlog for later.
- Rebuilding a broken cross-reference table, AES-256 decryption, password prompts.
- Keeping the PDF file itself on the NAS or showing it in the app.
- The XMP metadata and the `Content-Disposition` file name as title sources: RG-28 names the metadata title (document information) and the file name of the URL only.
- A longer download limit for PDFs: RG-23 keeps 15 s.
- Any API change beyond the fields already in `Item`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `item-extraction`: a link answered as a PDF is read instead of waiting; the requirement « YouTube and PDF links wait for their own extraction » is replaced by one for YouTube only; the fallback requirement names the HTML limits; a NAS that cannot store a PDF leaves the item queued; the logs requirement covers PDFs.

## Impact

- **Story**: E2-08.
- **Business rules**: RG-12, RG-20, RG-23, RG-24, RG-25, RG-26, RG-27, RG-28 (new), RG-90, RG-92 (exception of E2-05: the URL is requested from its own site, private items included).
- **ADRs**: ADR-0003, ADR-0004, ADR-0008, ADR-0019 (128 MB container, unchanged; re-measured with a 50 MB PDF, whose written pages count in the container's memory until reclaimed), ADR-0020 (backups never see the temporary file: it has no name), ADR-0027 (test PDFs generated by code, fictional), ADR-0033, ADR-0035.
- **Dependency** (approved by Ghislain on 2026-11-02): `github.com/ledongthuc/pdf`, pinned at the pseudo-version `v0.0.0-20261020125853-5e46805f718e` (the module has no tags), BSD-3, no transitive dependency, about +0.6 MB of binary. Compared with `github.com/digitorus/pdf` (cannot decode Type0 fonts, the common case on the web), `github.com/pdfcpu/pdfcpu` (no text extraction) and `github.com/klippa-app/go-pdfium` in WebAssembly (best reading, but 36 MiB for a small PDF and 264 MiB at the first start: does not fit the child limit nor the container).
- **AI cost**: none here; a PDF's content goes to the nightly analysis like a page's, within the same 120 000 characters.
- **Private items**: extracted like the others (RG-92 exception); nothing is sent to the AI.
- **Persisted data**: no migration. `extraction_attempts.outcome` is no longer written nor read (the rows left `deferred_pdf` by E2-05 are picked up, then deleted with their result). Temporary files under `DATA_DIR/tmp`, without name once created, removed at start-up if a crash left one.
- **API contract**: none.
- **Logs**: one line per extraction as today, with the new reason classes; never the URL, its file name, the title, the content, the temporary file's path, nor an error of the PDF library (its messages quote bytes of the file).
- **Security**: same outbound rules as web pages (RG-27); PDF parsing of hostile files confined to the child (memory, time, recovered panics), which receives the file as a read-only descriptor and nothing else; the library opens no file and no connection of its own.
- **Prompt and fiche schemas**: none.
- **User QA: no**.

## Open questions

None blocking. Questions for Ghislain that change nothing in this change unless he decides otherwise are listed in `design.md`, Open Questions.
