# Design

## Context

See `proposal.md`. E2-05 and E2-08 built the extraction (`backend/internal/extraction`): one worker goroutine; a fetcher with its own client, address policy (RG-27), redirects checked, a deadline of 15 s set inside `fetch` (`fetch.go:155`), HTML read in memory up to 2 MiB, any other non-PDF type kept to its first 512 bytes (`fetch.go:215`); each page read by a child process of the same binary (`hippo extract-page`, `childRequest.Format` selects the reader) bounded at 48 MiB and 10 s, its answer bounded at 2 MiB (`maxChildOutput`); reasons logged as classes; network outages leave the item `queued`. Today `worker.extract` returns early for `isYouTube(host)` (`reasonDeferredYouTube`) and `downloadHost` returns `""` for such hosts.

Live check of 2026-11-05 (proposal, Impact): the watch page embeds `var ytInitialPlayerResponse = {…};` in a `<script>`, 1.35 MiB of HTML with the block about 770 KB in; `videoDetails` holds `videoId`, `title`, `author`, `lengthSeconds` (a string) and `shortDescription` (full description); an unknown video gives `playabilityStatus.status` `ERROR` and no `videoDetails`. oEmbed answers `{"title", "author_name", …}`, `400` for an unknown video. That check was not made with the fetcher's own headers (`User-Agent: Hippo/<version> (personal read-later)`, `Accept-Language: fr,en`, `fetch.go:99`, `fetch.go:165-167`): task 4.2 repeats it with them.

Live check with the fetcher's headers (task 4.2, 2026-11-05, development machine, home network in France): one public video's canonical page requested with exactly `User-Agent: Hippo/dev (personal read-later)`, the page `Accept`, `Accept-Language: fr,en`, `Accept-Encoding: gzip`, no cookie. Answer `200` from `www.youtube.com` with no redirect (final host `www.youtube.com`), no consent page; 1 371 218 bytes of HTML once decompressed (322 740 compressed), under the 2 MiB limit. The token `ytInitialPlayerResponse` appears 3 times; the first is `= {`, about 787 KB in, a block of 37 KB with `playabilityStatus.status` `OK` and `videoDetails` holding `videoId`, `title`, `author`, `lengthSeconds` and `shortDescription`, all JSON strings. Its oEmbed, with the same headers and `Accept: application/json`, answered `200` from `www.youtube.com`, 408 bytes, with a non-empty `title` and `author_name`. No page content was kept in the repository (ADR-0027).

## Goals / Non-Goals

**Goals:**
- Every YouTube link reaches `extracted`; a video gets at least its title whenever YouTube answers oEmbed.
- No new dependency, no secret, no cookie of ours sent to YouTube.
- Same bounds as a page: memory and time of the child, 2 MiB, 15 s of download.

**Non-Goals:** see `proposal.md`.

## Vocabulary

