# Design

## Context

See `proposal.md`. E2-05 and E2-08 built the extraction (`backend/internal/extraction`): one worker goroutine; a fetcher with its own client, address policy (RG-27), redirects checked, a single 15 s deadline, HTML read in memory up to 2 MiB; each page read by a child process of the same binary (`hippo extract-page`, `childRequest.Format` selects the reader) bounded at 48 MiB and 10 s; reasons logged as classes; network outages leave the item `queued`. Today `worker.extract` returns early for `isYouTube(host)` (`reasonDeferredYouTube`) and `downloadHost` returns `""` for such hosts.

Live check of 2026-11-05 (proposal, Impact): the watch page embeds `var ytInitialPlayerResponse = {…};` in a `<script>`, 1.35 MiB of HTML with the block about 770 KB in; `videoDetails` holds `videoId`, `title`, `author`, `lengthSeconds` (a string) and `shortDescription` (full description); an unknown video gives `playabilityStatus.status` `ERROR` and no `videoDetails`. oEmbed answers `{"title", "author_name", …}`, `400` for an unknown video.

## Goals / Non-Goals

**Goals:**
- Every YouTube link reaches `extracted`; a video gets at least its title whenever YouTube answers oEmbed.
- No new dependency, no secret, no cookie sent to YouTube.
- Same bounds as a page: memory and time of the child, 2 MiB, 15 s.

**Non-Goals:** see `proposal.md`.

## Decisions

### Packages touched

| Package | Change |
| --- | --- |
| `internal/extraction` | `youtube.go` (video id, canonical page, oEmbed URL and decoding), the child's `youtube` reader, the worker's video path; `isYouTube` kept for host matching; `reasonDeferredYouTube` removed |
| `internal/extraction/pagestest` (or the existing fixtures of the package) | Fictional video pages built by code: player data block, no description, duration 0, 130 000-character description, another video id, malformed JSON, status `ERROR` |
| `docs/glossary.md`, `docs/spec-v1.md` | Reason classes; the YouTube rows of the extraction section (task 5) |

No API contract, no migration, no prompt nor fiche schema.

### Recognising a video

`videoID(u *url.URL) (string, bool)`: the host, lower-cased and without final dot, is `youtube.com`, a subdomain of it, or `youtu.be`; the id matches `^[A-Za-z0-9_-]{11}$` and comes from:

| URL form | Id |
| --- | --- |
| `youtu.be/<id>` | first path segment |
| `…youtube.com/watch?v=<id>` (any other parameter, `list` included) | the `v` parameter |
| `…youtube.com/shorts/<id>`, `/live/<id>`, `/embed/<id>`, `/v/<id>` | the second path segment |

Anything else on a YouTube host (`/@channel`, `/channel/…`, `/playlist?list=…`, `/`, `/results`) is not a video and goes through the page path unchanged, so `downloadHost` returns its host like any link.

The page requested is always `https://www.youtube.com/watch?v=<id>` (canonical page). The captured URL is never requested: its tracking parameters (`si`, `utm_*`, `t`) do not leave the NAS. No cookie and no `Accept-Language` beyond the fetcher's defaults are sent.

### Reading the page

The canonical page is fetched with the existing `fetch` (policy, redirects, 2 MiB, HTML in memory) under a **10 s** share of the item's 15 s (RG-23), so that oEmbed keeps at least 5 s. When the final URL's host is not `www.youtube.com` (a consent page on `consent.youtube.com`), the page gives no metadata.

The body goes to the child with `childRequest.Format = formatYouTube` (`"youtube"`) and the video id. The child does not build an HTML tree: it finds the first `ytInitialPlayerResponse = ` marker in the body, decodes the JSON value that follows with `encoding/json` into a struct holding only `videoDetails.{videoId,title,author,lengthSeconds,shortDescription}`, and stops there. It answers `ok` with the four fields when `videoDetails.videoId` equals the requested id and the title is not empty after trimming; otherwise `no_metadata`. The memory watchdog, the time limit and the stdout bound of E2-05 apply unchanged.

