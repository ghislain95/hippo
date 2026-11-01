# Design

## Context

See `proposal.md`. Captures insert items `queued` (`store.CaptureItem`, one `BEGIN IMMEDIATE` transaction, `change_seq` through `sequence.go`). Nothing reads them afterwards. The backend has one process (`cmd/hippo/main.go`: config, store, server, graceful stop), a 128 MB container (`infra/docker-compose.yml`), and no outbound HTTP yet. Only `createItem` and `getAsset` are implemented, so the extraction is observed through the store in tests (no read route before E4-02), and `Item` never exposes `content_text`.

Readability is `codeberg.org/readeck/go-readability/v2@v2.1.3`, verified on its sources: `FromURL` uses the default transport without context nor limit (`readability.go:40-70`), `dom.Parse` reads the whole body and guesses the charset statistically (go-shiori/dom `parser.go:28-50`), `grabArticle` clones the document up to four times (`parser.go:748`), `MaxElemsToParse` is 0 (unbounded) by default (`parser.go:77,113`), `ParseAndMutate` reads an already parsed document without cloning (`parser-parse.go:30`), `Article.RenderText` keeps blocks and `<pre>` (`article.go:34`).

## Goals / Non-Goals

**Goals:**
- No page can crash the service, or crash it in a loop.
- No stop of the backend nor network outage of the NAS turns a link into a permanent fallback.
- The NAS never connects to a local or private address (RG-27).

**Non-Goals:**
- Concurrency: one extraction at a time (`docs/spec-v1.md`, pipeline: "une par une"), oldest first.
- Re-extraction of links that fell back.

## Decisions

### Packages touched

| Area | Change |
| --- | --- |
| `backend/internal/extraction` (new) | The worker (wake-up, scan per user, one item at a time), the HTTP fetcher with its address policy, the page reader (charset, `og:*`, Readability, text, truncation, reading time). A domain package that may import `net/http` (`docs/dev/conventions-backend.md`, "Dépendances entre couches", which already names `extraction`). Only this package imports Readability and `golang.org/x/net/html` (new `depguard` rule). |
| `backend/internal/store` | `00005_extraction_attempts.sql`; `extraction.go`: list users, next `queued` items per user, start an attempt, write the result, defer an item. |
| `backend/internal/capture` | Declares a `Waker` interface (`Wake()`), called after a capture that created an item (links, notes, images). |
| `backend/cmd/hippo` | Builds the worker with the production policy, starts it, wakes it at start-up, stops it before closing the store. |
| `api/openapi.yaml` | None: `ExtractionMode`, `site_name`, `reading_minutes`, `content_truncated`, `extracted_at` are already declared. |

Prompt and fiche schemas: unchanged (`fallback` and `none` keep the meaning the prompt gives them).

### Migration `00005_extraction_attempts.sql` (new, SQL)

`extraction_attempts (item_id TEXT PRIMARY KEY, user_id TEXT NOT NULL, started_at TEXT NOT NULL, outcome TEXT CHECK (outcome IN ('deferred_pdf')), FOREIGN KEY (item_id, user_id) REFERENCES items (id, user_id)) STRICT`. Not synced, no `change_seq`. A row is written, and committed, before a link is downloaded; it is deleted when the item is written as `extracted`, kept with `outcome = 'deferred_pdf'` for a PDF (E2-08 picks those up), and kept as is after a stop that left the item `queued` on purpose (see below, it is then deleted).

### Worker

- One goroutine, woken by a buffered channel of size 1 (non-blocking send) after each capture that created an item, at start-up, and every 5 minutes as a safety net.
- Each pass lists the users, then, per user, `SELECT … FROM items WHERE user_id = ? AND status = 'queued' AND deleted_at IS NULL ORDER BY id` (RG-90), excluding links with a `deferred_pdf` attempt and YouTube hosts.
- Notes and images: written `extracted`, `extraction_mode` `none`, no network.
- YouTube links (`youtube.com`, any subdomain, `youtu.be`, host compared lower-case): skipped, left `queued` (E2-06).
- A link with an attempt row and no outcome: the previous attempt did not finish (crash, kill by lack of memory). It is written `fallback` with its provisional title, without downloading again.
- Otherwise: insert the attempt row, commit, download and read outside any transaction, then write the result.

### Stop and network outage (B2, C2)

- The worker runs with its own context, cancelled by `main` on shutdown; `main` waits for the worker to return before `st.Close()`.
- A download cancelled by that context writes nothing and deletes the attempt row: the item stays `queued` and is retried after the restart.
- An error that comes from the NAS side — DNS resolution failing for any host (`*net.DNSError` with `IsTemporary` or `IsNotFound` on the resolver itself), no route, network unreachable — writes nothing, deletes the attempt row, and pauses the pass for 5 minutes. A site that does not answer, a refused connection or a TLS error are failures of the site: fallback.

### Fetching

An `http.Client` of its own, never `http.DefaultClient` nor `readability.FromURL` (checked in review):

- `Transport.Proxy = nil`; `DialContext` through a `net.Dialer` whose `Control` checks each resolved address against the address policy (DNS rebinding and redirects covered); `TLSHandshakeTimeout`, `MaxResponseHeaderBytes = 64 KiB`, no persistent cookie jar (an ephemeral jar per item, so that consent redirects do not loop).
- `CheckRedirect`: at most 10, `http` and `https` only.
- One deadline of 15 s for the whole download (DNS, TLS, redirects, headers, body), through the request context derived from the worker's.
- User info is removed from the URL before the request (`net/http` would send it as Basic Auth).
- Headers: `User-Agent: Hippo/<version> (lecture différée personnelle)` (decided with Ghislain), `Accept: text/html,application/xhtml+xml`, `Accept-Language: fr,en`. No manual `Accept-Encoding`, so Go's transparent gzip stays on.
- Body read through `io.LimitReader(limit + 1)` on the decompressed stream (decompression bombs and lying `Content-Length` covered); over the limit → fallback. The limit is 5 MB, lowered to 2 MB if the heap peak measured on a 5 MB fixture exceeds 40 MB (task 3.3; the result is written here).
- The URL is the item's trimmed `url`; the final URL after redirects is the base for Readability.

