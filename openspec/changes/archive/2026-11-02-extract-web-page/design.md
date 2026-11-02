# Design

## Context

See `proposal.md`. Captures insert items `queued` (`store.CaptureItem`, one `BEGIN IMMEDIATE` transaction, `change_seq` through `sequence.go`). Nothing reads them afterwards. The backend has one process (`cmd/hippo/main.go`: config, store, server, graceful stop), a 128 MB container (`infra/docker-compose.yml`), and no outbound HTTP yet. Only `createItem` and `getAsset` are implemented, so the extraction is observed through the store in tests (no read route before E4-02), and `Item` never exposes `content_text`.

Readability is `codeberg.org/readeck/go-readability/v2@v2.1.3`, verified on its sources: `FromURL` uses the default transport without context nor limit (`readability.go:40-70`), `dom.Parse` reads the whole body and guesses the charset statistically (go-shiori/dom `parser.go:28-50`), `grabArticle` clones the document up to four times (`parser.go:748`), `MaxElemsToParse` is 0 (unbounded) by default (`parser.go:77,113`), `ParseAndMutate` reads an already parsed document without cloning (`parser-parse.go:30`), `Article.RenderText` keeps blocks and `<pre>` (`article.go:34`).

## Goals / Non-Goals

**Goals:**
- No page can crash the service, or crash it in a loop. *(Holds by the child process that reads each page, bounded in memory and in time: see "Reading a page in a child process".)*
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
| `backend/internal/store` | `00005_extraction_attempts.sql`; `extraction.go`: list users, next `queued` items per user, start, release or postpone an attempt, write the result, defer an item. |
| `backend/internal/capture` | Declares a `Waker` interface (`Wake()`), called after a capture that created an item (links, notes, images). |
| `backend/cmd/hippo` | Builds the worker with the production policy and its own binary as the page reader, starts it, wakes it at start-up, stops it before closing the store. Runs `hippo extract-page`, the child that reads one page, before anything else (no configuration, no database, no log). |
| `api/openapi.yaml` | None: `ExtractionMode`, `site_name`, `reading_minutes`, `content_truncated`, `extracted_at` are already declared. |

Prompt and fiche schemas: unchanged (`fallback` and `none` keep the meaning the prompt gives them).

### Migration `00005_extraction_attempts.sql` (new, SQL)

`extraction_attempts (item_id TEXT PRIMARY KEY, user_id TEXT NOT NULL, started_at TEXT, network_retries INTEGER NOT NULL DEFAULT 0, outcome TEXT CHECK (outcome IN ('deferred_pdf')), FOREIGN KEY (item_id, user_id) REFERENCES items (id, user_id)) STRICT`. Not synced, no `change_seq`. **`started_at` not null means a download is under way.** It is set, and committed, before a link is downloaded (the row is created, or a released one gets its `started_at` back, its retries kept); the row is deleted when the item is written as `extracted`, kept with `outcome = 'deferred_pdf'` and `started_at` cleared for a PDF (E2-08 picks those up). A stop that leaves the item `queued` on purpose clears `started_at` and keeps the row; a network outage of the NAS clears `started_at` and, when the outage counts, adds one to `network_retries` (see below). A row found at start-up with `started_at` set is the crash marker. *(Amended at implementation, before merge: `started_at` nullable and `network_retries` added, so that an outage is counted per item.)*

### Worker

- One goroutine, woken by a buffered channel of size 1 (non-blocking send) after each capture that created an item, at start-up, and every 5 minutes as a safety net.
- Each pass lists the users, then, per user, `SELECT … FROM items WHERE user_id = ? AND status = 'queued' AND deleted_at IS NULL ORDER BY id` (RG-90), excluding links with a `deferred_pdf` attempt and YouTube hosts.
- Notes and images: written `extracted`, `extraction_mode` `none`, no network.
- YouTube links (`youtube.com`, any subdomain, `youtu.be`, host compared lower-case): skipped, left `queued` (E2-06).
- A link with an attempt row whose `started_at` is set and no outcome: the previous attempt did not finish (crash, kill by lack of memory). It is written `fallback` with its provisional title, without downloading again.
- Otherwise: set the attempt as started, commit, download and read outside any transaction, then write the result.