- **Format** (`format` of the logs, glossary « Format de la réponse »): gains the value `youtube`, the page of a video read for its player data. It is a reader, not a media type; the glossary row says so (task 6.1).
- **Reason classes**: `oembed` and `no_metadata` added, `deferred_youtube` removed (glossary « Classe de raison »).
- The glossary rows « Extraction » (« transcription d'une vidéo YouTube ») and « Repli » (only `og:*`) describe the old plan; task 6.1 aligns them on RG-22 as modified: a video's metadata, and a video in fallback keeps its description and its duration.
- No other new term: « vidéo », « chaîne », « durée » are those of RG-22.

## Decisions

### Packages touched

| Package | Change |
| --- | --- |
| `internal/extraction` | `youtube.go` (video id, canonical page and oEmbed URLs, oEmbed decoding); `fetch` takes its timeout and its reading mode per call; the child's `youtube` reader; the worker's video path; `downloadHost` returns `www.youtube.com` for a video; `isYouTube` kept for host matching; `reasonDeferredYouTube` removed |
| `internal/extraction` tests | Fictional video pages and oEmbed answers built by code; the test certificate and resolver extended to `youtube.com`, its subdomains and `youtu.be`; handlers that see the query string |
| `docs/glossary.md`, `docs/spec-v1.md` | Vocabulary above; the YouTube rows of the extraction section (task 6.1) |

No API contract change (`Item` already has every field; `ExtractionMode` « `fallback`: metadata only » stays true). No migration. No prompt nor fiche schema change: the analysis receives the channel as `site:` and the description as `<contenu>` through the existing template (`docs/prompt/system-prompt.md`, « Message utilisateur »); the duration is not sent to the AI.

### Recognising a video

`videoID(u *url.URL) (string, bool)`: the host, lower-cased and without final dot, is `youtube.com`, a subdomain of it, or `youtu.be`; the id matches `^[A-Za-z0-9_-]{11}$` and comes from:

| URL form | Id |
| --- | --- |
| `youtu.be/<id>` | first path segment |
| `…youtube.com/watch?v=<id>` (any other parameter, `list` included) | the first `v` parameter, decoded |
| `…youtube.com/shorts/<id>`, `/live/<id>`, `/embed/<id>`, `/v/<id>` | the second path segment |

A trailing slash and further segments are tolerated; scheme and port are ignored. Anything else on a YouTube host (`/@channel`, `/channel/…`, `/playlist?list=…`, `/`, `/results`, `/watch` without a valid `v`) is not a video and goes through the page path unchanged. Hosts that merely look alike (`youtube-nocookie.com`, `youtube.com.example.com`, `www.youtu.be`) are not YouTube hosts and are plain web pages.

The page requested is always `https://www.youtube.com/watch?v=<id>` (canonical page). The captured URL is never requested: its tracking parameters (`si`, `utm_*`, `t`) do not leave the NAS.

**RG-92.** The exception lets the extraction ask « the captured URL from its own site ». The canonical page and oEmbed are asked from that same site, YouTube, and carry only the video id, a strict subset of the captured URL: nothing else leaves the NAS. Ghislain chose these two requests when framing the scope on 2026-11-05; RG-92 in `metier.md` names them explicitly since this change.

`downloadHost` returns `www.youtube.com` for a video, whatever the captured form, so that the outage logic of a pass (`failedHosts`, probes of other hosts) treats every video as the one site it actually asks.

### Requests and time budget

`fetch` gets its options per call instead of from the fetcher: `fetch(ctx, target, req fetchRequest)` with `fetchRequest{Timeout, Accept, Mode}` where `Mode` is `modePage` (today's behaviour, PDF included), `modeHTML` (an HTML body in memory up to 2 MiB, any other format not read beyond its first bytes, never a temporary file) or `modeJSON` (the body in memory up to 64 KiB whatever its type, `TooLarge` beyond). The deadline stays inside `fetch`, derived from the worker's context, so that `classify(ctx, err)` with the worker's context still tells a stop (`reasonStopped`) from a deadline (`reasonTimeout`). The worker MUST NOT derive a shorter context itself and classify with it: a deadline would read as a stop, the attempt be released, and the item retried every five minutes forever. The only other caller, `export_test.go` `Fetcher`, follows.

- **Page**: `modeHTML`, today's `Accept`, timeout = 2/3 of `Options.Timeout` (10 s in production).
- **oEmbed**: `modeJSON`, `Accept: application/json`, timeout = 1/3 of `Options.Timeout` (5 s), whatever the page took.

Download time of a video is thus at most 15 s, as for a page (RG-23); the child's reading (at most 10 s) comes on top, as for a page. Tests set `Options.Timeout` short and keep the same shares.

Each `fetch` builds its own cookie jar (`fetch.go:172`): no cookie of ours is sent; a cookie that YouTube sets during the redirects of one request is returned within that request only, and nothing passes from the page to oEmbed. `Accept-Language: fr,en` is sent as for any page (`fetch.go:167`).

### Reading the page

The page's final URL must be on a YouTube host (`isYouTube`), its status 2xx, its body HTML and not `TooLarge`; otherwise it gives no metadata (a consent page on `consent.youtube.com` is a YouTube host: its body simply holds no player data). A redirect off YouTube (`www.google.com/sorry/…` when YouTube suspects a robot) gives no metadata.

The body goes to the child with `childRequest.Format = formatYouTube` (`"youtube"`) and a new field `childRequest.VideoID`. The child does not build an HTML tree:

1. It looks for the token `ytInitialPlayerResponse` followed by optional spaces, `=`, optional spaces and `{`; at most 3 occurrences are tried, in order.
2. From that `{`, it decodes one JSON value with `json.Decoder.Decode`, which reads one value and ignores what follows (`;var meta = …`) (`$GOROOT/src/encoding/json/stream.go:45-51`, Go 1.27.3 of `backend/go.mod`). Nesting beyond 10 000 levels is an error (`$GOROOT/src/encoding/json/scanner.go:148`); invalid UTF-8 in strings becomes U+FFFD (`$GOROOT/src/encoding/json/decode.go:99-102`).
3. `videoDetails` is decoded into a struct of `json.RawMessage` fields (`videoId`, `title`, `author`, `lengthSeconds`, `shortDescription`), each then decoded on its own: a field of an unexpected type counts as absent without losing the others (`Unmarshal` does not guarantee the fields after a type error, `decode.go:86-92`). `lengthSeconds` is accepted as a JSON string or number.
4. The first occurrence whose `videoDetails.videoId` equals `VideoID` and whose title is not empty after cleaning wins; none → `no_metadata`.

The token cannot be forged from a description: inside the JSON block it comes after the real token, and in the `<head>` meta tags quotes are escaped as entities, so the value does not decode. A page `<title>` holding the token can only spoof the uploader's own metadata, which the uploader already controls.

The child builds the final outcome itself, as `readPage` does, so that its answer stays under `maxChildOutput` (a 2 MiB description of `"` would be 4 MiB of JSON before truncation): mode `fallback`, reason `ok`, and

| Column | Value |
| --- | --- |
| `title` | the video title through `clean`, bounded by `cutTitle` (`MaxTitleLength`, 1 000 code points) |
| `site_name` | the channel, same cleaning and bound; the host of the captured URL when empty |
| content | the description, valid UTF-8, line breaks kept (chapters, links), carriage returns and control characters other than line feed and tab removed, trimmed; null when empty; `truncate` (120 000 code points, word boundary, `content_truncated`) (RG-24) |
| `reading_minutes` | `ceil(lengthSeconds / 60)`, at least 1; null when `lengthSeconds` is absent, not a decimal integer, 0, negative, or above 1 000 000 (about 11 days) (RG-22, RG-25) |

`childReasons[formatYouTube]` accepts `ModeFallback` with `ok` or `no_metadata` only. The memory watchdog, the time limit and the output bound of E2-05 apply unchanged; a child that exceeds them (`too_large`, `timeout`, `reading_failed`) means no metadata. `errReaderStopped` and `errReaderUnavailable` are handled exactly as for a page (attempt released, `stepStop` / `stepPause`).

Alternative rejected: read `og:title`, `og:description` and the microdata `itemprop="duration"` with the page reader. `og:description` is cut at about 160 characters, and the channel name is not in `og:*`. They could serve as a second source, but oEmbed already guarantees the title.

### oEmbed fallback

When the page path yields no metadata (non-2xx status, timeout, too large, not HTML, final host not YouTube, no usable player data, child failure, unreachable or blocked), the worker requests `https://www.youtube.com/oembed?url=<canonical page, query-escaped>&format=json`. Its final URL must be on a YouTube host and its status 2xx; the body (64 KiB at most) is decoded in the parent into `title` and `author_name` only, each through `clean` and `cutTitle`. A non-empty title writes `title`, `site_name` (the captured host when `author_name` is empty), no content, no reading time, reason `oembed`. Any other outcome writes the fallback of RG-26: provisional title, captured host as `site_name`.

### Outages, stops and the attempt row

- The attempt row (`StartExtractionAttempt`) is written once, before the page request, after the `AttemptStarted` check; it covers the page, the child and oEmbed, and is cleared by the single write. A video whose attempt never finished falls back (`crashed_before`) with neither request.
- A stop at any point (page, child, oEmbed) releases the attempt and leaves the item `queued`; the next pass starts again from the page.
- A network outage of the NAS (`reasonNetwork`) on **either** request goes through `networkOutage` (the item stays `queued`, counted as E2-05 counts it, falls back at its third counted outage), as the requirement « Every captured item reaches extracted without blocking » demands. oEmbed is not tried after a network outage on the page.
- `write` gets `reachedSite` true when either request got an answer from YouTube.

### Logs

One line per extraction as today: item id, mode, reason, `format` `youtube`, `http_status` of the page when it answered, `oembed_status` when oEmbed was asked and answered, durations. Never the URL, the host, the id, the title, the channel nor the description; the errors of `fetch` name the URL and are classified, never logged (`fetch.go:153`); the child's stderr is discarded (`child.go:287`).

| Reason | When |
| --- | --- |
| `ok` | metadata read from the page |
| `oembed` | page without metadata, oEmbed gave the title |
| `status`, `timeout`, `too_large`, `not_html`, `blocked`, `unreachable`, `reading_failed` | class of the page's failure, when oEmbed failed too |
| `no_metadata` | page answered 2xx (or ended off YouTube), no usable player data, oEmbed failed too |
| `crashed_before`, `network` | as for a page |

`deferred_youtube` disappears.

### Links left waiting

The `deferred_youtube` path wrote nothing in `extraction_attempts` and left the item `queued` with no retry counted: removing it is enough for the next pass (start-up, five-minute tick or next capture) to extract them. Non-video YouTube links left waiting are read as pages. No data migration.

### Tests

Fictional pages built by code (ADR-0027: no real YouTube page in the repo), served by the TLS test server of `worker_test.go`. Its certificate gains `youtube.com`, `*.youtube.com` and `youtu.be`, its resolver maps them to the server; the handlers of `www.youtube.com/watch` and `/oembed` dispatch on the query (`v`, `url`) and the test web records raw queries, so that a test asserts the exact page requested and that `youtu.be` or `m.youtube.com` were never asked. The harness check that no log holds `youtube.com` nor `youtu.be` (`worker_test.go:308`) applies to every video test. One sub-test per scenario of the delta, named after it; the old « YouTube link » sub-test goes with its requirement. Table tests for `videoID` (each URL form, ids of 10 and 12 characters, `watch` without `v`, two `v`, look-alike hosts, channel and playlist URLs); child tests for the player data reader (token absent, `= null`, malformed JSON, nesting of 10 001 levels, another id then the right one, `lengthSeconds` `"abc"`, `"-5"`, `"0"`, `754`, `"9999999"`, `title` of another type, description with `\r\n` and NUL); fetch tests for the modes (JSON over 64 KiB, PDF in `modeHTML` not written to disk) and for a page deadline classified `timeout`, not `stopped`.

### user_id, private items and budget

Every query goes through the existing store functions, filtered by `user_id` (RG-90). A private item's video is requested from YouTube like any link (RG-92 exception, see above), and a scenario proves it; the extraction never talks to the AI (RG-12), no budget is involved.

### Measures

Task 5.1, `BenchmarkReadYouTubeInChild` (`memory_test.go`, `go test -run '^$' -bench ReadYouTubeInChild -benchtime 5x ./internal/extraction/`, without `-race`), on an Apple M4 Max, fictional pages of 1 900 KiB, the description twice in the player data as on a real page (`videoDetails` and `microformat`):

| Page | Child peak held | Child RSS | Reading (spawn included) | Outcome |
| --- | --- | --- | --- | --- |
| Player data of the real shape, 30 adaptive formats, 900-character description, block 40 % in, filler scripts around | 10.3 MiB | 20.1 MiB | 11 ms | `fallback`, `ok` |
| Description filling the page, ASCII words | 18.8 MiB | 29.3 MiB | 17 ms | `fallback`, `ok` |
| Description filling the page, 4-byte characters | 15.5 MiB | 25.7 MiB | 20 ms | `fallback`, `ok` |

Every peak stays under half of `ChildMemoryLimit` (24 MiB).

### Deviations found during implementation

- **The output bound of the child never applied (defect of E2-05, fixed here).** `cappedBuffer` embedded `bytes.Buffer`, which lent it `ReadFrom`; `exec.Cmd` copies the child's stdout with `io.Copy`, which prefers `ReadFrom` to `Write`, so the cap of `maxChildOutput` was bypassed and a child could fill the service's memory with its answer. The buffer is now a field. A test child (`flood-child`) writes `maxChildOutput + 1` spaces then an outcome valid for the format it was asked (`full`, `ok` for a page; `fallback`, `ok` for a video), so that only the output bound refuses it: it falls back with `reading_failed` for a page and for a video, and both tests were checked to fail against the old embedded buffer (task 3.1 asked to verify the bound applies unchanged: it did not).
- **The fetcher no longer holds a timeout.** Each call gives its own (`fetchRequest.Timeout`); the dialer keeps `DownloadTimeout` as a mere ceiling, the deadline of the call bounding the connection first.
- **Order of the page checks** (`videoPageReason`): a non-2xx status gives `status`; a final host off YouTube gives `no_metadata` whatever its body; then `too_large`, then `not_html` (a PDF read in `modeHTML` keeps its first bytes and is `not_html`).
- **A page reader that cannot start** leaves the video `queued` and pauses the pass without asking oEmbed, as for a page: the NAS is at fault, not the video.
- **Time shares tested**: in « Video page that does not answer in time » and « Neither the page nor oEmbed answers in time » (timeout shortened to 900 ms), the server measures each hanging request from its arrival to its end: the page lasts between 0.4 and 0.8 of the timeout, oEmbed at most 0.5, and the extraction ends within 1.5 times the timeout. Swapping the two shares was checked to fail these tests.
- **Extra tests beyond the scenarios**: a page redirected off YouTube; an oEmbed answer redirected off YouTube or over 64 KiB; a page over 2 MB; a network outage on oEmbed; a stop during oEmbed; an unavailable reader; every form of a video counted as the one host `www.youtube.com` after an outage.

## Risks / Trade-offs

- [YouTube changes its page, or serves a lighter page to the fetcher's User-Agent] → no description and no duration; oEmbed keeps the title; the reason `oembed` rising in the logs shows it. Task 4.2 checks the fetcher's headers once before merging.
- [YouTube serves a consent page to the NAS] → oEmbed, title and channel only. Answering the consent would mean sending a cookie: not done (non-goal). A non-video YouTube link (a channel) behind a consent page would get the consent page's `og:title`; accepted, the live check saw no consent page.
- [YouTube rate-limits the NAS (`429`, redirect to `google.com/sorry`)] → page and oEmbed fail, provisional title; one or two requests per captured video, a few videos a week: unlikely.
- [The page grows beyond 2 MiB] → `too_large`, then oEmbed. Measured at 1.35 MiB on 2026-11-05.
- [A title or description translated for the requester's language] → the fetcher sends `Accept-Language: fr,en`; YouTube may answer a French translation of the title chosen by the creator or YouTube (unverified); accepted, Ghislain reads French.
- [The same video captured as `youtu.be/<id>` and `watch?v=<id>`] → two items, as deduplication works on the normalised URL (RG-04, RG-05); out of scope.

## Migration Plan

None: no migration; the waiting links are picked up by the next pass after deployment.

## Open Questions

None.