The parent then writes, with `extraction_mode` `fallback`:

| Column | Value |
| --- | --- |
| `title` | the video title, control characters removed, bounded to `MaxTitleLength` (1 000 code points) as page titles |
| `site_name` | the channel, same cleaning and bound; the host when empty |
| content | the description, trimmed; null when empty; cut at 120 000 characters and `content_truncated` beyond (RG-24, existing `text.go` helpers) |
| `reading_minutes` | `ceil(lengthSeconds / 60)`, at least 1; null when `lengthSeconds` is absent, not a decimal integer, 0, or above 1 000 000 (about 11 days) |

Alternative rejected: read `og:title`, `og:description` and the microdata `itemprop="duration"` with the page reader. `og:description` is cut at about 160 characters, and the channel name is not in `og:*`. They could serve as a second source, but oEmbed already guarantees the title.

### oEmbed fallback

When the page path yields no metadata — fetch failure other than a network outage or a stop, non-2xx status, timeout, too large, consent page, child answer `no_metadata`, child failure — the worker requests `https://www.youtube.com/oembed?url=<canonical page, query-escaped>&format=json` with the same client and policy, within the time left of the 15 s, body bounded to **64 KiB** (`application/json` is read in memory for this request only). It decodes `title` and `author_name` in the parent (a bounded JSON object, decoded into two strings). A `2xx` answer with a non-empty title writes `title`, `site_name` (the host when `author_name` is empty), no content, no reading time. Any other outcome writes the fallback of RG-26: provisional title, host as `site_name`.

A network outage on the page request follows E2-05 (`networkOutage`: the item stays `queued`, a link that alone keeps failing falls back at its third failure). A network failure of the oEmbed request alone writes the provisional fallback: the page request already reached the network.

### Reason classes

| Reason | When |
| --- | --- |
| `ok` | metadata read from the page |
| `oembed` | page without metadata, oEmbed gave the title |
| existing classes (`status`, `timeout`, `too_large`, `blocked`, `unreachable`, `network`, `reading_failed`, `crashed_before`) | as for a page, when oEmbed failed too |
| `no_metadata` | page answered, no usable player data, oEmbed failed too |

`deferred_youtube` disappears. The log line keeps its fields (item id, mode, reason, HTTP status, durations), with `format` `youtube`.

### Links left waiting

The `deferred_youtube` path wrote nothing in `extraction_attempts` and left the item `queued`: removing it is enough for the next pass (start-up, five-minute tick or next capture) to extract them. No data migration.

### Tests

Fictional pages built by code (ADR-0027: no real YouTube page in the repo), served by the test server that already stands for the Internet in `worker_test.go`, the test resolver mapping `www.youtube.com`, `youtu.be` and `m.youtube.com` to it. One sub-test per scenario of the delta, named after it; table tests for `videoID` (each URL form, ids of 10 and 12 characters refused, `watch` without `v`, channel and playlist URLs); child tests for the player data reader (marker absent, malformed JSON, another id, `lengthSeconds` `"abc"`, `"-5"`, `"0"`, `"9999999"`).

### user_id, private items and budget

Every query goes through the existing store functions, filtered by `user_id` (RG-90). A private item's video is requested from YouTube like any link (RG-92 exception of E2-05); nothing goes to the AI, no budget is involved.

## Risks / Trade-offs

- [YouTube changes its page] → no description and no duration; oEmbed keeps the title; the reason `oembed` rising in the logs shows it.
- [YouTube serves a consent page to the NAS] → oEmbed, title and channel only. Answering the consent would mean sending a cookie: not done (non-goal).
- [The page grows beyond 2 MiB] → `too_large`, then oEmbed. Measured at 1.35 MiB on 2026-11-05.
- [A title translated by YouTube for the requester's language] → the fetcher sends no `Accept-Language`; accepted.
- [YouTube rate-limits the NAS] → one or two requests per captured video, a few videos a week: negligible.

## Migration Plan

None: no migration; the waiting links are picked up by the next pass after deployment.

## Open Questions

None.