### Stop and network outage (B2, C2)

- The worker runs with its own context, cancelled by `main` on shutdown; `main` waits for the worker to return before `st.Close()`.
- A download cancelled by that context writes nothing to the item and releases its attempt (`started_at` cleared, retries kept): the item stays `queued` and is retried after the restart.
- An error that comes from the NAS side — a DNS failure other than "no such host" (`*net.DNSError` without `IsNotFound`: no resolver reachable, timeout, server failure), no route (`EHOSTUNREACH`), network unreachable (`ENETUNREACH`) — writes nothing to the item and releases its attempt. The pass then tries at most 2 links of other hosts (`outageProbes`), skipping the links of the hosts that failed, and stops; the worker pauses 5 minutes before the next pass. Those links tell an outage of the NAS (they fail too) from a site that fails alone (they succeed). *(Amended in review: the pass first stopped at the outage, so that a link at the head of the queue held the others back; then it went on with every item, which during an outage tried every queued link.)*
- **Counted outages.** An outage of a link counts (`network_retries` + 1) only when another link reached its site since this link's previous outage (since start-up for its first one): a download that got an answer, or a failure of the site itself, written `full` or `fallback`, or deferred as a PDF; neither a refusal of the address policy, a crash marker, a URL without host nor another outage counts. At the third counted outage (`MaxNetworkRetries`), the link is written `fallback` with its provisional title, reason `network`. A real outage of the NAS therefore never makes a link fall back (nothing reaches a site, so at most the first outage after the last success counts), while a site that fails alone (a domain whose DNS fails with a server error, an address answered "no route to host") falls back at its third counted outage. The worker keeps, in memory, the number of links that reached their site since start-up and its value at each link's last outage: a restart forgets the successes before it, which can only delay a count, never add one. A site that does not answer, a refused connection, a TLS error, a redirect loop or a host that does not exist (`IsNotFound`: the resolver answered) are failures of the site: fallback. *(Implementation: `IsNotFound` was first listed on the NAS side; a dead or mistyped domain would then have stayed `queued` forever and paused every pass at the head of the queue, so it falls back.)*

### Fetching

An `http.Client` of its own, never `http.DefaultClient` nor `readability.FromURL` (checked in review):

- `Transport.Proxy = nil`; `DialContext` through a `net.Dialer` whose `Control` checks each resolved address against the address policy (DNS rebinding and redirects covered); `TLSHandshakeTimeout`, `MaxResponseHeaderBytes = 64 KiB`, no persistent cookie jar (an ephemeral jar per item, so that consent redirects do not loop).
- `CheckRedirect`: at most 10, `http` and `https` only; the `Referer` that `net/http` sets on a redirect is removed (RG-92: only the URL is sent).
- One deadline of 15 s for the whole download (DNS, TLS, redirects, headers, body), through the request context derived from the worker's.
- User info is removed from the URL before the request (`net/http` would send it as Basic Auth).
- Headers: `User-Agent: Hippo/<version> (personal read-later)` (decided with Ghislain; ASCII only, some servers refuse other bytes in a header), `Accept: text/html,application/xhtml+xml`, `Accept-Language: fr,en`. No manual `Accept-Encoding`, so Go's transparent gzip stays on.
- Body read through `io.LimitReader(limit + 1)` on the decompressed stream (decompression bombs and lying `Content-Length` covered); over the limit → fallback. The limit is 2 MB: 5 MB were planned, and the measure of task 3.3 (`runtime.ReadMemStats`, darwin/arm64) found 59 MB of heap for Readability on an article of 5 MB, 23 MB on the same article of 2 MB.
- A body whose type is neither HTML nor PDF is not read beyond its first 512 bytes (enough to recognise `%PDF-`).
- The URL is the item's trimmed `url`; the final URL after redirects is the base for Readability.

