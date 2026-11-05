# Proposal

## Why

Since E2-05, a YouTube link stays `queued` without being downloaded, so it never reaches the analysis. Ghislain rarely captures videos, and reading their transcript means an undocumented YouTube interface that breaks without notice. On 2026-11-05 he decided that a video is kept with what can be read simply: title, channel, duration and description, the transcript being postponed to V1.5 (RG-22 modified, ADR-0037 amending ADR-0008). This change implements backlog story **E2-06 — Extraire une vidéo YouTube**, so reduced.

## What Changes

- A **YouTube video link** is recognised by its host (`youtube.com`, any subdomain, `youtu.be`) and an 11-character video id in one of the forms `watch?v=<id>`, `youtu.be/<id>`, `/shorts/<id>`, `/live/<id>`, `/embed/<id>`, `/v/<id>`. Its extraction downloads the canonical page `https://www.youtube.com/watch?v=<id>` (not the captured URL: no tracking parameter, one form for all), with the fetcher of E2-05 (address policy, single 15 s limit, 2 MiB).
- The page is read in the extraction child process (E2-05: memory and time bounded). The child looks for the JSON block `ytInitialPlayerResponse` and keeps, from its `videoDetails` whose `videoId` is the requested one, the title, the channel (`author`), the duration (`lengthSeconds`) and the description (`shortDescription`) (RG-22).
- The item is written with `extraction_mode` `fallback` (no transcript, RG-22, RG-23): `title` the video title; `site_name` the channel name; the description as content, cut at 120 000 characters and marked `content_truncated` beyond (RG-24), no content when it is empty; `reading_minutes` the duration in minutes, rounded up, at least 1, null when the duration is 0 or unknown (a live stream) (RG-22, RG-25).
- When the page gives no metadata (error status, time out, consent page, no JSON block, another video, malformed JSON), the extraction asks **oEmbed** (`https://www.youtube.com/oembed?url=<canonical page>&format=json`, response bounded to 64 KiB): its `title` and `author_name` become title and `site_name`, without content nor reading time. When oEmbed fails too (deleted or private video), the item falls back with its provisional title and the host as site name (RG-23, RG-26).
- A YouTube link that is not a video (channel, playlist without video, home page) is extracted as a web page, like any link.
- The links left `queued` as `deferred_youtube` by E2-05 are picked up by the next pass; nothing is deferred any more.
- A network outage while downloading the page leaves the item `queued`, as for a page (E2-05).

## Non-goals

- The transcript (V1.5, ADR-0037).
- The thumbnail, the chapters, the publication date, the view count.
- A cookie or a consent answer to YouTube: a consent page leads to oEmbed.
- Any API change: the fields already exist in `Item`.
- A fiche schema for videos.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `item-extraction`: the requirement « YouTube links wait for their own extraction » is replaced by the extraction of a video's metadata, its oEmbed fallback, and the extraction of non-video YouTube links as web pages; the logs requirement covers videos.

## Impact

- **Story**: E2-06 (scope reduced with Ghislain on 2026-11-05).
- **Business rules**: RG-12, RG-20, RG-22 (modified on 2026-11-05), RG-23, RG-24, RG-25, RG-26, RG-90, RG-92 (exception of E2-05: a private item's video is requested from YouTube like any link).
- **ADRs**: ADR-0008 (amended), ADR-0037 (new), ADR-0019, ADR-0027 (test pages fictional, built by code, no YouTube page committed), ADR-0035.
- **Live check** (2026-11-05, home network in France): the watch page (1.35 MiB) held `ytInitialPlayerResponse` with title, author, `lengthSeconds` and the full description, without consent page; `youtu.be/<id>` and `/shorts/<id>` redirect to it; an unknown video answers `200` with status `ERROR` and no `videoDetails`; oEmbed answers the title and `author_name`, and `400` for an unknown video.
- **AI cost**: none here; the nightly analysis reads title, channel, description and capture note, a few hundred tokens per video.
- **Private items**: extracted like the others (RG-92 exception); nothing is sent to the AI.
- **Persisted data**: no migration; the existing extraction columns.
- **API contract**: none.
- **Logs**: one line per extraction as today, with the reason classes of design.md; never the URL, the video id, the title, the channel nor the description.
- **Dependencies**: none.
- **Prompt and fiche schemas**: none.
- **User QA: no**.