### Address policy (RG-27)

Allowed only when `addr.Unmap()` is `IsGlobalUnicast() && !IsPrivate()` and has no IPv6 zone, and is not in `0.0.0.0/8`, `100.64.0.0/10`, `192.0.0.0/24`, `198.18.0.0/15`, `240.0.0.0/4`, `64:ff9b::/96`, `2002::/16`, `fec0::/10`. All ports are allowed: blocking ports would add nothing once addresses are filtered. The policy is a `func(netip.AddrPort) bool` given to the worker; `cmd/hippo` passes the production one; tests pass one that also allows `127.0.0.1` for their `httptest` servers, and a dedicated test runs the production policy against `httptest` on `127.0.0.1` and `localhost` (fallback, zero request received) and a redirect from an allowed server to a refused one (the second receives nothing). Accepted with Ghislain's rule as written: a link to the home's public address goes through the box (NAT loopback); RG-27 does not cover it.

### Reading a page

1. Status 2xx and a `Content-Type` of `text/html` or `application/xhtml+xml` (or none and `http.DetectContentType` says HTML). `application/pdf`, or a body starting with `%PDF-`: the item is deferred (`deferred_pdf`, stays `queued`, E2-08). Any other type, or a non-2xx status: fallback, `og:*` not read (a 404 page's `og:title` would be "Page introuvable").
2. Decode with `golang.org/x/net/html/charset.NewReader(body, contentType)`, which honours the header and `<meta charset>`; then `strings.ToValidUTF8` on every text kept.
3. One `html.Parse`; read `og:title`, `og:description`, `og:site_name` and `<title>`; then `Parser.ParseAndMutate(doc, finalURL)` with `MaxElemsToParse = 20 000`.
4. `full` when the article text (`RenderText`) has at least 200 characters after trimming; otherwise fallback (a JavaScript-only page).

### What is written

- `full`: `title` = Readability title, else `og:title`, else `<title>`, else the provisional title, whitespace collapsed; `site_name` = `og:site_name`, else Readability's, else the host in the provisional-title form (lower case, no port); `content_text` = the text cut as below; `content_truncated`; `reading_minutes` = `ceil(words / 230)` on the text before truncation (RG-25: "de l'original"), at least 1.
- `fallback`: `title` = `og:title`, else `<title>`, else the provisional title (only from a 2xx answer); `site_name` as above when readable, else the host; `content_text` = `og:description` or null; `reading_minutes` null; `content_truncated` false.
- Notes and images (`none`): no content, `reading_minutes` null.
- Truncation (RG-24): count code points; beyond 120 000, cut at the last whitespace within the last 1 000 code points before the limit, else at the limit; never inside a UTF-8 sequence.
- Write: one short transaction, `UPDATE items SET … WHERE user_id = ? AND id = ? AND status = 'queued' AND deleted_at IS NULL`, extraction columns only (never `user_note`, `is_private`), `updated_at` and `extracted_at` from the store clock, `change_seq` through `sequence.go`; zero row → ignored (deleted or changed meanwhile); the attempt row is deleted in the same transaction.

### Logs (C8)

One line per extraction: `item_id`, mode, a reason class (`ok`, `timeout`, `status`, `not_html`, `too_large`, `blocked`, `short`, `crashed_before`, `deferred_pdf`, `deferred_youtube`, `network`), the HTTP status when there is one, and the duration. The download error is never logged nor stored (`*url.Error` prints the URL); `last_error` stays null.

### Tests

The `httpapi` and `capture` tests run without worker (a no-op waker), so the existing scenarios stay deterministic. The `extraction` tests run the worker on a real SQLite file and `httptest` servers serving fictional fixtures in `backend/testdata/pages/` (no real page, public repository): minified HTML, windows-1252 / ISO-8859-1, JavaScript-only, `og:*` only, 404 with `og:*`, oversized, gzip, a 920-word article, a 200 000-character article. The crash scenario is reproduced by inserting the attempt row through the store before starting the worker.

### user_id, private items and budget

Every query filters by `user_id` (the scan is per user, the write and the attempt rows carry it); a test with two users checks that each item is written for its own user. Private items are fetched (RG-92 exception) and never sent to the AI. No AI call, no budget check.

## Risks / Trade-offs

- [Memory of Readability on large pages] → body limit, `MaxElemsToParse`, one extraction at a time, the crash marker; measured in task 3.3.
- [Sites refusing an honest User-Agent] → they fall back; to revisit with real results.
- [A single-use link is consumed by the download] → accepted with the RG-92 exception.
- [`search_text` is still empty and the title changes at extraction] → the search story (E4-01) builds `search_text`.
- [First start with many `queued` items] → processed one by one, up to 15 s each; notes and images advance `change_seq` once each.
- [A YouTube or PDF link waits `queued` until E2-06 / E2-08] → nothing analyses items before E3.

## Migration Plan

`00005_extraction_attempts.sql` at start-up (ADR-0032). Existing `queued` items are extracted by the first pass. Rollback is the backup (ADR-0020).