### Address policy (RG-27)

Allowed only when `addr.Unmap()` is `IsGlobalUnicast() && !IsPrivate()` and has no IPv6 zone, and is not in `0.0.0.0/8`, `100.64.0.0/10`, `192.0.0.0/24`, `198.18.0.0/15`, `240.0.0.0/4`, `64:ff9b::/96`, `2002::/16`, `fec0::/10`. All ports are allowed: blocking ports would add nothing once addresses are filtered. The policy is a `func(netip.AddrPort) bool` given to the worker; `cmd/hippo` passes the production one; tests pass one that also allows `127.0.0.1` for their `httptest` servers, and a dedicated test runs the production policy against `httptest` on `127.0.0.1` and `localhost` (fallback, zero request received) and a redirect from an allowed server to a refused one (the second receives nothing). Accepted with Ghislain's rule as written: a link to the home's public address goes through the box (NAT loopback); RG-27 does not cover it.

### Reading a page

1. Status 2xx and a `Content-Type` of `text/html` or `application/xhtml+xml` (or none and `http.DetectContentType` says HTML). `application/pdf`, or a body starting with `%PDF-`: the item is deferred (`deferred_pdf`, stays `queued`, E2-08). Any other type, or a non-2xx status: fallback, `og:*` not read (a 404 page's `og:title` would be "Page introuvable").
2. Decode with `golang.org/x/net/html/charset.NewReader(body, contentType)`, which honours the header and `<meta charset>`; then `strings.ToValidUTF8` on every text kept.
3. In the child process (below): one `html.Parse`; read `og:title`, `og:description`, `og:site_name` and `<title>`; then `Parser.ParseAndMutate(doc, finalURL)` with `MaxElemsToParse = 20 000`, kept to bound the time of Readability (its only error; fallback `too_large` with the metadata). A panic of the reading is recovered in the child: fallback, reason `reading_failed`.
4. `full` when the article text (`RenderText`) has at least 200 characters after trimming; otherwise fallback (a JavaScript-only page).

Steps 1 and the size are decided in the service (`readBody`), from what is cheap to read; steps 2 to 4, with the truncation and the reading time, run in the child (`readPage`).

### Reading a page in a child process

*(Amended at implementation, before merge: the reading of a page moves to a child process; the count of elements that preceded it is removed. Decided with Ghislain on 2026-11-02.)*

The memory of an HTML tree follows the markup, not the size of the page. Three reviews built pages of about 200 KB whose tree takes 85 to 160 MB, in a 128 MB container: formatting elements left open, which the parser rebuilds before each block (`<b>` × 40 then `</div><div>x` × 19 900), closed by end tags it ignores (stray `</i>`, `</b>` inside an `<object>`, a `<select>`, an SVG `<foreignObject>` or a MathML `<mi>`), or elements inside a `<style>` that the parser reads as markup inside SVG. A conservative count of elements before parsing was bypassed each time, and refused normal pages (700 cards `<a><div>…</div></a>`, an old page with `<font>` never closed). The service no longer estimates: it bounds the memory it measures.

- **Child.** The same binary, `hippo extract-page` (`extraction.RunChild`): it reads one JSON line (`content_type`, `url`, `title`, `host`) then the body to its end, at most `MaxBodySize` + 64 KiB; runs `readPage`; writes the outcome as JSON on stdout, with the peak of memory it held. It loads no configuration, opens no database, logs nothing; `cmd/hippo` dispatches it before anything else. Only `internal/extraction` imports Readability and `golang.org/x/net/html` still (`depguard`).
- **Memory guard.** `debug.SetMemoryLimit(36 MiB)` makes the garbage collector work harder first; a watchdog goroutine reads every millisecond, through `runtime/metrics`, the memory the runtime holds (`/memory/classes/total:bytes` minus `/memory/classes/heap/released:bytes`) and exits with code 86 above `ChildMemoryLimit`, 48 MiB. `GOMAXPROCS=2` at least, and the asynchronous preemption of Go, keep the watchdog running while `html.Parse` loops.
- **Environment.** The child's environment is `GOMAXPROCS=2`, and nothing else: never the service's, which holds the API token and the Anthropic key (a test asserts it). Its stderr is discarded: a panic message may quote the page.
- **Parent.** `exec.CommandContext` with its own time limit, `ReadTimeout` = 10 s, apart from the 15 s of the download; killed at the limit, `WaitDelay` 1 s for its pipes. The answer is read up to 2 MiB. Worst case of the answer, JSON-encoded without HTML escaping (`SetEscapeHTML(false)`): each code point takes at most 6 bytes (4 in UTF-8, 6 for a control character or U+2028 escaped as `\u00XX`), so the content or the description (120 000 code points, never both) takes at most 720 KB, the title and the site name (1 000 code points each) 6 KB each, the rest a few hundred bytes: under 750 KB, well under the 2 MiB. One child at a time: the worker is sequential. On Linux, the child gets `SIGKILL` when the service dies (`Pdeathsig`, `child_linux.go`), so that no orphan keeps reading after a crash.
- **Child that does not start.** The binary missing or a fork refused (no process state): the NAS is at fault, not the page. The attempt is released, the item stays `queued`, an error is logged (`item_id`, reason `reader_unavailable`, the error, which names the binary only), and the pass ends and pauses as for a network outage (`OutagePause`), rather than downloading every page for nothing.
- **Outcomes.** Exit 86 → fallback `too_large`, provisional title, no metadata. Time limit → fallback `timeout`. Any other exit, an answer too long or unreadable, a mode or a reason that `readPage` does not give (`full` with `ok`; `fallback` with `short`, `too_large`, `not_html` or `reading_failed`), a panic in the child → fallback `reading_failed`. The backend stopping while the child runs (the worker's context, not the time limit) → the item stays `queued` and its attempt is released, as for a download. The crash marker stays, for a crash of the service itself.
- **Measures** (2026-11-02, `BenchmarkReadInChild`, darwin/arm64, test binary; then the image in Docker, linux/arm64, `--memory 128m`, the server running and the child started by `docker exec` in the same cgroup):

| Page | Child, macOS | Child, container |
| --- | --- | --- |
| Article of 2 MB, ~18 000 elements (accepted) | 34.5 MB held, RSS 44.7 MB, 89 ms | 34.5 MB held, `full` |
| 700 cards | 9.9 MB held, RSS 20 MB, `full` | — |
| Each hostile page of the reviews (tiny elements 2 MB, reopened, stray ends, scoped ends, select ends, foreign ends, style in SVG) | exit 86 at the limit, RSS 59 to 61 MB, 60 to 80 ms | exit 86 |
| A page of a few words (cost of a spawn) | 5 MB held, RSS 14 MB, 8 ms | — |

  In the container, the server alone holds 31 MB (memory of the cgroup at rest, 36.6 MB of anonymous memory in an earlier run); with the child at its limit on each hostile page and the 2 MB article, the peak of the cgroup is 98.6 MB, no process killed for memory, and the server still answers its health check. Margin: 29 MB for what the service holds while a child reads (the body, 2 MB at most, and the answer, 2 MiB at most). A first try at 64 MiB (soft limit 48 MiB) peaked at 118 MB: too close to 128. The legitimate article of 2 MB keeps 13.5 MB below the child's limit.
- **Image.** `os.Executable()` reads `/proc/self/exe`, `/hippo` in the distroless image: no change to the Dockerfile nor to the compose file.

### What is written

- `full`: `title` = Readability title, else `og:title`, else `<title>`, else the provisional title, whitespace collapsed; `site_name` = `og:site_name`, else Readability's, else the host in the provisional-title form (lower case, no port); `content_text` = the text cut as below; `content_truncated`; `reading_minutes` = `ceil(words / 230)` on the text before truncation (RG-25: "de l'original"), at least 1.
- `fallback`: `title` = `og:title`, else `<title>`, else the provisional title (only from a 2xx answer); `site_name` as above when readable, else the host; `content_text` = `og:description` or null, cut as below and marked `content_truncated` when cut (RG-24 applies to any extracted content; *amended in review, 2026-11-02*); `reading_minutes` null.
- Titles and site name: a title kept from the page (Readability's, `og:title`, `<title>`), in any mode, and a site name are cut at `MaxTitleLength`, 1 000 code points, on a word boundary, without ellipsis nor mark: a technical bound, so that a hostile page cannot write a megabyte of title. No real title comes near it.
- Notes and images (`none`): no content, `reading_minutes` null.
- Truncation (RG-24): count code points; beyond 120 000, cut at the last whitespace within the last 1 000 code points before the limit, else at the limit; never inside a UTF-8 sequence.
- Write: one short transaction, `UPDATE items SET … WHERE user_id = ? AND id = ? AND status = 'queued' AND deleted_at IS NULL`, extraction columns only (never `user_note`, `is_private`), `updated_at` and `extracted_at` from the store clock, `change_seq` through `sequence.go`; zero row → ignored (deleted or changed meanwhile); the attempt row is deleted in the same transaction.

### Logs (C8)

One line per extraction: `item_id`, mode, a reason class (`ok`, `timeout`, `status`, `not_html`, `too_large`, `blocked`, `short`, `crashed_before`, `deferred_pdf`, `deferred_youtube`, `network`, and, added at implementation, `unreachable` for a site that failed before answering and `reading_failed` for a child that panicked, exited abnormally or answered something unreadable, and `reader_unavailable`, logged as an error, for a child that could not start), the HTTP status when there is one, and the duration. The download error is never logged nor stored (`*url.Error` prints the URL); `last_error` stays null.

### Tests

The `httpapi` and `capture` tests run without worker (a no-op waker), so the existing scenarios stay deterministic. The `extraction` tests run the worker on a real SQLite file and `httptest` servers serving fictional fixtures in `backend/testdata/pages/` (no real page, public repository): minified HTML, windows-1252 / ISO-8859-1, JavaScript-only, `og:*` only, 404 with `og:*`, oversized, gzip, a 920-word article, a 200 000-character article. The crash scenario is reproduced by inserting the attempt row through the store before starting the worker.

### user_id, private items and budget

Every query filters by `user_id` (the scan is per user, the write and the attempt rows carry it); a test with two users checks that each item is written for its own user. Private items are fetched (RG-92 exception) and never sent to the AI. No AI call, no budget check.

### Implementation notes

- A fallback writes the item's title (its provisional title) and, as site name, the host of the URL in lower case (the form of the provisional title, RG-26), explicitly.

- Store (`internal/store/extraction.go`): `UserIDs`, `QueuedItems(userID, afterID, limit)` (keyset paging by id, so that a pass walks each queued item once, YouTube links skipped in Go included), `StartExtractionAttempt` (an upsert that refuses another user's row), `ReleaseExtractionAttempt`, `PostponeExtraction`, `DeferExtraction`, `WriteExtraction` (deletes the attempt row, checks the item is still `queued`, then advances the sequence and updates, all in one transaction; nothing advances when the item is not written), `ItemContent` (the extracted content, read by the tests and later by E3).
- The fetcher resolves the host itself through an injectable `Resolver` (`Resolve(host, port) → []netip.AddrPort`, the system resolver in production) and dials each address in turn; `Dialer.Control` checks the address actually dialled. Tests inject their resolver, their certificate authority (`RootCAs`), a shorter `Timeout` for the 15 s scenario (the constant is asserted at 15 s), and `After`, so that the safety net and the outage pause are driven by the test; a hook after each pass replaces any sleep.
- Writes after a download (result, PDF deferral, release or postponement of the attempt) use `context.WithoutCancel` with a 10 s bound: a stop between the end of the download and the write must not leave an attempt under way that would make the item fall back at the next start. A failed write of the result releases the attempt, so that the item is extracted again rather than falling back.
- YouTube links are logged at debug level (`deferred_youtube`), since every pass meets them again.
- `capture.NewService(store, waker, paris, logger)`: the `Waker` is woken after a created item only (not after a replay nor a duplicate).
- Fixtures: the hand-written pages are files in `backend/testdata/pages/`; the pages whose size or shape matters (200 000 characters, 2 MB, tiny elements, 200-character threshold, the hostile pages of the reviews, 700 cards, an old page with `<font>`) are built by `internal/extraction/pagestest`, rather than committed as large files.
- The child in tests is the test binary: its `TestMain` runs `RunChild` when called with `extract-page`, and other modes simulate a child that hangs (it says so through a FIFO, without sleep), crashes or answers garbage. `Options.ReaderCommand` gives the command; `cmd/hippo` passes `os.Executable()` and `extract-page`. Under `-race`, the test child exits through `syscall.Exit`: `os.Exit(0)` would wait a second for the race detector at each reading.
- `gosec` G204 (a subprocess whose command is a variable) is excluded for `internal/extraction/child.go` only, in `.golangci.yml`: the command is the service's own binary, with fixed arguments.
- Readability keeps the full `<title>` (`Title | Site`) when the cut title would have four words or fewer (Readability.js heuristic); `og:title` wins when present.
- The scenario "A captured link becomes extracted" checks the `Created` result and the `queued` status of the capture service, which the API answers as `201`: `depguard` forbids `httpapi` outside `cmd/hippo`, test files included.

## Risks / Trade-offs

- [Memory of Readability on large pages] → read in a child bounded at 48 MiB, killed beyond: the page falls back, the service does not move (measured in task 3.4); body limit and `MaxElemsToParse` bound the time.
- [A legitimate page needs more than the child's limit] → it falls back (`too_large`). Measured margin: the 2 MB article of about 18 000 elements, at the body limit and near `MaxElemsToParse`, holds 34.5 MB, 13.5 MB below the limit.
- [A spawn per page] → 8 ms and 14 MB of resident memory for a short page; one child at a time.
- [A domain whose DNS fails with a server error, or an address answered "no route to host", is read as a NAS outage] → bounded: the link falls back at its third counted outage, once other links have succeeded in between; it never holds the links after it back, since the pass goes on. With no other link to extract, it waits `queued` until one comes.
- [During a NAS outage, each pass tries the first failing link and two others] → three failures every 5 minutes; the rest waits.
- [A NAS outage that is not a failure of resolution, of the network or of reaching a host] → the guarantee that a real outage never makes a link fall back holds for those three only. When the DNS answers from a cache while the Internet link is down, the connection times out: that is a failure of the site, and the link falls back (`timeout`).
- [Sites refusing an honest User-Agent] → they fall back; to revisit with real results.
- [A single-use link is consumed by the download] → accepted with the RG-92 exception.
- [`search_text` is still empty and the title changes at extraction] → the search story (E4-01) builds `search_text`.
- [First start with many `queued` items] → processed one by one, up to 15 s each; notes and images advance `change_seq` once each.
- [A YouTube or PDF link waits `queued` until E2-06 / E2-08] → nothing analyses items before E3.

## Migration Plan

`00005_extraction_attempts.sql` at start-up (ADR-0032), amended before its merge (nullable `started_at`, `network_retries`); no database has applied the earlier version outside development. Existing `queued` items are extracted by the first pass. Rollback is the backup (ADR-0020).
